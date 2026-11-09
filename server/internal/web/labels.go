package web

import (
	"fmt"
	"sort"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/api"
	"github.com/yuntyu01/MyHome/server/internal/period"
)

// sourceNames는 파서 이름을 화면에 보여줄 이름으로 바꾼다. 없는 이름은 그대로 보여준다.
var sourceNames = map[string]string{
	"kb":         "KB국민카드",
	"toss":       "토스",
	"kakaopay":   "카카오페이",
	"naverpay":   "네이버페이",
	"coupang":    "쿠팡",
	"tmoney":     "티머니",
	"samsungpay": "삼성페이",
	"manual":     "직접 입력",
	"generic":    "기타 앱",
}

func sourceName(s string) string {
	if n, ok := sourceNames[s]; ok {
		return n
	}
	return s
}

var matchLabels = map[string]string{
	"contains": "포함",
	"exact":    "정확히 같음",
	"prefix":   "이름으로 시작",
	"regex":    "정규식",
}

func matchLabel(s string) string {
	if n, ok := matchLabels[s]; ok {
		return n
	}
	return s
}

// monthName은 기간 라벨("2026-10")을 읽기 쉽게 바꾼다. 올해면 "10월", 아니면 "2025년 10월".
func monthName(label string) string { return monthNameAt(label, time.Now()) }

func monthNameAt(label string, now time.Time) string {
	t, err := time.ParseInLocation("2006-01", label, period.KST)
	if err != nil {
		return label
	}
	if t.Year() == now.In(period.KST).Year() {
		return fmt.Sprintf("%d월", t.Month())
	}
	return fmt.Sprintf("%d년 %d월", t.Year(), t.Month())
}

// shortDate는 "2026-10-15"를 "10월 15일"로 바꾼다.
func shortDate(s string) string {
	t, err := time.ParseInLocation("2006-01-02", s, period.KST)
	if err != nil {
		return s
	}
	return fmt.Sprintf("%d월 %d일", t.Month(), t.Day())
}

// dayNameAt은 "2026-10-15"를 "10월 15일"로, 올해가 아니면 "2025년 10월 15일"로 바꾼다.
// 기준일처럼 연도가 헷갈리면 안 되는 날짜에 쓴다.
func dayNameAt(s string, now time.Time) string {
	t, err := time.ParseInLocation("2006-01-02", s, period.KST)
	if err != nil {
		return s
	}
	if t.Year() == now.In(period.KST).Year() {
		return fmt.Sprintf("%d월 %d일", t.Month(), t.Day())
	}
	return fmt.Sprintf("%d년 %d월 %d일", t.Year(), t.Month(), t.Day())
}

// monthRange는 시작일이 1일이 아닐 때 기간을 함께 보여주는 글자다. 1일이면 달 이름으로 충분하다.
func monthRange(p period.Period, startDay int) string {
	if period.ClampStartDay(startDay) == 1 {
		return ""
	}
	return p.Start.Format("1/2") + "~" + p.LastDay().Format("1/2")
}

// dashboardTopCategories는 홈에 항상 펼쳐 둘 분류 수다.
const dashboardTopCategories = 5

// splitCategories는 홈에 바로 보일 분류와 접어 둘 분류를 나눈다.
// 예산을 넘긴 분류를 많이 넘긴 순으로 맨 위에, 그다음 사용률 높은 순으로 둔다.
// 80% 이상 쓴 분류는 항상 보이고, 아직 안 쓴 분류는 항상 접는다. 예산도 지출도 없는 분류는 뺀다.
// 맨 위 분류만 나눈다(폴더 값은 안의 분류를 더한 것이라 안의 분류까지 넣으면 두 번 센다).
func splitCategories(cats []api.CategoryBudget) (top, rest []api.CategoryBudget) {
	var used, unused []api.CategoryBudget
	for _, c := range cats {
		switch {
		case c.ParentID != nil:
		case c.Spent == 0 && c.Settled == 0 && c.Budget == 0:
		case c.Spent == 0 && c.Settled == 0:
			unused = append(unused, c)
		default:
			used = append(used, c)
		}
	}
	sort.SliceStable(used, func(i, j int) bool {
		a, b := used[i], used[j]
		aOver, bOver := a.Remaining < 0, b.Remaining < 0
		if aOver != bOver {
			return aOver
		}
		if aOver {
			return a.Remaining < b.Remaining
		}
		return a.Ratio > b.Ratio
	})
	for i, c := range used {
		if i < dashboardTopCategories || c.Remaining < 0 || c.Ratio >= 0.8 {
			top = append(top, c)
		} else {
			rest = append(rest, c)
		}
	}
	return top, append(rest, unused...)
}
