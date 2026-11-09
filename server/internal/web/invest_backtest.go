package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/yuntyu01/MyHome/server/internal/httpx"
	"github.com/yuntyu01/MyHome/server/internal/invest/backtest"
)

// 백테스트 결과 — TQ 뷰어 「백테스트 결과」 이식. 고정된 과거 표라 DB도 증권사도 안 읽는다.
// 🔴 투자 첫 화면에 두지 않고 한 번 더 들어간 화면에 둔다(여는 것은 의식적 행위). 표와 읽는 법을 같이 둔다.

type btCell struct {
	Text string
	Top  bool
}

type btRow struct {
	Name  string
	Star  bool
	Cells []btCell
}

type btTable struct {
	Label string
	Hint  string
	Rows  []btRow
}

type btPage struct {
	All    bool
	Period string
	Head   []string
	Key    btTable
	Tables []btTable
	Notes  []string
}

func btAmount(v float64) string { return fmt.Sprintf("%.2f", v) }

func btPct(v float64, places int) string {
	return strings.Replace(fmt.Sprintf("%.*f%%", places, v), "-", "−", 1)
}

func btCellText(c backtest.Column, r backtest.Row) string {
	v := r.Value(c.Key)
	switch c.Key {
	case "loss_pct":
		return btPct(v, 1)
	case "mdd_median_pct", "mdd_worst_pct":
		return btPct(v, 0)
	}
	return btAmount(v)
}

func btRows(rows []backtest.Row) []btRow {
	tops := backtest.Tops(rows)
	out := make([]btRow, len(rows))
	for i, r := range rows {
		top := map[string]bool{}
		for _, k := range tops[i] {
			top[k] = true
		}
		cells := make([]btCell, len(backtest.Columns))
		for j, c := range backtest.Columns {
			cells[j] = btCell{Text: btCellText(c, r), Top: top[c.Key]}
		}
		out[i] = btRow{Name: strings.Replace(r.Name, "+변동성", "+vol", 1), Star: r.Star, Cells: cells}
	}
	return out
}

// btKeyRows는 맨 위 요약이다 — 7년 적립·전체 표본에서 ★를 TQQQ·QLD·QQQ 단순 보유와 나란히 놓는다.
func btKeyRows(t backtest.Table) []backtest.Row {
	var rows []backtest.Row
	for _, n := range backtest.KeyNames {
		if r, ok := t.Find("7년", true, n); ok {
			rows = append(rows, r)
		}
	}
	return rows
}

func buildBacktestPage(t backtest.Table, all bool) btPage {
	p := btPage{All: all, Period: t.Period, Notes: backtest.Notes(t)}
	for _, c := range backtest.Columns {
		p.Head = append(p.Head, c.Label)
	}
	key := btKeyRows(t)
	p.Key = btTable{Label: "7년 적립 · 전체 표본(닷컴 포함)", Rows: btRows(key)}
	if len(key) > 0 {
		p.Key.Hint = fmt.Sprintf("원금 %.2f억 · 표본 %d개 윈도우", key[0].Principal, key[0].Windows)
	}
	for _, h := range t.Horizons {
		rows, note := h.ExDotcom, "닷컴 버블(1998~2002)을 뺀 표본"
		if all {
			rows, note = h.All, "전체 표본 · 닷컴 포함"
		}
		if len(rows) == 0 {
			continue
		}
		p.Tables = append(p.Tables, btTable{
			Label: h.Label + " 적립",
			Hint:  fmt.Sprintf("원금 %.2f억 · 표본 %d개 윈도우 · %s", rows[0].Principal, rows[0].Windows, note),
			Rows:  btRows(rows),
		})
	}
	return p
}

// investBacktest는 GET /invest/backtest 다. ?s=all이면 전체 표본, 아니면 닷컴 제외(TQ와 같은 기본값).
func (wb *Web) investBacktest(w http.ResponseWriter, r *http.Request) {
	t, err := backtest.Load()
	if err != nil {
		wb.fail(w, err)
		return
	}
	pd := wb.base(r, "백테스트 결과", "invest")
	pd.Wide = true
	pd.Data = buildBacktestPage(t, r.URL.Query().Get("s") == "all")
	wb.render(w, "invest_backtest", pd)
}

// apiBacktestRow는 앱에 주는 한 줄이다. 숫자 그대로와 1등인 열(tops)을 같이 준다.
type apiBacktestRow struct {
	backtest.Row
	Tops []string `json:"tops"`
}

type apiBacktestHorizon struct {
	Label    string           `json:"label"`
	ExDotcom []apiBacktestRow `json:"ex_dotcom"`
	All      []apiBacktestRow `json:"all"`
}

type apiBacktest struct {
	Period   string               `json:"period"`
	FeeRate  float64              `json:"fee_rate"`
	Unit     string               `json:"unit"`
	Columns  []backtest.Column    `json:"columns"`
	Key      []apiBacktestRow     `json:"key"` // 7년 전체: ★·TQQQ·QLD·QQQ 보유
	Horizons []apiBacktestHorizon `json:"horizons"`
	Notes    []string             `json:"notes"`
}

func apiBtRows(rows []backtest.Row) []apiBacktestRow {
	tops := backtest.Tops(rows)
	out := make([]apiBacktestRow, len(rows))
	for i, r := range rows {
		out[i] = apiBacktestRow{Row: r, Tops: tops[i]}
		if out[i].Tops == nil {
			out[i].Tops = []string{}
		}
	}
	return out
}

// apiInvestBacktest는 앱 「백테스트 결과」용 GET /v1/invest/backtest 다.
func (wb *Web) apiInvestBacktest(w http.ResponseWriter, r *http.Request) {
	t, err := backtest.Load()
	if err != nil {
		wb.Logger.Error("백테스트 표 읽기 실패", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := apiBacktest{Period: t.Period, FeeRate: t.FeeRate, Unit: t.Unit, Columns: backtest.Columns,
		Key: apiBtRows(btKeyRows(t)), Notes: backtest.Notes(t)}
	for _, h := range t.Horizons {
		out.Horizons = append(out.Horizons, apiBacktestHorizon{Label: h.Label, ExDotcom: apiBtRows(h.ExDotcom), All: apiBtRows(h.All)})
	}
	httpx.JSON(w, http.StatusOK, out)
}
