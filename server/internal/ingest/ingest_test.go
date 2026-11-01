package ingest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/auth"
	"github.com/yuntyu01/MyHome/server/internal/config"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/ingest"
	"github.com/yuntyu01/MyHome/server/internal/server"
	"github.com/yuntyu01/MyHome/server/internal/testdb"
)

// 합성 데이터: 실제 알림 형식이 아니다.
func sampleEvent() map[string]any {
	return map[string]any{
		"source_package": "test.synthetic.card",
		"title":          "합성카드 승인",
		"text":           "12,345원 일시불 테스트가맹점",
		"big_text":       "",
		"posted_at":      time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC).UnixMilli(),
	}
}

func newServer(t *testing.T) (http.Handler, *dbq.Queries, string) {
	t.Helper()
	pool := testdb.New(t)
	q := dbq.New(pool)
	tok, _, err := auth.IssueDeviceToken(context.Background(), q, "test-phone")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{MaxBodyBytes: 64 * 1024, RateLimitRPS: 1000, RateLimitBurst: 1000}
	h := server.New(server.Deps{Config: cfg, Pool: pool, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	return h, q, tok
}

func post(t *testing.T, h http.Handler, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) ingest.Response {
	t.Helper()
	var r ingest.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("응답 JSON 해석 실패: %v (%s)", err, rec.Body.String())
	}
	return r
}

func TestPostEvent_SavesRawEvent(t *testing.T) {
	h, q, tok := newServer(t)
	rec := post(t, h, tok, sampleEvent())
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	resp := decode(t, rec)
	if resp.Duplicate || resp.ID == 0 {
		t.Fatalf("응답 이상: %+v", resp)
	}
	ev, err := q.GetRawEvent(context.Background(), resp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Text != "12,345원 일시불 테스트가맹점" || ev.SourcePackage != "test.synthetic.card" || ev.DeviceID == nil {
		t.Fatalf("저장 값 이상: %+v", ev)
	}
	if !ev.PostedAt.Equal(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("posted_at = %v", ev.PostedAt)
	}
}

func TestPostEvent_DuplicateIsIdempotent(t *testing.T) {
	h, _, tok := newServer(t)
	first := decode(t, post(t, h, tok, sampleEvent()))

	rec := post(t, h, tok, sampleEvent())
	if rec.Code != http.StatusOK {
		t.Fatalf("재전송 status = %d, want 200", rec.Code)
	}
	second := decode(t, rec)
	if !second.Duplicate || second.ID != first.ID {
		t.Fatalf("중복 처리 이상: first=%+v second=%+v", first, second)
	}

	// 게시 시각이 다르면 같은 내용이라도 별개 이벤트다(같은 가게 두 번 결제).
	ev := sampleEvent()
	ev["posted_at"] = ev["posted_at"].(int64) + 1000
	third := decode(t, post(t, h, tok, ev))
	if third.Duplicate || third.ID == first.ID {
		t.Fatalf("다른 시각 이벤트가 중복 처리됨: %+v", third)
	}
}

func TestPostEvent_AuthFailures(t *testing.T) {
	h, q, tok := newServer(t)

	if rec := post(t, h, "", sampleEvent()); rec.Code != http.StatusUnauthorized {
		t.Errorf("토큰 없음: status = %d", rec.Code)
	}
	if rec := post(t, h, "mhd_wrongtoken", sampleEvent()); rec.Code != http.StatusUnauthorized {
		t.Errorf("틀린 토큰: status = %d", rec.Code)
	}

	devs, _ := q.ListDevices(context.Background())
	if _, err := q.RevokeDevice(context.Background(), devs[0].ID); err != nil {
		t.Fatal(err)
	}
	if rec := post(t, h, tok, sampleEvent()); rec.Code != http.StatusUnauthorized {
		t.Errorf("폐기된 토큰: status = %d", rec.Code)
	}
}

func TestPostEvent_Validation(t *testing.T) {
	h, _, tok := newServer(t)
	ev := sampleEvent()
	delete(ev, "source_package")
	if rec := post(t, h, tok, ev); rec.Code != http.StatusBadRequest {
		t.Errorf("패키지 없음: status = %d", rec.Code)
	}
	big := sampleEvent()
	big["big_text"] = string(bytes.Repeat([]byte("가"), 70*1024))
	if rec := post(t, h, tok, big); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("큰 본문: status = %d", rec.Code)
	}
}

func TestContentHashStable(t *testing.T) {
	r := ingest.Request{SourcePackage: "a", Title: "b", Text: "c", PostedAt: 1}
	if ingest.ContentHash(r) != ingest.ContentHash(r) {
		t.Fatal("해시가 매번 달라짐")
	}
	// 필드 경계가 섞여도 다른 해시가 나와야 한다.
	r2 := ingest.Request{SourcePackage: "a", Title: "bc", Text: "", PostedAt: 1}
	if ingest.ContentHash(r) == ingest.ContentHash(r2) {
		t.Fatal("필드 경계 충돌")
	}
}
