package api

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/yuntyu01/MyHome/server/internal/httpx"
)

// ClientError는 앱이 보내는 오류 보고다(사용자 요청 뒤 2026-11-15: 폰에서만 나는 오류를 서버 로그로 보려고).
// 앱이 죽었을 때의 스택(다음 실행 때 보냄)과 화면에 띄운 오류 메시지만 보낸다. 알림 원문·금액·사진은 보내지 않는다.
type ClientError struct {
	Kind       string `json:"kind"`        // crash, meal_analyze, camera 등
	Message    string `json:"message"`     // 오류 메시지·스택
	AppVersion string `json:"app_version"` // versionName(versionCode)
	At         string `json:"at"`          // 폰 시각(ISO-8601)
}

const maxClientErrorRunes = 6000

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// postClientError는 POST /v1/client-errors다. DB에 넣지 않고 서버 로그(경고)에만 남긴다.
func (a *API) postClientError(w http.ResponseWriter, r *http.Request) {
	var in ClientError
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(in.Kind) == "" || strings.TrimSpace(in.Message) == "" {
		httpx.Error(w, http.StatusBadRequest, "kind, message required")
		return
	}
	a.Logger.Warn("앱 오류", "kind", clip(in.Kind, 40), "app_version", clip(in.AppVersion, 40), "at", clip(in.At, 40), "message", clip(in.Message, maxClientErrorRunes))
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}
