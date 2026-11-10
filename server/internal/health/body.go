package health

import (
	"context"
	"math"
	"net/http"

	"github.com/yuntyu01/MyHome/server/internal/ai"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/httpx"
)

// 몸 기록(사용자 요청, 2026-11-10): 몸무게는 날짜마다 하나를 서버에 둔다. 몸 사진은 폰에만 둔다(서버로 보내지 않는다).

// BodyScanPath는 저울 사진을 받으므로 서버 조립에서 본문 제한을 사진 크기로 연다.
const BodyScanPath = "/v1/health/body/scan"

// Weight는 하루 몸무게다.
type Weight struct {
	Date     Date    `json:"date"`
	WeightKg float64 `json:"weightKg"`
}

func validWeight(kg float64) bool { return !math.IsNaN(kg) && kg >= 20 && kg <= 400 }

// SaveWeight는 그날 몸무게를 넣거나 바꾼다(소수 한 자리로).
func (s *Service) SaveWeight(ctx context.Context, d Date, kg float64) (Weight, error) {
	if !validWeight(kg) {
		return Weight{}, invalid("weightKg", "20~400kg여야 해요")
	}
	kg = math.Round(kg*10) / 10
	row, err := s.q().UpsertBodyWeight(ctx, dbq.UpsertBodyWeightParams{Day: d.Time(), WeightKg: kg})
	if err != nil {
		return Weight{}, err
	}
	return Weight{Date: DateOf(row.Day), WeightKg: row.WeightKg}, nil
}

// Weights는 기간 몸무게다(날짜 순).
func (s *Service) Weights(ctx context.Context, from, to Date) ([]Weight, error) {
	rows, err := s.q().ListBodyWeights(ctx, dbq.ListBodyWeightsParams{FromDay: from.Time(), ToDay: to.Time()})
	if err != nil {
		return nil, err
	}
	out := make([]Weight, 0, len(rows))
	for _, r := range rows {
		out = append(out, Weight{Date: DateOf(r.Day), WeightKg: r.WeightKg})
	}
	return out, nil
}

// getBody: GET /v1/health/body?from=&to= (없으면 오늘까지 1년)
func (h *Handler) getBody(w http.ResponseWriter, r *http.Request) error {
	to := h.Svc.today()
	if r.URL.Query().Has("to") {
		d, err := queryDate(r, "to")
		if err != nil {
			return err
		}
		to = d
	}
	from := to.AddDays(-365)
	if r.URL.Query().Has("from") {
		d, err := queryDate(r, "from")
		if err != nil {
			return err
		}
		from = d
	}
	list, err := h.Svc.Weights(r.Context(), from, to)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"weights": list})
	return nil
}

// putBody: PUT /v1/health/body {"date","weightKg"}
func (h *Handler) putBody(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Date     Date     `json:"date"`
		WeightKg *float64 `json:"weightKg"`
	}
	if err := decode(r, &b); err != nil {
		return err
	}
	if b.Date.IsZero() {
		return invalid("date", "필수예요")
	}
	if b.WeightKg == nil {
		return invalid("weightKg", "필수예요")
	}
	out, err := h.Svc.SaveWeight(r.Context(), b.Date, *b.WeightKg)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

// deleteBody: DELETE /v1/health/body?date=
func (h *Handler) deleteBody(w http.ResponseWriter, r *http.Request) error {
	d, err := queryDate(r, "date")
	if err != nil {
		return err
	}
	if _, err := h.Svc.q().DeleteBodyWeight(r.Context(), d.Time()); err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"deleted": d})
	return nil
}

// scanBody: POST /v1/health/body/scan (multipart "photo" 한 장) → {"weightKg","confidence"}. 저장은 하지 않는다(앱이 확인하고 PUT)
func (h *Handler) scanBody(w http.ResponseWriter, r *http.Request) error {
	reader, ok := h.Analyzer.(ai.ScaleReader)
	if !ok {
		return &Error{http.StatusServiceUnavailable, "AI_UNAVAILABLE", "저울 읽기를 쓸 수 없어요 - 직접 입력해 주세요"}
	}
	ctx, cancel := context.WithTimeout(r.Context(), AnalyzeBudget)
	defer cancel()
	release, err := h.acquire(ctx, func(h *Handler) chan struct{} { return h.analyzeSlot })
	if err != nil {
		return err
	}
	defer release()
	photos, err := readPhotos(w, r)
	if err != nil {
		return err
	}
	if len(photos) == 0 {
		return badRequest("INVALID_PHOTO", "사진 파일이 필요해요")
	}
	s, err := reader.ReadScale(ctx, photos[0])
	if err != nil {
		h.Logger.Warn("저울 읽기 실패", "err", err)
		if ai.NoCredit(err) {
			return &Error{http.StatusServiceUnavailable, "AI_NO_CREDIT", "AI 크레딧이 없어요 - 직접 입력해 주세요"}
		}
		return &Error{http.StatusBadGateway, "AI_FAILED", "저울을 읽지 못했어요. 다시 찍거나 직접 입력해 주세요"}
	}
	httpx.JSON(w, http.StatusOK, s)
	return nil
}
