package health

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/ai"
	"github.com/yuntyu01/MyHome/server/internal/db"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
)

// Service는 건강 기록 저장소다(사용자 1명).
type Service struct {
	Pool *pgxpool.Pool
	Now  func() time.Time // 테스트용. nil이면 time.Now
	// PhotoDir은 끼니 사진을 두는 폴더(MEAL_PHOTO_DIR)다. 비우면 사진 저장을 끈다(분석만 한다) - photos.go
	PhotoDir string
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) today() Date { return Today(s.now()) }

func (s *Service) q() *dbq.Queries { return dbq.New(s.Pool) }

func (s *Service) tx(ctx context.Context, fn func(*dbq.Queries) error) error {
	return db.InTx(ctx, s.Pool, func(tx pgx.Tx) error { return fn(dbq.New(tx)) })
}

// ── 설정 ──────────────────────────────────────────────────────────────

// loadSettings는 저장된 설정이다. 아직 없으면 기본값과 stored=false.
func loadSettings(ctx context.Context, q *dbq.Queries) (st Settings, stored bool, err error) {
	doc, err := q.GetHealthSettings(ctx)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		st = Defaults()
	case err != nil:
		return st, false, err
	default:
		stored = true
		if err := json.Unmarshal(doc, &st); err != nil {
			return st, true, fmt.Errorf("건강 설정 파손: %w", err)
		}
	}
	rows, err := q.ListTargetHistory(ctx)
	if err != nil {
		return st, stored, err
	}
	h := make([]TargetEntry, 0, len(rows))
	for _, r := range rows {
		h = append(h, TargetEntry{From: DateOf(r.FromDate), Targets: Targets{int(r.Kcal), int(r.Carbs), int(r.Protein), int(r.Fat)}})
	}
	st.TargetHistory = Some(h)
	st.normalize()
	return st, stored, nil
}

// Settings는 지금 설정이다(저장한 적 없으면 기본값).
func (s *Service) Settings(ctx context.Context) (Settings, error) {
	st, _, err := loadSettings(ctx, s.q())
	return st, err
}

// PutSettings는 요청 본문(JSON)을 저장된 설정 위에 덮어 검증하고 저장한다. 보낸 키만 바뀐다
// (객체 안도 같다: gym에 arriveBy만 보내면 arriveBy만 바뀐다). null은 그 값을 지운다.
//
// targetHistory를 보내면 그대로 쓰고, 안 보내면 목표가 바뀐 경우 오늘(KST)부터로 이력에 남긴다
// (처음 바꾸면 예전 목표를 2000-01-01부터로 남겨서 지난 날은 그때 목표로 판단한다). 처음 저장할 때는
// 기본값을 "예전 목표"로 남기지 않는다.
func (s *Service) PutSettings(ctx context.Context, body []byte) (Settings, error) {
	var out Settings
	err := s.tx(ctx, func(q *dbq.Queries) error {
		if err := q.AcquireHealthLock(ctx); err != nil {
			return err
		}
		cur, stored, err := loadSettings(ctx, q)
		if err != nil {
			return err
		}
		// 저장된 값을 깊은 복사한 뒤 그 위에 요청을 푼다(포인터·슬라이스를 cur와 나누지 않게).
		var next Settings
		if b, err := json.Marshal(cur); err != nil {
			return err
		} else if err := json.Unmarshal(b, &next); err != nil {
			return err
		}
		next.TargetHistory = Opt[[]TargetEntry]{}
		if err := json.NewDecoder(bytes.NewReader(body)).Decode(&next); err != nil {
			return badRequest("INVALID", "요청 형식이 잘못됐어요")
		}
		sentHistory := next.TargetHistory.Set // null이면 이력을 지운다
		next.normalize()
		if err := next.Validate(); err != nil {
			return err
		}
		if err := validateRules(next); err != nil {
			return err
		}
		if !sentHistory {
			h := *cur.TargetHistory.Val
			if stored {
				h = recordTargets(h, *cur.Targets, *next.Targets, s.today())
			}
			next.TargetHistory = Some(h)
		}
		if err := saveSettings(ctx, q, next); err != nil {
			return err
		}
		out = next
		return nil
	})
	return out, err
}

// saveSettings는 문서(targetHistory 제외)와 날짜별 목표 표를 함께 바꾼다.
func saveSettings(ctx context.Context, q *dbq.Queries, st Settings) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(b, &doc); err != nil {
		return err
	}
	delete(doc, "targetHistory")
	if b, err = json.Marshal(doc); err != nil {
		return err
	}
	if err := q.UpsertHealthSettings(ctx, b); err != nil {
		return err
	}
	if err := q.DeleteTargetHistory(ctx); err != nil {
		return err
	}
	for _, e := range *st.TargetHistory.Val {
		if err := q.InsertTargetHistory(ctx, dbq.InsertTargetHistoryParams{FromDate: e.From.Time(),
			Kcal: int32(e.Kcal), Carbs: int32(e.Carbs), Protein: int32(e.Protein), Fat: int32(e.Fat)}); err != nil {
			return err
		}
	}
	return nil
}

// ── 끼니 ──────────────────────────────────────────────────────────────

// mealsBetween은 [from, to] 끼니를 날짜·번호 순으로 읽는다. withItems면 음식도 채운다.
func mealsBetween(ctx context.Context, q *dbq.Queries, from, to Date, withItems bool) ([]meal, error) {
	rows, err := q.ListMealLogsBetween(ctx, dbq.ListMealLogsBetweenParams{FromDate: from.Time(), ToDate: to.Time()})
	if err != nil {
		return nil, err
	}
	out := make([]meal, 0, len(rows))
	pos := map[int64]int{}
	for i, r := range rows {
		pos[r.ID] = i
		out = append(out, meal{ID: r.ID, Date: DateOf(r.MealDate), Index: int(r.MealIndex), Deadline: r.Deadline,
			Barcode: r.Barcode, Base: Nutrition{int(r.Kcal), int(r.Carbs), int(r.Protein), int(r.Fat)},
			Confidence: r.Confidence, Portion: r.Portion, SharedMeal: r.SharedMeal, LoggedAt: r.LoggedAt, Items: []ai.Item{}})
	}
	if !withItems || len(out) == 0 {
		return out, nil
	}
	items, err := q.ListMealItemsBetween(ctx, dbq.ListMealItemsBetweenParams{FromDate: from.Time(), ToDate: to.Time()})
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		i, ok := pos[it.MealLogID]
		if !ok {
			continue
		}
		out[i].Items = append(out[i].Items, ai.Item{Name: it.Name, Amount: it.Amount, Kcal: intPtr(it.Kcal),
			Carbs: intPtr(it.Carbs), Protein: intPtr(it.Protein), Fat: intPtr(it.Fat), Portion: it.Portion})
	}
	return out, nil
}

func intPtr(v *int32) *int {
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}

func int32Ptr(v *int) *int32 {
	if v == nil {
		return nil
	}
	n := int32(*v)
	return &n
}

// LogMeal은 끼니를 저장한다. 같은 날 같은 번호는 덮어쓰고(음식 목록도 바뀐다), 저장한 끼니를 돌려준다.
func (s *Service) LogMeal(ctx context.Context, r MealRequest) (MealView, error) {
	if err := r.Validate(); err != nil {
		return MealView{}, err
	}
	var out MealView
	err := s.tx(ctx, func(q *dbq.Queries) error {
		st, _, err := loadSettings(ctx, q)
		if err != nil {
			return err
		}
		extra := IsExtra(r.Index)
		if !extra && r.Index > len(st.Meals) {
			return badRequest("INVALID", fmt.Sprintf("설정한 끼니 수(%d)보다 큰 번호예요", len(st.Meals)))
		}
		m := meal{Date: r.Date, Index: r.Index, Barcode: r.Barcode, Portion: r.portion(), SharedMeal: r.SharedMeal,
			Base:       Nutrition{r.AI.Kcal, r.AI.Carbs, r.AI.Protein, r.AI.Fat},
			Confidence: r.AI.Confidence, LoggedAt: s.now(), Items: r.AI.Items}
		if !extra { // "기타"는 마감이 없다
			for _, slot := range st.Meals {
				if slot.Index == r.Index {
					m.Deadline = slot.Deadline
				}
			}
		}
		if r.Deadline != nil {
			m.Deadline = *r.Deadline
		}
		if r.LoggedAt != nil {
			m.LoggedAt = *r.LoggedAt
		}
		id, err := q.UpsertMealLog(ctx, dbq.UpsertMealLogParams{
			MealDate: m.Date.Time(), MealIndex: int32(m.Index), Deadline: m.Deadline, Barcode: m.Barcode,
			Kcal: int32(m.Base.Kcal), Carbs: int32(m.Base.Carbs), Protein: int32(m.Base.Protein), Fat: int32(m.Base.Fat),
			Confidence: m.Confidence, Portion: m.Portion, SharedMeal: m.SharedMeal, LoggedAt: m.LoggedAt,
		})
		if err != nil {
			return err
		}
		m.ID = id
		if err := q.DeleteMealItems(ctx, id); err != nil {
			return err
		}
		for i, it := range m.Items {
			if err := q.InsertMealItem(ctx, dbq.InsertMealItemParams{MealLogID: id, Position: int32(i), Name: it.Name,
				Amount: it.Amount, Kcal: int32Ptr(it.Kcal), Carbs: int32Ptr(it.Carbs), Protein: int32Ptr(it.Protein),
				Fat: int32Ptr(it.Fat), Portion: it.Portion}); err != nil {
				return err
			}
		}
		out = m.view()
		return nil
	})
	return out, err
}

// MealsOn은 그날 끼니다(번호 순).
func (s *Service) MealsOn(ctx context.Context, d Date) ([]MealView, error) {
	ms, err := mealsBetween(ctx, s.q(), d, d, true)
	if err != nil {
		return nil, err
	}
	out := make([]MealView, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.view())
	}
	return out, nil
}

// DeleteMeal은 그날 그 번호 끼니와 그 사진을 지운다(없어도 성공).
func (s *Service) DeleteMeal(ctx context.Context, d Date, index int) error {
	var files []string
	err := s.tx(ctx, func(q *dbq.Queries) error {
		if _, err := q.DeleteMealLog(ctx, dbq.DeleteMealLogParams{MealDate: d.Time(), MealIndex: int32(index)}); err != nil {
			return err
		}
		var err error
		files, err = q.DeleteMealPhotos(ctx, dbq.DeleteMealPhotosParams{MealDate: d.Time(), MealIndex: int32(index)})
		return err
	})
	if err == nil {
		s.deleteMealPhotoFiles(files)
	}
	return err
}

// ── 헬스·휴식권 ────────────────────────────────────────────────────────

// Verified는 헬스 인증 방법이다: photo(사진), geofence(헬스장 위치에 머묾, 폰이 확인), rest(휴식권).
// 헬스장 좌표는 서버에 두지 않는다. 폰이 지오펜스로 확인하고 결과만 보낸다(2026-11-08 사용자 요청으로 다시 켬).
type Verified string

const (
	Geofence Verified = "geofence"
	Photo    Verified = "photo"
	Rest     Verified = "rest"
)

func (v *Verified) UnmarshalJSON(b []byte) error {
	s, err := enumJSON(b, "geofence", "photo", "rest")
	*v = Verified(s)
	return err
}

// CheckIn은 헬스 응답 모양이다.
type CheckIn struct {
	Date       Date       `json:"date"`
	VerifiedBy Verified   `json:"verifiedBy"`
	CheckInAt  *time.Time `json:"checkInAt"`
}

// GymCheckIn은 사진 인증을 기록한다. 휴식으로 둔 날이어도 방문으로 바꾼다(휴식권은 돌려주지 않는다).
func (s *Service) GymCheckIn(ctx context.Context, d Date, by Verified, at *time.Time) (CheckIn, error) {
	t := s.now().UTC()
	if at != nil {
		t = at.UTC()
	}
	row, err := s.q().UpsertGymCheckIn(ctx, dbq.UpsertGymCheckInParams{GymDate: d.Time(), CheckInAt: &t, VerifiedBy: string(by)})
	if err != nil {
		return CheckIn{}, err
	}
	out := CheckIn{Date: DateOf(row.GymDate), VerifiedBy: Verified(row.VerifiedBy), CheckInAt: row.CheckInAt}
	if out.CheckInAt != nil {
		u := out.CheckInAt.UTC()
		out.CheckInAt = &u
	}
	return out, nil
}

// Balance는 이번 주 장수다.
type Balance struct {
	Total int `json:"total"`
	Left  int `json:"left"`
}

// CouponUse는 휴식권을 쓴 기록이다. date는 쉰 날(핏두잉에는 없던 필드).
type CouponUse struct {
	Type   string    `json:"type"` // 항상 "rest"
	Date   Date      `json:"date"`
	UsedAt time.Time `json:"usedAt"`
}

// RestSummary는 이번 주 휴식권 상태다(핏두잉 coupon.Summary에서 치팅권·비상 해제를 뺐다).
type RestSummary struct {
	WeekKey Date        `json:"weekKey"` // 그 주 월요일
	Rest    Balance     `json:"rest"`
	Uses    []CouponUse `json:"uses"`
}

func restSummary(ctx context.Context, q *dbq.Queries, st Settings, d Date) (RestSummary, error) {
	week := d.Monday()
	rows, err := q.ListRestUses(ctx, week.Time())
	if err != nil {
		return RestSummary{}, err
	}
	uses := make([]CouponUse, 0, len(rows))
	for _, r := range rows {
		uses = append(uses, CouponUse{Type: "rest", Date: DateOf(r.UsedOn), UsedAt: r.UsedAt.UTC()})
	}
	total := st.Coupons.Rest
	return RestSummary{WeekKey: week, Rest: Balance{total, max(0, total-len(uses))}, Uses: uses}, nil
}

// Rest는 d가 든 주의 휴식권 상태다.
func (s *Service) Rest(ctx context.Context, d Date) (RestSummary, error) {
	q := s.q()
	st, _, err := loadSettings(ctx, q)
	if err != nil {
		return RestSummary{}, err
	}
	return restSummary(ctx, q, st, d)
}

// UseRest는 휴식권 한 장으로 d의 헬스를 쉰다(방문으로 세지 않는다). 매주 월요일에 다시 채워진다.
// 이미 쉰 날이면 다시 차감하지 않고 지금 상태를 준다. 이미 인증한 날이면 409(방문 기록을 지우지 않는다).
func (s *Service) UseRest(ctx context.Context, d Date) (RestSummary, error) {
	var out RestSummary
	err := s.tx(ctx, func(q *dbq.Queries) error {
		if err := q.AcquireHealthLock(ctx); err != nil {
			return err
		}
		st, _, err := loadSettings(ctx, q)
		if err != nil {
			return err
		}
		g, err := q.GetGymLog(ctx, d.Time())
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return err
		case g.VerifiedBy == string(Rest):
			out, err = restSummary(ctx, q, st, d)
			return err
		default:
			return conflict("ALREADY_CHECKED_IN", "이미 헬스를 인증한 날이에요")
		}
		sum, err := restSummary(ctx, q, st, d)
		if err != nil {
			return err
		}
		if sum.Rest.Left <= 0 {
			return conflict("COUPON_EMPTY", "이번 주에 쓸 수 있는 장수를 다 썼어요")
		}
		if err := q.InsertRestUse(ctx, dbq.InsertRestUseParams{UsedOn: d.Time(), WeekKey: d.Monday().Time()}); err != nil {
			return err
		}
		if err := q.InsertGymRest(ctx, d.Time()); err != nil {
			return err
		}
		out, err = restSummary(ctx, q, st, d)
		return err
	})
	return out, err
}
