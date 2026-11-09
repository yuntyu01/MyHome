// Package focus는 집중(공부 타이머, 열품타 비슷한 것) 기록 API다.
//
// 앱이 먼저 자기 DB(Room)에 저장하고(local-first), 앱이 만든 id(UUID)로 PUT해서 올린다. 같은 PUT을 여러 번 보내도 된다.
// 서버는 백업(재설치 복원, GET /v1/focus/all)이자 나중에 웹 통계용이다. 하루는 05:00 KST에 바뀐다(day.go).
package focus

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/db"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
)

// Service는 집중 기록 저장소다(사용자 1명).
type Service struct {
	Pool *pgxpool.Pool
}

func (s *Service) q() *dbq.Queries { return dbq.New(s.Pool) }

func (s *Service) tx(ctx context.Context, fn func(*dbq.Queries) error) error {
	return db.InTx(ctx, s.Pool, func(tx pgx.Tx) error { return fn(dbq.New(tx)) })
}

// ── 쓰기 ──────────────────────────────────────────────────────────────
// 모두 last-write-wins다: 저장된 updatedAt이 보낸 것보다 새로우면 바꾸지 않고 저장된 값을 돌려준다(오류 아님).
// updatedAt이 같으면 덮어쓴다(같은 PUT을 다시 보내도 결과가 같다).

// PutSubject는 과목을 넣거나 바꾸고 저장된 값을 돌려준다.
func (s *Service) PutSubject(ctx context.Context, id string, in SubjectInput) (Subject, error) {
	if err := in.normalize(); err != nil {
		return Subject{}, err
	}
	var out Subject
	err := s.tx(ctx, func(q *dbq.Queries) error {
		if err := q.UpsertFocusSubject(ctx, dbq.UpsertFocusSubjectParams{ID: id, Name: in.Name, Color: in.Color,
			Sort: in.Sort, Archived: in.Archived, UpdatedAt: in.UpdatedAt}); err != nil {
			return err
		}
		r, err := q.GetFocusSubject(ctx, id)
		out = subjectOf(r)
		return err
	})
	return out, err
}

// PutSession은 공부 세션을 넣거나 바꾸고 저장된 값을 돌려준다. 과목이 서버에 없으면 400 UNKNOWN_SUBJECT
// (앱은 과목을 먼저 올린다).
func (s *Service) PutSession(ctx context.Context, id string, in SessionInput) (Session, error) {
	if err := in.normalize(); err != nil {
		return Session{}, err
	}
	var out Session
	err := s.tx(ctx, func(q *dbq.Queries) error {
		ok, err := q.FocusSubjectExists(ctx, in.SubjectID)
		if err != nil {
			return err
		}
		if !ok {
			return badRequest(CodeUnknownSubject, "subjectId: 서버에 없는 과목이에요. 과목을 먼저 올려 주세요")
		}
		if err := q.UpsertFocusSession(ctx, dbq.UpsertFocusSessionParams{ID: id, SubjectID: in.SubjectID,
			StartedAt: in.StartedAt, EndedAt: in.EndedAt, Deleted: in.Deleted, UpdatedAt: in.UpdatedAt}); err != nil {
			return err
		}
		r, err := q.GetFocusSession(ctx, id)
		out = sessionOf(r)
		return err
	})
	return out, err
}

// PutStay는 장소 머무름을 넣거나 바꾸고 저장된 값을 돌려준다.
func (s *Service) PutStay(ctx context.Context, id string, in StayInput) (Stay, error) {
	if err := in.normalize(); err != nil {
		return Stay{}, err
	}
	var out Stay
	err := s.tx(ctx, func(q *dbq.Queries) error {
		if err := q.UpsertFocusStay(ctx, dbq.UpsertFocusStayParams{ID: id, Place: in.Place,
			EnteredAt: in.EnteredAt, ExitedAt: in.ExitedAt, Deleted: in.Deleted, UpdatedAt: in.UpdatedAt}); err != nil {
			return err
		}
		r, err := q.GetFocusStay(ctx, id)
		out = stayOf(r)
		return err
	})
	return out, err
}

// ── 읽기 ──────────────────────────────────────────────────────────────

// Subjects는 모든 과목이다(숨긴 것 포함, sort·name 순).
func (s *Service) Subjects(ctx context.Context) ([]Subject, error) {
	return listSubjects(ctx, s.q())
}

func listSubjects(ctx context.Context, q *dbq.Queries) ([]Subject, error) {
	rows, err := q.ListFocusSubjects(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Subject, 0, len(rows))
	for _, r := range rows {
		out = append(out, subjectOf(r))
	}
	return out, nil
}

// All은 GET /v1/focus/all 응답이다.
type All struct {
	Subjects []Subject `json:"subjects"`
	Sessions []Session `json:"sessions"`
	Stays    []Stay    `json:"stays"`
}

// All은 지운 것·숨긴 것까지 모든 기록이다(앱 재설치 복원용). 사용자 1명이라 작아서 나눠 주지 않는다.
// 세 목록이 같은 시점이 되게 읽기 전용 REPEATABLE READ 트랜잭션에서 읽는다(세션의 과목이 목록에 꼭 있게).
func (s *Service) All(ctx context.Context) (All, error) {
	var out All
	err := pgx.BeginTxFunc(ctx, s.Pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		q := dbq.New(tx)
		var err error
		if out.Subjects, err = listSubjects(ctx, q); err != nil {
			return err
		}
		sessions, err := q.ListFocusSessions(ctx)
		if err != nil {
			return err
		}
		out.Sessions = make([]Session, 0, len(sessions))
		for _, r := range sessions {
			out.Sessions = append(out.Sessions, sessionOf(r))
		}
		stays, err := q.ListFocusStays(ctx)
		if err != nil {
			return err
		}
		out.Stays = make([]Stay, 0, len(stays))
		for _, r := range stays {
			out.Stays = append(out.Stays, stayOf(r))
		}
		return nil
	})
	return out, err
}

// Days는 from~to(포함, 최대 MaxDays일) 공부 날짜별 합계다. 지운 기록은 빼고, 기록 없는 날도 0으로 넣는다.
func (s *Service) Days(ctx context.Context, from, to string) ([]Day, error) {
	dates, err := DateRange(from, to)
	if err != nil {
		return nil, err
	}
	start, _, err := DayBounds(dates[0])
	if err != nil {
		return nil, err
	}
	_, end, err := DayBounds(dates[len(dates)-1])
	if err != nil {
		return nil, err
	}
	// 기록 하나는 MaxDuration(24시간)까지라 창 시작 24시간 전부터 시작한 것만 보면 된다
	since := start.Add(-MaxDuration)
	q := s.q()
	srows, err := q.ListFocusSessionsOverlapping(ctx, dbq.ListFocusSessionsOverlappingParams{Since: since, WindowStart: start, WindowEnd: end})
	if err != nil {
		return nil, err
	}
	sessions := make([]Span, 0, len(srows))
	for _, r := range srows {
		sessions = append(sessions, Span{SubjectID: r.SubjectID, Start: r.StartedAt, End: r.EndedAt})
	}
	trows, err := q.ListFocusStaysOverlapping(ctx, dbq.ListFocusStaysOverlappingParams{Since: since, WindowStart: start, WindowEnd: end})
	if err != nil {
		return nil, err
	}
	stays := make([]Span, 0, len(trows))
	for _, r := range trows {
		stays = append(stays, Span{Start: r.EnteredAt, End: r.ExitedAt})
	}
	return BuildDays(dates, sessions, stays), nil
}
