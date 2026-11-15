package api

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/yuntyu01/MyHome/server/internal/balance"
	"github.com/yuntyu01/MyHome/server/internal/db"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/httpx"
)

// listBalances는 GET /v1/balances다: 출처(계좌)마다 알림에 찍힌 마지막 잔액과 아직 처리하지 않은 빈 돈.
func (a *API) listBalances(w http.ResponseWriter, r *http.Request) {
	accts, err := balance.Accounts(r.Context(), a.q(), a.now())
	if err != nil {
		a.fail(w, err)
		return
	}
	if accts == nil {
		accts = []balance.Account{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"accounts": accts})
}

// fillBalanceGap은 POST /v1/balances/gaps/{id}/fill이다: 빈 돈을 확인 필요 거래로 넣는다(분류는 확인 필요에서).
func (a *API) fillBalanceGap(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var t dbq.Transaction
	err := db.InTx(r.Context(), a.Pool, func(tx pgx.Tx) error {
		var err error
		t, err = balance.Fill(r.Context(), dbq.New(tx), a.now(), id)
		return err
	})
	if a.balanceErr(w, err) {
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"transaction": fromTx(t, "")})
}

// ignoreBalanceGap은 POST /v1/balances/gaps/{id}/ignore다: 빈 돈을 무시한다(금액이 바뀌면 다시 보인다).
func (a *API) ignoreBalanceGap(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	if a.balanceErr(w, balance.Ignore(r.Context(), a.q(), a.now(), id)) {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// balanceErr는 오류를 응답하고 true를 돌려준다. 이미 처리한 빈 돈이면 409.
func (a *API) balanceErr(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, balance.ErrNoGap):
		httpx.Error(w, http.StatusConflict, err.Error())
	default:
		a.fail(w, err)
	}
	return true
}
