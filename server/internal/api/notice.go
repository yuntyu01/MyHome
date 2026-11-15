package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/yuntyu01/MyHome/server/internal/httpx"
	"github.com/yuntyu01/MyHome/server/internal/notice"
	"github.com/yuntyu01/MyHome/server/internal/pipeline"
)

// maxNoticeIDs는 GET /v1/events/notice 한 번에 물을 수 있는 원문 수다. 앱은 붙들고 있는 알림만 묻는다(보통 몇 건).
const maxNoticeIDs = 100

// EventNotice는 원문 하나에 대한 답이다. notice는 dismiss|wait|keep(notice 패키지).
type EventNotice struct {
	ID     int64  `json:"id"`
	Notice string `json:"notice"`
}

// eventNotices는 GET /v1/events/notice?ids=1,2,3이다. 앱이 남겨 둔 결제 알림을 지울 때가 됐는지(AI 판정·사용자 분류 뒤) 묻는다.
func (a *API) eventNotices(w http.ResponseWriter, r *http.Request) {
	var ids []int64
	for _, s := range strings.Split(r.URL.Query().Get("ids"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil || id <= 0 {
			a.fail(w, pipeline.ErrInvalid)
			return
		}
		ids = append(ids, id)
	}
	if len(ids) > maxNoticeIDs {
		a.fail(w, pipeline.ErrInvalid)
		return
	}
	c := notice.Checker{AI: a.Suggestions}
	out := make([]EventNotice, 0, len(ids))
	for _, id := range ids {
		n, err := c.State(r.Context(), a.q(), id)
		if err != nil {
			a.fail(w, err)
			return
		}
		out = append(out, EventNotice{ID: id, Notice: n})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"notices": out})
}
