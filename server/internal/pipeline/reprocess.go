package pipeline

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/yuntyu01/MyHome/server/internal/db"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
)

// ReprocessReport는 재처리 결과(또는 계획)다.
type ReprocessReport struct {
	Applied        bool `json:"applied"`
	RawEvents      int  `json:"raw_events"`      // 다시 처리할 원문 수
	SkippedManual  int  `json:"skipped_manual"`  // 사용자가 고친 거래가 있어 건너뛴 원문 수
	DeletedDerived int  `json:"deleted_derived"` // 지우고 다시 만든(만들) 자동 거래 수
	Created        int  `json:"created"`         // 새로 만든 거래 수
	ParseErrors    int  `json:"parse_errors"`    // 해석 실패 원문 수
	Ignored        int  `json:"ignored"`         // 결제 알림이 아니라서 무시한 원문 수
}

// Reprocess는 [from, to) 기간의 원문을 다시 처리한다. 원문(raw_events)은 지우지 않는다.
//
// 사용자가 고친 거래(manually_edited)는 보존하고 그 원문은 건너뛴다. 중복·취소로 연결된 거래는
// 기간 밖이어도 함께 다시 처리해서 상태가 어긋나지 않게 한다. apply=false면 계획만 보여준다.
func (p *Processor) Reprocess(ctx context.Context, from, to time.Time, apply bool) (ReprocessReport, error) {
	var rep ReprocessReport
	err := db.InTx(ctx, p.Pool, func(tx pgx.Tx) error {
		q := dbq.New(tx)
		if err := q.AcquireProcessingLock(ctx); err != nil {
			return err
		}
		raws, err := q.ListRawEventsInRange(ctx, dbq.ListRawEventsInRangeParams{FromAt: from, ToAt: to})
		if err != nil {
			return err
		}
		rawByID := map[int64]dbq.RawEvent{}
		for _, r := range raws {
			rawByID[r.ID] = r
		}

		// 다시 만들 거래 집합(사용자가 고치지 않은 것)과, 연결된 거래까지 확장
		derived := map[int64]dbq.Transaction{}
		txs, err := q.ListTransactionsForReprocess(ctx, keys(rawByID))
		if err != nil {
			return err
		}
		for _, t := range txs {
			derived[t.ID] = t
		}
		for {
			linked, err := q.ListLinkedTransactions(ctx, keys(derived))
			if err != nil {
				return err
			}
			added := false
			for _, l := range linked {
				if _, ok := derived[l.ID]; ok || l.ManuallyEdited || l.RawEventID == nil {
					continue
				}
				derived[l.ID] = l
				added = true
				if _, ok := rawByID[*l.RawEventID]; !ok {
					r, err := q.GetRawEvent(ctx, *l.RawEventID)
					if err != nil {
						return err
					}
					rawByID[r.ID] = r
				}
			}
			if !added {
				break
			}
		}

		// 사용자가 고친 거래가 달린 원문은 건너뛴다.
		var todo []dbq.RawEvent
		for _, r := range rawByID {
			all, err := q.ListTransactionsByRawEvent(ctx, &r.ID)
			if err != nil {
				return err
			}
			manual := false
			for _, t := range all {
				if t.ManuallyEdited {
					manual = true
				}
			}
			if manual {
				rep.SkippedManual++
				continue
			}
			todo = append(todo, r)
		}
		sort.Slice(todo, func(i, j int) bool {
			if !todo[i].PostedAt.Equal(todo[j].PostedAt) {
				return todo[i].PostedAt.Before(todo[j].PostedAt)
			}
			return todo[i].ID < todo[j].ID
		})
		rep.RawEvents = len(todo)
		rep.DeletedDerived = len(derived)
		if !apply {
			return nil
		}

		ids := keys(derived)
		if err := q.DetachLinks(ctx, ids); err != nil {
			return err
		}
		if _, err := q.DeleteDerivedTransactions(ctx, ids); err != nil {
			return err
		}
		todoIDs := make([]int64, len(todo))
		for i, r := range todo {
			todoIDs[i] = r.ID
		}
		if err := q.ResetRawEventProcessing(ctx, todoIDs); err != nil {
			return err
		}
		e, err := loadEnv(ctx, q)
		if err != nil {
			return err
		}
		for _, r := range todo {
			r.ProcessedAt, r.ParseError = nil, nil
			res, err := p.processLocked(ctx, q, e, r)
			if err != nil {
				return err
			}
			if res.Transaction != nil {
				rep.Created++
			}
			if res.ParseError != "" {
				rep.ParseErrors++
			}
			if res.Ignored != "" {
				rep.Ignored++
			}
		}
		rep.Applied = true
		return nil
	})
	return rep, err
}

// ProcessPending은 아직 처리되지 않은 원문(서버가 처리 도중 죽었거나 처리 오류가 난 경우)을 처리한다.
// 한 건이 실패해도 나머지는 계속 처리하고, 실패 건수와 첫 오류를 돌려준다.
func (p *Processor) ProcessPending(ctx context.Context, limit int) (int, error) {
	raws, err := dbq.New(p.Pool).ListUnprocessedRawEvents(ctx, int32(limit))
	if err != nil {
		return 0, err
	}
	n, failed := 0, 0
	var first error
	for _, r := range raws {
		if ctx.Err() != nil {
			return n, ctx.Err()
		}
		if _, err := p.ProcessRawEvent(ctx, r.ID); err != nil {
			failed++
			if first == nil {
				first = fmt.Errorf("원문 #%d: %w", r.ID, err)
			}
			continue
		}
		n++
	}
	if first != nil {
		return n, fmt.Errorf("미처리 원문 %d건 실패: %w", failed, first)
	}
	return n, nil
}

// RunPending은 every마다 미처리 원문을 다시 처리한다. 수집 때 처리에 실패한 원문(응답은 201)이
// 서버를 다시 띄울 때까지 남지 않게 한다. ctx가 끝나면 돌아온다.
func (p *Processor) RunPending(ctx context.Context, every time.Duration, limit int) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		n, err := p.ProcessPending(ctx, limit)
		if err != nil && ctx.Err() == nil {
			p.log().Warn("미처리 원문 재처리 실패", "processed", n, "err", err)
		} else if n > 0 {
			p.log().Info("미처리 원문 재처리", "count", n)
		}
	}
}

func keys[V any](m map[int64]V) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
