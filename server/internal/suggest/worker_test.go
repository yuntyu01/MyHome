package suggest_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/ai"
	"github.com/yuntyu01/MyHome/server/internal/db"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/ingest"
	"github.com/yuntyu01/MyHome/server/internal/parser"
	"github.com/yuntyu01/MyHome/server/internal/period"
	"github.com/yuntyu01/MyHome/server/internal/pipeline"
	"github.com/yuntyu01/MyHome/server/internal/suggest"
	"github.com/yuntyu01/MyHome/server/internal/testdb"
)

// 이 파일의 알림 문구는 전부 합성 데이터다(실제 형식 아님).

var base = time.Date(2026, 11, 10, 12, 0, 0, 0, period.KST)

type harness struct {
	t    *testing.T
	pool *pgxpool.Pool
	q    *dbq.Queries
	p    *pipeline.Processor
	n    int
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

// event는 원문을 저장하고 처리해서 만들어진 거래를 돌려준다.
func (h *harness) event(pkg, title, text string) dbq.Transaction {
	h.t.Helper()
	ctx := context.Background()
	h.n++
	at := base.Add(time.Duration(h.n) * time.Hour) // 중복 합치기에 걸리지 않게 한 시간씩 띄운다
	req := ingest.Request{SourcePackage: pkg, Title: title, Text: text, PostedAt: at.UnixMilli()}
	raw, err := h.q.InsertRawEvent(ctx, dbq.InsertRawEventParams{SourcePackage: pkg, Title: title, Text: text, PostedAt: at, ContentHash: ingest.ContentHash(req)})
	if err != nil {
		h.t.Fatal(err)
	}
	out, err := h.p.ProcessRawEvent(ctx, raw.ID)
	if err != nil {
		h.t.Fatal(err)
	}
	res := out.(pipeline.Result)
	if res.Transaction == nil {
		h.t.Fatalf("거래가 안 생김: %+v", res)
	}
	tx, err := h.q.GetTransaction(ctx, res.Transaction.ID)
	if err != nil {
		h.t.Fatal(err)
	}
	return tx
}

func (h *harness) kb(merchant string) dbq.Transaction {
	return h.event("test.kb", "KB국민카드", "홍*동님 7,000원 일시불 "+merchant)
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

func (h *harness) expenseNames() []string {
	cats, _ := h.q.ListCategories(context.Background())
	var out []string
	for _, c := range cats {
		if c.Kind == "expense" && !c.Archived {
			out = append(out, c.Name)
		}
	}
	return out
}

func (h *harness) expenseChoices() []ai.CategoryChoice {
	var out []ai.CategoryChoice
	for _, n := range h.expenseNames() {
		out = append(out, ai.CategoryChoice{Name: n})
	}
	return out
}

// lookup은 거래 하나에 보여줄 추천이다.
func (h *harness) lookup(tx dbq.Transaction) *suggest.Suggestion {
	h.t.Helper()
	tx, _ = h.q.GetTransaction(context.Background(), tx.ID)
	out, err := suggest.Lookup(context.Background(), h.q, []suggest.Input{{Status: tx.Status, ReviewReason: tx.ReviewReason, Direction: tx.Direction, Flow: tx.Flow, Merchant: tx.Merchant, CancelsID: tx.CancelsID}})
	if err != nil {
		h.t.Fatal(err)
	}
	return out[0]
}

// fake는 물어본 가맹점을 기록하고 answer로 답하는 분류기다.
type fake struct {
	mu      sync.Mutex
	calls   [][]string
	cats    [][]string
	choices [][]ai.CategoryChoice
	guides  []string
	answer  func(name string) ai.MerchantGuess
	err     func(call int) error // nil이 아닌 에러면 실패(패닉도 여기서)
}

func (f *fake) Model() string { return "fake-model" }

func (f *fake) ClassifyMerchants(_ context.Context, merchants []string, categories []ai.CategoryChoice, guide string) ([]ai.MerchantGuess, error) {
	f.mu.Lock()
	f.guides = append(f.guides, guide)
	f.calls = append(f.calls, append([]string(nil), merchants...))
	var names []string
	for _, c := range categories {
		names = append(names, c.Name)
	}
	f.cats = append(f.cats, names)
	f.choices = append(f.choices, append([]ai.CategoryChoice(nil), categories...))
	n := len(f.calls)
	f.mu.Unlock()
	if f.err != nil {
		if err := f.err(n); err != nil {
			return nil, err
		}
	}
	out := make([]ai.MerchantGuess, len(merchants))
	for i, m := range merchants {
		if f.answer != nil {
			out[i] = f.answer(m)
		}
	}
	return out, nil
}

func (f *fake) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var all []string
	for _, c := range f.calls {
		all = append(all, c...)
	}
	sort.Strings(all)
	return all
}

func (f *fake) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func always(cat string, conf float64) func(string) ai.MerchantGuess {
	return func(string) ai.MerchantGuess { return ai.MerchantGuess{Category: cat, Confidence: conf} }
}

func (h *harness) worker(cl ai.MerchantClassifier) *suggest.Worker {
	return &suggest.Worker{Pool: h.pool, Classifier: cl, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func (h *harness) runOnce(w *suggest.Worker) suggest.Stats {
	h.t.Helper()
	st, err := w.RunOnce(context.Background())
	if err != nil {
		h.t.Fatal(err)
	}
	return st
}

// 가맹점마다 한 번만 묻고(같은 가맹점 거래가 여러 건이어도), 20개씩 묶는다. 답을 저장한 가맹점은 다시 묻지 않는다.
func TestWorkerBatchesAndCachesPerMerchant(t *testing.T) {
	h := newHarness(t)
	var names []string
	for i := range 25 {
		name := "가게" + string(rune('가'+i)) + "점포"
		names = append(names, name)
		h.kb(name)
	}
	h.kb(names[0]) // 같은 가맹점 두 번째 거래
	h.kb(" " + names[1] + " ")
	f := &fake{answer: always("식비", 0.9)}
	w := h.worker(f)
	st := h.runOnce(w)
	if st.Calls != 2 || st.Asked != 25 || st.Suggested != 25 || len(f.calls[0]) != 20 || len(f.calls[1]) != 5 {
		t.Fatalf("묶음: %+v, 호출별 %d/%d", st, len(f.calls[0]), len(f.calls[1]))
	}
	sort.Strings(names)
	if got := f.sent(); !reflect.DeepEqual(got, names) {
		t.Errorf("가맹점마다 한 번: %v", got)
	}
	if !reflect.DeepEqual(f.cats[0], h.expenseNames()) {
		t.Errorf("지출 분류 이름만: %v", f.cats[0])
	}
	if st := h.runOnce(w); st.Calls != 0 {
		t.Errorf("두 번째 점검에서 다시 물음: %+v", st)
	}
	h.kb(names[3]) // 이미 답이 있는 가맹점의 새 거래
	if st := h.runOnce(w); st.Calls != 0 {
		t.Errorf("저장된 가맹점을 다시 물음: %+v", st)
	}
	h.kb("새로생긴곳")
	if st := h.runOnce(w); st.Calls != 1 || !reflect.DeepEqual(f.calls[2], []string{"새로생긴곳"}) {
		t.Errorf("새 가맹점만: %+v %v", st, f.calls)
	}

	// 호출 수 상한: 남은 것은 다음 점검에서
	for i := range 5 {
		h.kb("상한확인" + string(rune('가'+i)))
	}
	w2 := h.worker(f)
	w2.BatchSize, w2.MaxBatches = 2, 2
	if st := h.runOnce(w2); st.Calls != 2 || st.Asked != 4 || st.Left != 1 {
		t.Errorf("상한: %+v", st)
	}
	if st := h.runOnce(w2); st.Calls != 1 || st.Asked != 1 || st.Left != 0 {
		t.Errorf("남은 것: %+v", st)
	}
}

// 규칙 없음(no_rule)·지출·가맹점 있음만 묻는다. 결제대행사 이름뿐인 건, 송금, 입금, 확정된 건, 이름 없는 건은 보내지 않는다.
// 결제대행사 이름이 섞인 가맹점은 파이프라인처럼 떼어낸 이름을 보낸다.
func TestWorkerSendsOnlyNoRuleExpenseMerchants(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	reasonOf := func(tx dbq.Transaction) string {
		if tx.ReviewReason == nil {
			return ""
		}
		return *tx.ReviewReason
	}
	if tx := h.kb("동네분식"); reasonOf(tx) != pipeline.ReviewNoRule {
		t.Fatalf("no_rule 기대: %v", reasonOf(tx))
	}
	if tx := h.kb("카카오페이_골목식당"); reasonOf(tx) != pipeline.ReviewNoRule || tx.Merchant != "카카오페이_골목식당" {
		t.Fatalf("결제대행사+가게 이름: %q %v", tx.Merchant, reasonOf(tx))
	}
	if tx := h.kb("KG이니시스"); reasonOf(tx) != pipeline.ReviewPaymentGateway {
		t.Fatalf("payment_gateway 기대: %v", reasonOf(tx))
	}
	if tx := h.kb("네이버페이"); reasonOf(tx) != pipeline.ReviewPaymentGateway {
		t.Fatalf("payment_gateway 기대: %v", reasonOf(tx))
	}
	if tx := h.event("test.toss", "토스", "김민수님에게 25,000원을 보냈어요"); reasonOf(tx) != pipeline.ReviewTransfer {
		t.Fatalf("transfer 기대: %v", reasonOf(tx))
	}
	if tx := h.event("test.toss", "토스", "박지성님이 30,000원을 보냈어요"); tx.Status != "needs_review" || tx.Flow != "in" {
		t.Fatalf("입금 확인 필요 기대: %s %s", tx.Status, tx.Flow)
	}
	if tx := h.kb("스타벅스강남점"); tx.Status != "confirmed" {
		t.Fatalf("seed 규칙 확정 기대: %s", tx.Status)
	}
	if tx := h.event("test.unknown", "알림", "12,000원"); tx.Status != "needs_review" || reasonOf(tx) == pipeline.ReviewNoRule {
		t.Fatalf("이름 없는 건: %s %v", tx.Status, reasonOf(tx))
	}
	if tx := h.event("test.kb", "KB국민카드 승인취소", "홍*동님 9,900원 어딘가상점"); reasonOf(tx) != pipeline.ReviewCancelNoMatch {
		t.Fatalf("취소 미매칭 기대: %v", reasonOf(tx))
	}
	// 사용자가 이미 분류한 건(no_rule이었다가 확정)도 보내지 않는다
	done := h.kb("이미분류한집")
	cat := h.catID("생활")
	if _, err := (&pipeline.Editor{Pool: h.pool}).EditTransaction(ctx, done.ID, pipeline.Edit{CategoryID: &cat}); err != nil {
		t.Fatal(err)
	}

	// 들어온 돈은 no_rule이어도 묻지 않는다(파이프라인에서는 생기지 않는 조합이라 직접 넣는다)
	noRule := pipeline.ReviewNoRule
	if _, err := h.q.InsertTransaction(ctx, dbq.InsertTransactionParams{OccurredAt: base, Amount: 1000, Flow: "in", Merchant: "환불된가게",
		Direction: "expense", Status: "needs_review", ReviewReason: &noRule, Source: "kb", Kind: "approval"}); err != nil {
		t.Fatal(err)
	}

	f := &fake{answer: always("식비", 0.9)}
	h.runOnce(h.worker(f))
	if got := f.sent(); !reflect.DeepEqual(got, []string{"골목식당", "동네분식"}) {
		t.Fatalf("보낸 가맹점 %v", got)
	}
	// 같은 키라서 결제대행사 없이 온 같은 가게도 같은 추천을 쓴다
	plain := h.kb("골목 식당")
	if s := h.lookup(plain); s == nil || s.Category != "식비" {
		t.Errorf("같은 가게 키: %+v", s)
	}
	if st := h.runOnce(h.worker(f)); st.Calls != 0 {
		t.Errorf("다시 물음 %+v", st)
	}
}

// 실제 Gemini 클라이언트 + 가짜 서버: 요청 본문에는 고정 문구와 분류 이름, 가맹점 이름만 있다(금액·시각·원문·카드 정보 없음).
func TestWorkerGeminiRequestHasOnlyNames(t *testing.T) {
	h := newHarness(t)
	h.event("test.kb", "KB국민카드", "홍*동님 11/10 12:34 7,350원 일시불 동네분식 누적 345,678원 잔액 1,234,567원")
	h.event("test.kb", "KB국민카드", "홍*동님 43,210원 할부(3개월) 골목철물")
	var bodies [][]byte
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, b)
		mu.Unlock()
		// 확인용 답: 0 = 동네분식(최근 거래 순서는 골목철물이 먼저), 이름으로 맞춘다
		var req struct {
			Contents []struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"contents"`
		}
		_ = json.Unmarshal(b, &req)
		text := req.Contents[0].Parts[0].Text
		var items []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}
		_ = json.Unmarshal([]byte(text[strings.Index(text, "[{"):]), &items)
		var results []map[string]any
		for _, it := range items {
			cat := map[string]string{"동네분식": "식비", "골목철물": "생활"}[it.Name]
			results = append(results, map[string]any{"id": it.ID, "category": cat, "confidence": 0.8})
		}
		answer, _ := json.Marshal(map[string]any{"results": results})
		resp, _ := json.Marshal(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": string(answer)}}}}}})
		_, _ = w.Write(resp)
	}))
	defer srv.Close()
	g, err := ai.NewGemini("test-key", "test-model", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	g.WithBase(srv.URL)
	st := h.runOnce(h.worker(g))
	if st.Calls != 1 || st.Suggested != 2 || len(bodies) != 1 {
		t.Fatalf("호출 %+v, 요청 %d", st, len(bodies))
	}
	raw := string(bodies[0])
	var req struct {
		Contents []struct {
			Parts []map[string]any `json:"parts"`
		} `json:"contents"`
		GenerationConfig map[string]any `json:"generationConfig"`
	}
	if err := json.Unmarshal(bodies[0], &req); err != nil {
		t.Fatal(err)
	}
	if len(req.Contents) != 1 || len(req.Contents[0].Parts) != 1 || len(req.Contents[0].Parts[0]) != 1 {
		t.Fatalf("글 조각 하나만: %s", raw)
	}
	// 가맹점 순서는 최근 거래 먼저
	want := ai.MerchantPromptText([]string{"골목철물", "동네분식"}, h.expenseChoices(), "")
	if got := req.Contents[0].Parts[0]["text"]; got != want {
		t.Errorf("프롬프트\n got %q\nwant %q", got, want)
	}
	for _, leak := range []string{"7,350", "7350", "43,210", "43210", "345,678", "1,234,567", "홍*동", "KB국민카드", "일시불", "할부", "11/10", "12:34", "test.kb", "test-key"} {
		if strings.Contains(raw, leak) {
			t.Errorf("요청 본문에 %q가 있음", leak)
		}
	}
	// 요청 본문 키는 contents·generationConfig뿐
	var top map[string]any
	_ = json.Unmarshal(bodies[0], &top)
	if len(top) != 2 {
		t.Errorf("요청 최상위 키 %v", top)
	}
	rows, _ := h.q.ListMerchantSuggestions(context.Background(), []string{"동네분식", "골목철물"})
	if len(rows) != 2 || rows[0].Model != "test-model" {
		t.Errorf("저장 %+v", rows)
	}
}

// 낮은 확신은 저장하지만 보여주지 않는다. "모름"은 추천 없음으로 저장되고 다시 묻지 않는다.
func TestLowConfidenceAndUnknown(t *testing.T) {
	h := newHarness(t)
	low := h.kb("애매한상호")
	unk := h.kb("홍길동")
	sure := h.kb("동네분식")
	f := &fake{answer: func(name string) ai.MerchantGuess {
		switch name {
		case "애매한상호":
			return ai.MerchantGuess{Category: "쇼핑", Confidence: suggest.MinConfidence - 0.01}
		case "동네분식":
			return ai.MerchantGuess{Category: "식비", Confidence: suggest.MinConfidence}
		}
		return ai.MerchantGuess{} // 모름
	}}
	st := h.runOnce(h.worker(f))
	if st.Low != 1 || st.Unknown != 1 || st.Suggested != 1 {
		t.Fatalf("집계 %+v", st)
	}
	if s := h.lookup(low); s != nil {
		t.Errorf("낮은 확신은 숨김: %+v", s)
	}
	if s := h.lookup(unk); s != nil {
		t.Errorf("모름은 추천 없음: %+v", s)
	}
	if s := h.lookup(sure); s == nil || s.Category != "식비" || s.CategoryID != h.catID("식비") || s.Confidence != suggest.MinConfidence {
		t.Errorf("기준값 이상은 보임: %+v", s)
	}
	rows, _ := h.q.ListMerchantSuggestions(context.Background(), []string{"홍길동", "애매한상호"})
	if len(rows) != 2 {
		t.Fatalf("둘 다 저장: %+v", rows)
	}
	for _, r := range rows {
		if r.MerchantKey == "홍길동" && (r.CategoryID != nil || r.Confidence != 0) {
			t.Errorf("모름 저장 %+v", r)
		}
	}
	if st := h.runOnce(h.worker(f)); st.Calls != 0 {
		t.Errorf("모름·낮은 확신을 다시 물음 %+v", st)
	}
	// 거래를 확정하면 추천은 사라진다
	cat := h.catID("식비")
	if _, err := (&pipeline.Editor{Pool: h.pool}).EditTransaction(context.Background(), sure.ID, pipeline.Edit{CategoryID: &cat}); err != nil {
		t.Fatal(err)
	}
	if s := h.lookup(sure); s != nil {
		t.Errorf("확정한 거래에 추천: %+v", s)
	}
}

// 추천한 분류를 보관하면 추천을 숨기고, 다음 점검에서 보관되지 않은 분류로 다시 묻는다.
func TestArchivedCategoryHiddenAndReasked(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tx := h.kb("꽃바구니집")
	f := &fake{answer: func(string) ai.MerchantGuess { return ai.MerchantGuess{Category: "경조사", Confidence: 0.9} }}
	w := h.worker(f)
	h.runOnce(w)
	if s := h.lookup(tx); s == nil || s.Category != "경조사" {
		t.Fatalf("추천 %+v", s)
	}
	gift := h.catID("경조사")
	c, _ := h.q.GetCategory(ctx, gift)
	if _, err := h.q.UpdateCategory(ctx, dbq.UpdateCategoryParams{ID: gift, Name: c.Name, Sort: c.Sort, Archived: true}); err != nil {
		t.Fatal(err)
	}
	if s := h.lookup(tx); s != nil {
		t.Errorf("보관한 분류는 숨김: %+v", s)
	}
	f.answer = func(string) ai.MerchantGuess { return ai.MerchantGuess{Category: "생활", Confidence: 0.7} }
	if st := h.runOnce(w); st.Calls != 1 {
		t.Fatalf("다시 묻기 %+v", st)
	}
	for _, name := range f.cats[1] {
		if name == "경조사" {
			t.Errorf("보관한 분류를 보냄: %v", f.cats[1])
		}
	}
	if s := h.lookup(tx); s == nil || s.Category != "생활" {
		t.Errorf("다시 물은 추천 %+v", s)
	}
	if st := h.runOnce(w); st.Calls != 0 {
		t.Errorf("세 번째 점검 %+v", st)
	}
	// 이름을 바꾼 분류는 새 이름으로 보인다(분류 번호로 저장한다)
	life := h.catID("생활")
	lc, _ := h.q.GetCategory(ctx, life)
	if _, err := h.q.UpdateCategory(ctx, dbq.UpdateCategoryParams{ID: life, Name: "생활용품", Sort: lc.Sort}); err != nil {
		t.Fatal(err)
	}
	if s := h.lookup(tx); s == nil || s.Category != "생활용품" || s.CategoryID != life {
		t.Errorf("이름 바뀐 분류 %+v", s)
	}
	// 모르는 분류 이름 답(분류기가 목록 밖 이름을 줌)은 모름으로 저장.
	// 분류 이름이 바뀌어 목록 지문이 달라졌으니 꽃바구니집도 다시 묻는다.
	h.kb("이상한답가게")
	f.answer = func(string) ai.MerchantGuess { return ai.MerchantGuess{Category: "없는분류", Confidence: 0.99} }
	if st := h.runOnce(w); st.Unknown != 2 || st.Asked != 2 {
		t.Errorf("목록 밖 이름 %+v", st)
	}
}

// 작업은 AI 실패·패닉에도 죽지 않고, 기다렸다가 다시 해서 결국 저장한다. ctx를 끝내면 Run이 돌아온다.
func TestWorkerSurvivesAIErrors(t *testing.T) {
	h := newHarness(t)
	h.kb("동네분식")
	f := &fake{answer: always("식비", 0.9), err: func(call int) error {
		switch call {
		case 1:
			panic("가짜 패닉")
		case 2:
			return errors.New("가짜 503")
		}
		return nil
	}}
	w := h.worker(f)
	w.Interval, w.MinBackoff, w.MaxBackoff = 5*time.Millisecond, 5*time.Millisecond, 20*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		rows, _ := h.q.ListMerchantSuggestions(context.Background(), []string{"동네분식"})
		if len(rows) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("다시 하지 않음: 호출 %d", f.callCount())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if n := f.callCount(); n != 3 {
		t.Errorf("패닉·실패 뒤 세 번째에 성공: %d", n)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ctx를 끝내도 Run이 안 끝남")
	}
}

// 수집으로 no_rule 확인 필요 거래가 생기면 작업을 깨운다(주기가 길어도 바로 묻는다). 확정·다른 사유는 깨우지 않는다.
func TestIngestNotifiesWorker(t *testing.T) {
	h := newHarness(t)
	f := &fake{answer: always("식비", 0.9)}
	w := h.worker(f)
	w.Interval = time.Hour
	var pokes atomic.Int32
	h.p.OnNoRule = func() { pokes.Add(1); w.Notify() }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	h.kb("스타벅스강남점")
	h.kb("KG이니시스")
	if pokes.Load() != 0 {
		t.Fatalf("확정·결제대행사도 깨움: %d", pokes.Load())
	}
	h.kb("동네분식")
	if pokes.Load() != 1 {
		t.Fatalf("no_rule에서 깨우기: %d", pokes.Load())
	}
	deadline := time.Now().Add(10 * time.Second)
	for f.callCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("깨워도 묻지 않음")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Notify는 막지 않는다(작업이 없어도, 여러 번 불러도)
	idle := &suggest.Worker{}
	for range 5 {
		idle.Notify()
	}
}

// 마이그레이션 00008은 내렸다 다시 올릴 수 있다.
func TestMigrationDownUp(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.q.UpsertMerchantSuggestion(ctx, dbq.UpsertMerchantSuggestionParams{MerchantKey: "동네분식", Confidence: 0, Model: "m"}); err != nil {
		t.Fatal(err)
	}
	url := h.pool.Config().ConnString() // 테스트 스키마(search_path)가 들어 있다
	// 00008 아래(7)까지 내린다. 뒤에 마이그레이션이 더 생겨도 같은 것을 본다
	if err := db.Migrate(ctx, url, "down-to", "7"); err != nil {
		t.Fatal(err)
	}
	var exists bool
	_ = h.pool.QueryRow(ctx, "SELECT to_regclass('merchant_suggestions') IS NOT NULL").Scan(&exists)
	if exists {
		t.Fatal("down 뒤에도 표가 있음")
	}
	if err := db.Migrate(ctx, url, "up"); err != nil {
		t.Fatal(err)
	}
	if err := h.q.UpsertMerchantSuggestion(ctx, dbq.UpsertMerchantSuggestionParams{MerchantKey: "동네분식", Confidence: 2, Model: "m"}); err == nil {
		t.Error("confidence 0~1 제약")
	}
}

// 폴더 안 분류는 경로 이름("생활비 › 배달")으로, 설명과 함께 보낸다. 설명을 고치면 같은 가맹점을 다시 묻는다.
// 숨긴 분류로 가는 가게 규칙은 건너뛰어서, 그 가게 결제도 AI 추천 대상이 된다.
func TestFolderPathMemoAndReask(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	food := h.catID("식비")
	fc, _ := h.q.GetCategory(ctx, food)
	life, err := h.q.CreateCategory(ctx, dbq.CreateCategoryParams{Name: "생활비", Kind: "expense", Sort: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.q.UpdateCategory(ctx, dbq.UpdateCategoryParams{ID: food, Name: fc.Name, Sort: fc.Sort, ParentID: &life.ID, Memo: "밥, 배달"}); err != nil {
		t.Fatal(err)
	}
	h.kb("동네분식")
	f := &fake{answer: always("생활비 › 식비", 0.9)}
	w := h.worker(f)
	h.runOnce(w)
	var sent *ai.CategoryChoice
	for i, c := range f.choices[0] {
		if c.Name == "생활비 › 식비" {
			sent = &f.choices[0][i]
		}
		if c.Name == "식비" {
			t.Errorf("폴더 안 분류는 경로 이름으로: %v", f.cats[0])
		}
	}
	if sent == nil || sent.Memo != "밥, 배달" {
		t.Fatalf("설명과 함께 보냄: %+v", f.choices[0])
	}
	if st := h.runOnce(w); st.Calls != 0 {
		t.Fatalf("바뀐 게 없으면 다시 안 물음 %+v", st)
	}
	if _, err := h.q.UpdateCategory(ctx, dbq.UpdateCategoryParams{ID: food, Name: fc.Name, Sort: fc.Sort, ParentID: &life.ID, Memo: "밥, 배달, 편의점"}); err != nil {
		t.Fatal(err)
	}
	if st := h.runOnce(w); st.Calls != 1 || st.Asked != 1 {
		t.Fatalf("설명을 고치면 다시 물음 %+v", st)
	}

	// 스타벅스는 기본 규칙이 카페·간식으로 보낸다. 카페·간식을 숨기면 규칙을 건너뛰고 확인 필요(no_rule)로 간다.
	cafe := h.catID("카페·간식")
	cc, _ := h.q.GetCategory(ctx, cafe)
	if _, err := h.q.UpdateCategory(ctx, dbq.UpdateCategoryParams{ID: cafe, Name: cc.Name, Sort: cc.Sort, Archived: true}); err != nil {
		t.Fatal(err)
	}
	tx := h.kb("스타벅스강남점")
	if tx.Status != "needs_review" || tx.ReviewReason == nil || *tx.ReviewReason != "no_rule" || tx.CategoryID != nil {
		t.Fatalf("숨긴 분류 규칙은 건너뜀: %s %v %v", tx.Status, tx.ReviewReason, tx.CategoryID)
	}
}

// Auto: 확신이 AutoConfidence 이상이면 바로 그 분류로 넣고, 그보다 낮으면 추천만, 모름은 그대로 확인 필요.
// 같은 가맹점의 다음 거래는 AI를 다시 부르지 않고 저장된 답으로 넣는다. 사용자가 먼저 고친 거래는 건드리지 않는다.
func TestAutoFilesConfidentAnswers(t *testing.T) {
	h := newHarness(t)
	sure := h.kb("동네분식")
	maybe := h.kb("애매한상호")
	unk := h.kb("홍길동")
	mine := h.kb("내가고른상호")
	f := &fake{answer: func(name string) ai.MerchantGuess {
		switch name {
		case "동네분식":
			return ai.MerchantGuess{Category: "식비", Confidence: suggest.AutoConfidence}
		case "애매한상호":
			return ai.MerchantGuess{Category: "쇼핑", Confidence: suggest.AutoConfidence - 0.01}
		case "내가고른상호":
			return ai.MerchantGuess{Category: "쇼핑", Confidence: 0.99}
		}
		return ai.MerchantGuess{}
	}}
	// AI가 답하기 전에 사용자가 직접 분류해 둔 거래
	life := h.catID("생활")
	if _, err := (&pipeline.Editor{Pool: h.pool}).EditTransaction(context.Background(), mine.ID, pipeline.Edit{CategoryID: &life}); err != nil {
		t.Fatal(err)
	}
	w := h.worker(f)
	w.Auto = true
	if st := h.runOnce(w); st.Filed != 1 || st.Calls != 1 {
		t.Fatalf("집계 %+v", st)
	}
	get := func(tx dbq.Transaction) dbq.Transaction {
		got, err := h.q.GetTransaction(context.Background(), tx.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := get(sure); got.Status != "confirmed" || got.CategoryID == nil || *got.CategoryID != h.catID("식비") || got.ReviewReason != nil || got.ManuallyEdited {
		t.Errorf("확실한 답은 바로 넣음: %+v", got)
	}
	if got := get(maybe); got.Status != "needs_review" {
		t.Errorf("애매한 답은 확인 필요에 남음: %+v", got)
	} else if s := h.lookup(maybe); s == nil || s.Category != "쇼핑" {
		t.Errorf("애매한 답은 추천으로 보임: %+v", s)
	}
	if got := get(unk); got.Status != "needs_review" || got.CategoryID != nil {
		t.Errorf("모름은 그대로: %+v", got)
	}
	if got := get(mine); got.CategoryID == nil || *got.CategoryID != life {
		t.Errorf("사용자가 고른 분류를 덮음: %+v", got)
	}
	// 학습 규칙은 만들지 않는다
	rules, _ := h.q.ListRules(context.Background())
	for _, r := range rules {
		if r.Pattern == "동네분식" {
			t.Errorf("AI 분류로 규칙이 생김: %+v", r)
		}
	}
	// 같은 가게의 다음 결제: 다시 묻지 않고 넣는다
	again := h.kb("동네분식")
	if st := h.runOnce(w); st.Calls != 0 || st.Filed != 1 {
		t.Fatalf("저장된 답으로 넣기 %+v", st)
	}
	if got := get(again); got.Status != "confirmed" || *got.CategoryID != h.catID("식비") {
		t.Errorf("다음 결제도 바로 들어감: %+v", got)
	}
	// Auto가 꺼진 작업은 추천만 한다
	off := h.kb("동네분식")
	if st := h.runOnce(h.worker(f)); st.Filed != 0 {
		t.Fatalf("Auto 꺼짐인데 넣음 %+v", st)
	}
	if got := get(off); got.Status != "needs_review" {
		t.Errorf("Auto 꺼짐: %+v", got)
	}
}

// 공통 메모(설정 ai_classify_memo)를 AI에 함께 보내고, 메모를 바꾸면 같은 가게를 한 번 다시 묻는다(지운 뒤에도)
func TestWorkerSendsCommonMemoAndReasksWhenItChanges(t *testing.T) {
	h := newHarness(t)
	h.kb("동네분식")
	f := &fake{answer: func(string) ai.MerchantGuess { return ai.MerchantGuess{Category: "식비", Confidence: 0.7} }}
	setMemo := func(v string) {
		raw, _ := json.Marshal(v)
		if err := h.q.UpsertSetting(context.Background(), dbq.UpsertSettingParams{Key: "ai_classify_memo", Value: raw}); err != nil {
			t.Fatal(err)
		}
	}
	setMemo("분식집은 식비로")
	if st := h.runOnce(h.worker(f)); st.Calls != 1 || f.guides[0] != "분식집은 식비로" {
		t.Fatalf("메모를 보내야 함: %+v %q", st, f.guides)
	}
	if st := h.runOnce(h.worker(f)); st.Calls != 0 {
		t.Fatalf("같은 메모면 다시 묻지 않음: %+v", st)
	}
	setMemo("분식집은 외식으로")
	if st := h.runOnce(h.worker(f)); st.Calls != 1 || f.guides[1] != "분식집은 외식으로" {
		t.Fatalf("메모가 바뀌면 다시 물음: %+v %q", st, f.guides)
	}
	setMemo("")
	if st := h.runOnce(h.worker(f)); st.Calls != 1 || f.guides[2] != "" {
		t.Fatalf("메모를 지우면 다시 물음: %+v %q", st, f.guides)
	}
}
