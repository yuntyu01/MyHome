package web

import "net/http"

// report는 월간 리포트(기본: 지난 기간)다.
func (wb *Web) report(w http.ResponseWriter, r *http.Request) {
	p, startDay, err := wb.API.ReportPeriod(r)
	if err != nil {
		wb.fail(w, err)
		return
	}
	rep, err := wb.API.BuildMonthlyReport(r, p, p.Prev(startDay))
	if err != nil {
		wb.fail(w, err)
		return
	}
	pd := wb.base(r, "월간 리포트", "report")
	wb.withPeriod(&pd, p, startDay)
	pd.Data = rep
	wb.render(w, "report", pd)
}

// reconcile은 대사 도우미(출처별 합계, 제외 내역, 최근 6개 기간)다.
func (wb *Web) reconcile(w http.ResponseWriter, r *http.Request) {
	p, cfg, err := wb.API.PeriodFor(r)
	if err != nil {
		wb.fail(w, err)
		return
	}
	rc, err := wb.API.BuildReconcile(r, p, cfg.BudgetStartDay, 6)
	if err != nil {
		wb.fail(w, err)
		return
	}
	pd := wb.base(r, "대사 도우미", "reconcile")
	wb.withPeriod(&pd, p, cfg.BudgetStartDay)
	pd.Data = rc
	wb.render(w, "reconcile", pd)
}
