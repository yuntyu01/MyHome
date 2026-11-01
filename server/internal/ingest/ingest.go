// Package ingest는 폰이 보낸 알림 원문을 받아 raw_events에 저장한다.
package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/auth"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/httpx"
)

// Request는 POST /v1/events 본문이다. 앱은 해석하지 않은 원문을 그대로 보낸다.
type Request struct {
	SourcePackage string `json:"source_package"`
	Title         string `json:"title"`
	Text          string `json:"text"`
	BigText       string `json:"big_text"`
	// PostedAt은 알림 게시 시각(Unix 밀리초)이다. Android StatusBarNotification.postTime 그대로.
	PostedAt int64 `json:"posted_at"`
}

// Response는 수집 결과다.
type Response struct {
	ID        int64 `json:"id"`
	Duplicate bool  `json:"duplicate"`
	// Result는 처리 파이프라인 결과(거래 생성 등)다. 처리기가 없으면 비어 있다.
	Result any `json:"result,omitempty"`
}

// Processor는 저장된 원문을 해석해서 거래를 만든다(Phase 3).
type Processor interface {
	ProcessRawEvent(ctx context.Context, rawEventID int64) (any, error)
}

// Handler는 수집 API 핸들러다.
type Handler struct {
	Pool      *pgxpool.Pool
	Processor Processor // nil이면 저장만 한다.
	Logger    *slog.Logger
	Now       func() time.Time
}

const (
	maxPackageLen = 255
	maxFieldRunes = 8000
)

// Validate는 요청 값을 검사한다.
func (req *Request) Validate(now time.Time) error {
	req.SourcePackage = strings.TrimSpace(req.SourcePackage)
	switch {
	case req.SourcePackage == "":
		return errors.New("source_package is required")
	case len(req.SourcePackage) > maxPackageLen:
		return errors.New("source_package too long")
	case req.PostedAt <= 0:
		return errors.New("posted_at (unix ms) is required")
	case time.UnixMilli(req.PostedAt).After(now.Add(24 * time.Hour)):
		return errors.New("posted_at is in the future")
	case req.Title == "" && req.Text == "" && req.BigText == "":
		return errors.New("title, text, big_text are all empty")
	}
	for _, f := range []string{req.Title, req.Text, req.BigText} {
		if !utf8.ValidString(f) {
			return errors.New("invalid utf-8")
		}
		if utf8.RuneCountInString(f) > maxFieldRunes {
			return errors.New("field too long")
		}
	}
	return nil
}

// ContentHash는 멱등 처리용 해시다. 같은 알림을 재전송하면 같은 값이 나온다.
// 같은 내용이라도 게시 시각이 다르면 다른 이벤트(예: 같은 카페 두 번 결제)로 본다.
func ContentHash(req Request) string {
	h := sha256.New()
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(req.PostedAt))
	for _, part := range [][]byte{[]byte(req.SourcePackage), ts[:], []byte(req.Title), []byte(req.Text), []byte(req.BigText)} {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(part)))
		h.Write(n[:])
		h.Write(part)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// PostEvent는 POST /v1/events를 처리한다. 기기 인증 미들웨어 뒤에 둔다.
func (h *Handler) PostEvent(w http.ResponseWriter, r *http.Request) {
	dev, ok := auth.DeviceFromContext(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req Request
	if err := httpx.DecodeJSONLenient(r, &req); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpx.Error(w, http.StatusRequestEntityTooLarge, "body too large")
			return
		}
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	now := time.Now()
	if h.Now != nil {
		now = h.Now()
	}
	if err := req.Validate(now); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	q := dbq.New(h.Pool)
	hash := ContentHash(req)
	devID := dev.ID
	row, err := q.InsertRawEvent(r.Context(), dbq.InsertRawEventParams{
		DeviceID:      &devID,
		SourcePackage: req.SourcePackage,
		Title:         req.Title,
		Text:          req.Text,
		BigText:       req.BigText,
		PostedAt:      time.UnixMilli(req.PostedAt),
		ContentHash:   hash,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err := q.GetRawEventByHash(r.Context(), hash)
		if err != nil {
			h.Logger.Error("중복 원문 조회 실패", "err", err)
			httpx.Error(w, http.StatusInternalServerError, "internal error")
			return
		}
		h.Logger.Info("원문 중복 수신", "raw_event_id", existing.ID, "device_id", dev.ID)
		resp := Response{ID: existing.ID, Duplicate: true}
		// 이전 처리 도중 실패해서 처리되지 않은 원문이면 다시 처리한다.
		if existing.ProcessedAt == nil && h.Processor != nil {
			resp.Result = h.process(r.Context(), existing.ID)
		}
		httpx.JSON(w, http.StatusOK, resp)
		return
	}
	if err != nil {
		h.Logger.Error("원문 저장 실패", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.Logger.Info("원문 저장", "raw_event_id", row.ID, "device_id", dev.ID, "package", row.SourcePackage)

	resp := Response{ID: row.ID}
	if h.Processor != nil {
		resp.Result = h.process(r.Context(), row.ID)
	}
	// 원문은 이미 저장됐으므로 처리 실패와 관계없이 201을 반환한다.
	// 그래야 앱이 알림을 지우고 재전송을 멈춘다. 처리 실패는 parse_error로 남아 재처리할 수 있다.
	httpx.JSON(w, http.StatusCreated, resp)
}

func (h *Handler) process(ctx context.Context, id int64) any {
	res, err := h.Processor.ProcessRawEvent(ctx, id)
	if err != nil {
		h.Logger.Error("원문 처리 실패", "raw_event_id", id, "err", err)
		return map[string]string{"error": "processing failed"}
	}
	return res
}
