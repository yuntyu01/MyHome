package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/httpx"
	"github.com/yuntyu01/MyHome/server/internal/period"
	"github.com/yuntyu01/MyHome/server/internal/pipeline"
)

// TransactionDTO는 거래 JSON 표현이다.
type TransactionDTO struct {
	ID               int64     `json:"id"`
	OccurredAt       time.Time `json:"occurred_at"`
	Amount           int64     `json:"amount"`
	Flow             string    `json:"flow"`
	Merchant         string    `json:"merchant"`
	Memo             string    `json:"memo"`
	Direction        string    `json:"direction"`
	CategoryID       *int64    `json:"category_id"`
	Category         string    `json:"category"`
	SettleCategoryID *int64    `json:"settle_category_id"`
	Status           string    `json:"status"`
	ExclusionReason  *string   `json:"exclusion_reason"`
	ReviewReason     *string   `json:"review_reason"`
	MergedInto       *int64    `json:"merged_into"`
	CancelsID        *int64    `json:"cancels_id"`
	Source           string    `json:"source"`
	Kind             string    `json:"kind"`
	Installment      int32     `json:"installment_months"`
	ManuallyEdited   bool      `json:"manually_edited"`
	RawEventID       *int64    `json:"raw_event_id"`
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func fromTx(t dbq.Transaction, catName string) TransactionDTO {
	return TransactionDTO{
		ID: t.ID, OccurredAt: t.OccurredAt.In(period.KST), Amount: t.Amount, Flow: t.Flow, Merchant: t.Merchant, Memo: t.Memo,
		Direction: t.Direction, CategoryID: t.CategoryID, Category: catName, SettleCategoryID: t.SettleCategoryID,
		Status: t.Status, ExclusionReason: t.ExclusionReason, ReviewReason: t.ReviewReason, MergedInto: t.MergedInto,
		CancelsID: t.CancelsID, Source: t.Source, Kind: t.Kind, Installment: t.InstallmentMonths,
		ManuallyEdited: t.ManuallyEdited, RawEventID: t.RawEventID,
	}
}

// 아래 변환기들은 sqlc가 쿼리마다 만든 Row 타입을 공통 DTO로 바꾼다.

func listRowDTO(t dbq.ListTransactionsRow) TransactionDTO {
	return fromTx(dbq.Transaction{ID: t.ID, RawEventID: t.RawEventID, OccurredAt: t.OccurredAt, Amount: t.Amount, Flow: t.Flow, Merchant: t.Merchant, Memo: t.Memo, Direction: t.Direction, CategoryID: t.CategoryID, SettleCategoryID: t.SettleCategoryID, Status: t.Status, ExclusionReason: t.ExclusionReason, ReviewReason: t.ReviewReason, MergedInto: t.MergedInto, CancelsID: t.CancelsID, Source: t.Source, Kind: t.Kind, InstallmentMonths: t.InstallmentMonths, Balance: t.Balance, ManuallyEdited: t.ManuallyEdited}, str(t.CategoryName))
}

func reviewRowDTO(t dbq.ListNeedsReviewRow) TransactionDTO {
	return fromTx(dbq.Transaction{ID: t.ID, RawEventID: t.RawEventID, OccurredAt: t.OccurredAt, Amount: t.Amount, Flow: t.Flow, Merchant: t.Merchant, Memo: t.Memo, Direction: t.Direction, CategoryID: t.CategoryID, SettleCategoryID: t.SettleCategoryID, Status: t.Status, ExclusionReason: t.ExclusionReason, ReviewReason: t.ReviewReason, MergedInto: t.MergedInto, CancelsID: t.CancelsID, Source: t.Source, Kind: t.Kind, InstallmentMonths: t.InstallmentMonths, Balance: t.Balance, ManuallyEdited: t.ManuallyEdited}, str(t.CategoryName))
}

func recentDTO(t dbq.RecentTransactionsRow) TransactionDTO {
	return fromTx(dbq.Transaction{ID: t.ID, RawEventID: t.RawEventID, OccurredAt: t.OccurredAt, Amount: t.Amount, Flow: t.Flow, Merchant: t.Merchant, Memo: t.Memo, Direction: t.Direction, CategoryID: t.CategoryID, SettleCategoryID: t.SettleCategoryID, Status: t.Status, ExclusionReason: t.ExclusionReason, ReviewReason: t.ReviewReason, MergedInto: t.MergedInto, CancelsID: t.CancelsID, Source: t.Source, Kind: t.Kind, InstallmentMonths: t.InstallmentMonths, Balance: t.Balance, ManuallyEdited: t.ManuallyEdited}, str(t.CategoryName))
}

// TransactionFilter는 거래 목록 조회 조건이다.
type TransactionFilter struct {
	From, To   time.Time
	Status     *string
	CategoryID *int64
	Direction  *string
	Q          *string
	Limit      int
	Offset     int
}

// ParseTransactionFilter는 쿼리 문자열을 해석한다.
// month=YYYY-MM 또는 from=YYYY-MM-DD&to=YYYY-MM-DD(KST, to 포함). 둘 다 없으면 이번 기간.
func (a *API) ParseTransactionFilter(r *http.Request) (TransactionFilter, error) {
	v := r.URL.Query()
	f := TransactionFilter{Limit: queryInt(r, "limit", 100, 500)}
	f.Offset, _ = strconv.Atoi(v.Get("offset"))
	if f.Offset < 0 {
		f.Offset = 0
	}
	if from, to := v.Get("from"), v.Get("to"); from != "" || to != "" {
		var err error
		f.From, err = time.ParseInLocation("2006-01-02", from, period.KST)
		if err != nil {
			return f, pipeline.ErrInvalid
		}
		toDay, err := time.ParseInLocation("2006-01-02", to, period.KST)
		if err != nil {
			return f, pipeline.ErrInvalid
		}
		f.To = toDay.AddDate(0, 0, 1)
	} else {
		p, _, err := a.periodFor(r)
		if err != nil {
			return f, err
		}
		f.From, f.To = p.Start, p.End
	}
	if s := v.Get("status"); s != "" {
		f.Status = &s
	}
	if d := v.Get("direction"); d != "" {
		f.Direction = &d
	}
	if c := v.Get("category_id"); c != "" {
		id, err := strconv.ParseInt(c, 10, 64)
		if err != nil {
			return f, pipeline.ErrInvalid
		}
		f.CategoryID = &id
	}
	if s := strings.TrimSpace(v.Get("q")); s != "" {
		f.Q = &s
	}
	return f, nil
}

// ListTransactions는 필터로 거래를 조회한다(웹에서도 쓴다).
func (a *API) ListTransactions(r *http.Request, f TransactionFilter) ([]TransactionDTO, error) {
	rows, err := a.q().ListTransactions(r.Context(), dbq.ListTransactionsParams{
		FromAt: f.From, ToAt: f.To, Status: f.Status, CategoryID: f.CategoryID, Direction: f.Direction, Q: f.Q,
		Lim: int32(f.Limit), Off: int32(f.Offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]TransactionDTO, 0, len(rows))
	for _, t := range rows {
		out = append(out, listRowDTO(t))
	}
	return out, nil
}

func (a *API) listTransactions(w http.ResponseWriter, r *http.Request) {
	f, err := a.ParseTransactionFilter(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	out, err := a.ListTransactions(r, f)
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"transactions": out})
}

func (a *API) listReview(w http.ResponseWriter, r *http.Request) {
	rows, err := a.q().ListNeedsReview(r.Context(), int32(queryInt(r, "limit", 100, 500)))
	if err != nil {
		a.fail(w, err)
		return
	}
	out := make([]TransactionDTO, 0, len(rows))
	for _, t := range rows {
		out = append(out, reviewRowDTO(t))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"transactions": out})
}

func (a *API) patchTransaction(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in pipeline.Edit
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	res, err := a.Editor.EditTransaction(r.Context(), id, in)
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"transaction": fromTx(res.Transaction, a.categoryName(r, res.Transaction.CategoryID)),
		"rule_id":     res.RuleID, "also_updated": res.AlsoUpdated,
	})
}

func (a *API) createTransaction(w http.ResponseWriter, r *http.Request) {
	var in pipeline.Manual
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	t, err := a.Editor.CreateManual(r.Context(), in, a.now())
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"transaction": fromTx(t, a.categoryName(r, t.CategoryID))})
}

func (a *API) categoryName(r *http.Request, id *int64) string {
	if id == nil {
		return ""
	}
	c, err := a.q().GetCategory(r.Context(), *id)
	if err != nil {
		return ""
	}
	return c.Name
}
