package api

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/httpx"
	"github.com/yuntyu01/MyHome/server/internal/pipeline"
)

// 고정으로 빠지는 돈(주식 적립 등): 매달 이번 달 쓸 수 있는 돈에서 미리 뺀다. docs/DECISIONS.md 「고정으로 빠지는 돈」

type fixedInput struct {
	Name   *string `json:"name"`
	Amount *int64  `json:"amount"`
}

func cleanFixed(name string, amount int64) (string, int64, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 40 {
		return "", 0, errors.Join(pipeline.ErrInvalid, errors.New("이름은 1~40자"))
	}
	if amount <= 0 || amount > TotalBudgetMax {
		return "", 0, errors.Join(pipeline.ErrInvalid, errors.New("금액은 1원 이상 1000억 이하"))
	}
	return name, amount, nil
}

func (a *API) listFixed(w http.ResponseWriter, r *http.Request) {
	items, err := a.q().ListFixedDeductions(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	if items == nil {
		items = []dbq.FixedDeduction{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) createFixed(w http.ResponseWriter, r *http.Request) {
	var in fixedInput
	if err := httpx.DecodeJSON(r, &in); err != nil || in.Name == nil || in.Amount == nil {
		httpx.Error(w, http.StatusBadRequest, "name, amount가 필요해요")
		return
	}
	name, amount, err := cleanFixed(*in.Name, *in.Amount)
	if err != nil {
		a.fail(w, err)
		return
	}
	f, err := a.q().CreateFixedDeduction(r.Context(), dbq.CreateFixedDeductionParams{Name: name, Amount: amount})
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, f)
}

func (a *API) patchFixed(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in fixedInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	cur, err := a.q().GetFixedDeduction(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	name, amount := cur.Name, cur.Amount
	if in.Name != nil {
		name = *in.Name
	}
	if in.Amount != nil {
		amount = *in.Amount
	}
	if name, amount, err = cleanFixed(name, amount); err != nil {
		a.fail(w, err)
		return
	}
	f, err := a.q().UpdateFixedDeduction(r.Context(), dbq.UpdateFixedDeductionParams{ID: id, Name: name, Amount: amount, Archived: cur.Archived})
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, f)
}

// archiveFixed: DELETE는 보관으로 처리한다(데이터를 지우지 않는다). 목록과 합계에서 빠진다.
func (a *API) archiveFixed(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	cur, err := a.q().GetFixedDeduction(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	f, err := a.q().UpdateFixedDeduction(r.Context(), dbq.UpdateFixedDeductionParams{ID: id, Name: cur.Name, Amount: cur.Amount, Archived: true})
	if err != nil {
		a.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, f)
}
