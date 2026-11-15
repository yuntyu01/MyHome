package notice_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/ai"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/ingest"
	"github.com/yuntyu01/MyHome/server/internal/notice"
	"github.com/yuntyu01/MyHome/server/internal/parser"
	"github.com/yuntyu01/MyHome/server/internal/period"
	"github.com/yuntyu01/MyHome/server/internal/pipeline"
	"github.com/yuntyu01/MyHome/server/internal/suggest"
	"github.com/yuntyu01/MyHome/server/internal/testdb"
)

// 이 파일의 알림 문구는 전부 합성 데이터다(실제 형식 아님).

var base = time.Date(2026, 11, 15, 12, 0, 0, 0, period.KST)

type harness struct {
	t    *testing.T
	pool *pgxpool.Pool
	q    *dbq.Queries
	p    *pipeline.Processor
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := testdb.New(t)
	reg, err := parser.NewRegistry("kb=test.kb;toss=test.toss")
	if err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, pool: pool, q: dbq.New(pool),
		p: &pipeline.Processor{Pool: pool, Registry: reg, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
}

// event는 원문을 저장하고 처리한 뒤 원문 id와 처리 결과를 돌려준다.
func (h *harness) event(pkg, title, text string, at time.Time) (int64, pipeline.Result) {
	h.t.Helper()
	ctx := context.Background()
	req := ingest.Request{SourcePackage: pkg, Title: title, Text: text, PostedAt: at.UnixMilli()}
	raw, err := h.q.InsertRawEvent(ctx, dbq.InsertRawEventParams{SourcePackage: pkg, Title: title, Text: text, PostedAt: at, ContentHash: ingest.ContentHash(req)})
	if err != nil {
		h.t.Fatal(err)
	}
	out, err := h.p.ProcessRawEvent(ctx, raw.ID)
	if err != nil {
		h.t.Fatal(err)
	}
	return raw.ID, out.(pipeline.Result)
}

func (h *harness) state(c notice.Checker, id int64) string {
	h.t.Helper()
	s, err := c.State(context.Background(), h.q, id)
	if err != nil {
		h.t.Fatal(err)
	}
	return s
}

func (h *harness) catID(name string) int64 {
	cats, _ := h.q.ListCategories(context.Background())
	for _, c := range cats {
		if c.Name == name {
			return c.ID
		}
	}
	h.t.Fatalf("분류 없음 %s", name)
	return 0
}

// guesses는 가맹점 이름마다 정해 둔 답을 주는 분류기다(모르는 이름은 "모름").
type guesses map[string]ai.MerchantGuess

func (g guesses) Model() string { return "fake" }

func (g guesses) ClassifyMerchants(_ context.Context, merchants []string, _ []ai.CategoryChoice, _ string) ([]ai.MerchantGuess, error) {
	out := make([]ai.MerchantGuess, len(merchants))
	for i, m := range merchants {
		out[i] = g[m]
	}
	return out, nil
}

// 알아본 결제(규칙 분류·지출에서 뺀 것·중복으로 합친 것)와 테스트 알림만 지운다.
// 읽지 못한 알림·결제가 아닌 알림·송금(사람이 고를 것)·없는 id는 남긴다. 남긴 송금도 사용자가 분류하면 지운다.
func TestKnownOnly(t *testing.T) {
	h := newHarness(t)
	c := notice.Checker{AI: true}
	rule, _ := h.event("test.kb", "KB국민카드", "홍*동님 12,345원 일시불 스타벅스강남점", base)
	card, _ := h.event("test.toss", "토스뱅크", "출금 812,340원 | KB국민카드 결제대금", base.Add(time.Hour))
	toss, _ := h.event("test.toss", "토스페이", "스타벅스 역삼점에서 5,600원 결제했어요", base.Add(2*time.Hour))
	dup, res := h.event("test.kb", "KB국민카드", "홍*동님 5,600원 일시불 토스페이먼츠", base.Add(2*time.Hour+70*time.Second))
	if res.Transaction == nil || res.Transaction.MergedInto == nil {
		t.Fatalf("중복으로 합쳐져야 함: %+v", res.Transaction)
	}
	failed, res := h.event("test.toss", "토스", "결제 내역을 확인해 주세요", base.Add(3*time.Hour))
	if res.ParseError == "" {
		t.Fatalf("해석 실패 기대: %+v", res)
	}
	ad, res := h.event("test.toss", "토스", "스타벅스에서 결제하면 1,000원 적립해드려요", base.Add(4*time.Hour))
	if res.Ignored == "" {
		t.Fatalf("무시 기대: %+v", res)
	}
	send, res := h.event("test.toss", "토스", "김민수님에게 25,000원을 보냈어요", base.Add(5*time.Hour))
	test, _ := h.event("test.kb", pipeline.TestTitleMarker+" 알림", "합성 1,000원", base.Add(6*time.Hour))

	for name, tc := range map[string]struct {
		id   int64
		want string
	}{
		"규칙 분류": {rule, notice.Dismiss}, "카드대금": {card, notice.Dismiss}, "중복 대표": {toss, notice.Dismiss}, "중복": {dup, notice.Dismiss},
		"해석 실패": {failed, notice.Keep}, "광고": {ad, notice.Keep}, "송금": {send, notice.Keep}, "테스트 알림": {test, notice.Dismiss},
		"없는 id": {999999, notice.Keep},
	} {
		if got := h.state(c, tc.id); got != tc.want {
			t.Errorf("%s: %s, 기대 %s", name, got, tc.want)
		}
	}

	// 사용자가 송금을 분류하면 지운다
	food := h.catID("식비")
	if _, err := (&pipeline.Editor{Pool: h.pool}).EditTransaction(context.Background(), res.Transaction.ID, pipeline.Edit{CategoryID: &food}); err != nil {
		t.Fatal(err)
	}
	if got := h.state(c, send); got != notice.Dismiss {
		t.Errorf("분류한 송금: %s", got)
	}
}

// 규칙이 없는 지출: AI 판정 전엔 기다리고, 확신해서 넣으면 지우고, 모름·애매하면 남긴다. AI가 꺼져 있으면 바로 남긴다.
func TestWaitsForAI(t *testing.T) {
	h := newHarness(t)
	on, off := notice.Checker{AI: true}, notice.Checker{AI: false}
	sure, _ := h.event("test.kb", "KB국민카드", "홍*동님 7,000원 일시불 동네분식", base)
	maybe, _ := h.event("test.kb", "KB국민카드", "홍*동님 8,000원 일시불 애매한상호", base.Add(time.Hour))
	unk, _ := h.event("test.kb", "KB국민카드", "홍*동님 9,000원 일시불 처음보는곳", base.Add(2*time.Hour))
	for _, id := range []int64{sure, maybe, unk} {
		if got := h.state(on, id); got != notice.Wait {
			t.Errorf("AI 답 전 %d: %s", id, got)
		}
		if got := h.state(off, id); got != notice.Keep {
			t.Errorf("AI 꺼짐 %d: %s", id, got)
		}
	}

	g := guesses{
		"동네분식":  {Category: "식비", Confidence: suggest.AutoConfidence},
		"애매한상호": {Category: "쇼핑", Confidence: suggest.AutoConfidence - 0.01},
	}
	// 답은 저장됐지만 아직 넣기 전(Auto 꺼짐): 확신한 답은 곧 들어가므로 기다린다
	w := &suggest.Worker{Pool: h.pool, Classifier: g, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := h.state(on, sure); got != notice.Wait {
		t.Errorf("확신했지만 넣기 전: %s", got)
	}
	w.Auto = true
	if _, err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		id   int64
		want string
	}{"확신": {sure, notice.Dismiss}, "애매": {maybe, notice.Keep}, "모름": {unk, notice.Keep}} {
		if got := h.state(on, tc.id); got != tc.want {
			t.Errorf("%s: %s, 기대 %s", name, got, tc.want)
		}
	}
}
