package suggest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/ai"
	"github.com/yuntyu01/MyHome/server/internal/cattree"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/pipeline"
)

// 기본값. Worker의 같은 이름 필드가 0이면 쓴다.
const (
	DefaultInterval    = time.Minute      // 주기 점검(알림 신호를 놓쳐도 이 간격으로 다시 본다)
	DefaultMaxBatches  = 10               // 한 번 점검에서 부르는 AI 호출 수 상한(남은 것은 다음 점검에)
	DefaultCallTimeout = 45 * time.Second // AI 호출 하나(+저장)의 제한 시간
	DefaultMinBackoff  = time.Minute      // 실패 뒤 첫 기다림, 실패할 때마다 두 배
	DefaultMaxBackoff  = 30 * time.Minute
	// candidateLimit은 한 번에 읽는 대상 가맹점 수다. 개인 가계부의 확인 필요 큐는 이보다 훨씬 작다.
	candidateLimit = 1000
)

// Worker는 `myhome serve` 안에서 도는 AI 분류 추천 작업이다. 수집은 기다리지 않는다(수집 API는 위젯 때문에 동기 처리 그대로):
// 거래가 no_rule 확인 필요로 들어오면 Notify로 깨우고(막지 않는 신호), 신호가 없어도 Interval마다 한 번 본다.
// 아직 답이 없는 가맹점만 BatchSize개씩 묶어서 묻고, 답(모름 포함)을 가맹점마다 저장한다.
// AI 실패·패닉은 기록하고 기다렸다가(두 배씩, 최대 MaxBackoff) 다시 한다. serve를 멈추지 않는다.
type Worker struct {
	Pool       *pgxpool.Pool
	Classifier ai.MerchantClassifier
	Logger     *slog.Logger

	Interval    time.Duration
	BatchSize   int // 0이거나 ai.MaxMerchantsPerCall보다 크면 ai.MaxMerchantsPerCall(20)
	MaxBatches  int
	CallTimeout time.Duration
	MinBackoff  time.Duration
	MaxBackoff  time.Duration

	once sync.Once
	wake chan struct{}
}

// Stats는 한 번 점검한 결과(건수만)다. 로그에는 이것만 남기고 가맹점 이름은 남기지 않는다.
type Stats struct {
	Calls     int // AI 호출 수
	Asked     int // 물어본 가맹점 수
	Suggested int // 보여줄 만한 추천(MinConfidence 이상)
	Low       int // 저장했지만 확신이 낮아 숨기는 추천
	Unknown   int // 모름
	Left      int // 호출 상한 때문에 다음 점검으로 넘긴 가맹점 수
}

func (w *Worker) init() { w.once.Do(func() { w.wake = make(chan struct{}, 1) }) }

// Notify는 작업을 깨운다. 막지 않는다(이미 깨울 신호가 쌓여 있으면 그냥 돌아간다). Processor.OnNoRule에 건다.
func (w *Worker) Notify() {
	w.init()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *Worker) log() *slog.Logger {
	if w.Logger == nil {
		return slog.Default()
	}
	return w.Logger
}

func orDur(v, def time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return def
}

func (w *Worker) batchSize() int {
	if w.BatchSize <= 0 || w.BatchSize > ai.MaxMerchantsPerCall {
		return ai.MaxMerchantsPerCall
	}
	return w.BatchSize
}

// Run은 ctx가 끝날 때까지 돈다(serve 종료 신호로 끝난다). 시작하자마자 한 번 본다(꺼져 있던 동안 쌓인 것).
func (w *Worker) Run(ctx context.Context) {
	w.init()
	ticker := time.NewTicker(orDur(w.Interval, DefaultInterval))
	defer ticker.Stop()
	minB, maxB := orDur(w.MinBackoff, DefaultMinBackoff), orDur(w.MaxBackoff, DefaultMaxBackoff)
	var backoff time.Duration
	var notBefore time.Time
	for {
		if ctx.Err() != nil {
			return
		}
		if !time.Now().Before(notBefore) {
			st, err := w.safeRunOnce(ctx)
			switch {
			case ctx.Err() != nil:
				return
			case err != nil:
				backoff = min(max(backoff*2, minB), maxB)
				notBefore = time.Now().Add(backoff)
				w.log().Warn("AI 분류 추천 실패, 나중에 다시 함", "err", err, "retry_in", backoff.String(), "asked", st.Asked, "calls", st.Calls)
			default:
				backoff, notBefore = 0, time.Time{}
				if st.Calls > 0 {
					w.log().Info("AI 분류 추천", "calls", st.Calls, "asked", st.Asked, "suggested", st.Suggested, "low", st.Low, "unknown", st.Unknown, "left", st.Left)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-w.wake:
		}
	}
}

// safeRunOnce는 RunOnce의 패닉을 에러로 바꾼다(작업이 죽어도 serve는 계속).
func (w *Worker) safeRunOnce(ctx context.Context) (st Stats, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("패닉: %v", r)
		}
	}()
	return w.RunOnce(ctx)
}

type target struct{ name, key string }

// RunOnce는 한 번 점검한다: 답이 없는 가맹점(또는 추천한 분류가 보관된 가맹점)을 모아 묻고 저장한다.
// 호출이 하나라도 실패하면 거기서 멈추고 에러를 돌려준다(그 전에 받은 답은 저장돼 있다).
func (w *Worker) RunOnce(ctx context.Context) (Stats, error) {
	var st Stats
	if w.Classifier == nil {
		return st, errors.New("분류기가 없음")
	}
	q := dbq.New(w.Pool)
	cfg, err := pipeline.LoadConfig(ctx, q)
	if err != nil {
		return st, err
	}
	cats, err := q.ListCategories(ctx)
	if err != nil {
		return st, err
	}
	names, byName := Choices(cats)
	if len(names) == 0 {
		return st, nil
	}
	catalog := Catalog(names)

	rows, err := q.ListSuggestionMerchants(ctx, candidateLimit)
	if err != nil {
		return st, err
	}
	var targets []target
	seen := map[string]bool{}
	for _, r := range rows {
		name, key, ok := Target(r.Merchant, cfg.ReviewMerchantPatterns)
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		targets = append(targets, target{name, key})
	}
	if len(targets) == 0 {
		return st, nil
	}
	keys := make([]string, len(targets))
	for i, t := range targets {
		keys[i] = t.key
	}
	cached, err := q.ListMerchantSuggestions(ctx, keys)
	if err != nil {
		return st, err
	}
	done := map[string]bool{}
	for _, c := range cached {
		// 추천한 분류가 보관(삭제)됐으면 다시 묻는다. 새 질문은 보관되지 않은 분류만 보내므로 같은 답이 반복되지 않는다.
		// 분류 목록(폴더·이름·설명)이 물을 때와 달라졌어도 다시 묻는다. 설명을 고치면 추천도 따라 바뀐다.
		stale := c.CategoryID != nil && (c.CategoryArchived == nil || *c.CategoryArchived) || c.Catalog != catalog
		if !stale {
			done[c.MerchantKey] = true
		}
	}
	var todo []target
	for _, t := range targets {
		if !done[t.key] {
			todo = append(todo, t)
		}
	}

	size, maxBatches := w.batchSize(), w.MaxBatches
	if maxBatches <= 0 {
		maxBatches = DefaultMaxBatches
	}
	for start := 0; start < len(todo); start += size {
		if st.Calls >= maxBatches {
			st.Left = len(todo) - start
			break
		}
		chunk := todo[start:min(start+size, len(todo))]
		if err := w.ask(ctx, q, chunk, names, byName, catalog, &st); err != nil {
			return st, err
		}
	}
	return st, nil
}

// ask는 가맹점 한 묶음을 묻고 답을 저장한다.
func (w *Worker) ask(ctx context.Context, q *dbq.Queries, chunk []target, names []ai.CategoryChoice, byName map[string]int64, catalog string, st *Stats) error {
	ctx, cancel := context.WithTimeout(ctx, orDur(w.CallTimeout, DefaultCallTimeout))
	defer cancel()
	merchants := make([]string, len(chunk))
	for i, t := range chunk {
		merchants[i] = t.name
	}
	st.Calls++
	guesses, err := w.Classifier.ClassifyMerchants(ctx, merchants, names)
	if err != nil {
		return err
	}
	if len(guesses) != len(chunk) {
		return fmt.Errorf("답 개수가 다름: %d개 물어 %d개", len(chunk), len(guesses))
	}
	model := w.Classifier.Model()
	for i, g := range guesses {
		var cid *int64
		conf := g.Confidence
		if id, ok := byName[g.Category]; ok && g.Category != "" {
			cid = &id
		}
		switch {
		case cid == nil:
			conf = 0
			st.Unknown++
		case conf < MinConfidence:
			st.Low++
		default:
			st.Suggested++
		}
		conf = min(max(conf, 0), 1)
		if err := q.UpsertMerchantSuggestion(ctx, dbq.UpsertMerchantSuggestionParams{MerchantKey: chunk[i].key, CategoryID: cid, Confidence: conf, Model: model, Catalog: catalog}); err != nil {
			return err
		}
		st.Asked++
	}
	return nil
}

// Choices는 AI에 보낼 분류 목록이다: 숨기지 않은 지출 분류를 폴더 순서대로, 이름은 폴더 경로("생활비 › 식비"), 설명은 사용자 메모.
// byName은 경로 이름 → 분류 id다. "모름"이라는 이름은 AI의 "모름"과 구분할 수 없어서 보내지 않는다.
func Choices(cats []dbq.Category) ([]ai.CategoryChoice, map[string]int64) {
	t := cattree.New(cats)
	var out []ai.CategoryChoice
	byName := map[string]int64{}
	for _, c := range t.Preorder() {
		if c.Kind != "expense" || c.Archived {
			continue
		}
		label := t.Label(c.ID)
		if label == ai.UnknownCategory {
			continue
		}
		out = append(out, ai.CategoryChoice{Name: label, Memo: c.Memo})
		byName[label] = c.ID
	}
	return out, byName
}

// Catalog는 분류 목록의 지문이다. 저장된 추천의 지문이 지금과 다르면 그 가맹점을 다시 묻는다.
func Catalog(choices []ai.CategoryChoice) string {
	h := sha256.New()
	for _, c := range choices {
		h.Write([]byte(c.Name + "\x00" + c.Memo + "\x00"))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
