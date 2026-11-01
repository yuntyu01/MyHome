package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/httpx"
)

type ctxKey int

const deviceKey ctxKey = iota

// DeviceFromContext는 기기 토큰 인증을 통과한 기기를 반환한다.
func DeviceFromContext(ctx context.Context) (dbq.Device, bool) {
	d, ok := ctx.Value(deviceKey).(dbq.Device)
	return d, ok
}

// WithDevice는 ctx에 기기를 넣는다.
func WithDevice(ctx context.Context, d dbq.Device) context.Context {
	return context.WithValue(ctx, deviceKey, d)
}

// DeviceLookup은 토큰 해시로 활성 기기를 찾는다.
type DeviceLookup interface {
	GetActiveDeviceByTokenHash(ctx context.Context, tokenHash []byte) (dbq.Device, error)
	TouchDevice(ctx context.Context, id int64) error
}

// AuthenticateDevice는 요청의 Bearer 토큰으로 기기를 찾는다. 폐기됐거나 없으면 ok=false.
func AuthenticateDevice(r *http.Request, q DeviceLookup) (dbq.Device, bool, error) {
	tok, ok := BearerToken(r.Header.Get("Authorization"))
	if !ok {
		return dbq.Device{}, false, nil
	}
	d, err := q.GetActiveDeviceByTokenHash(r.Context(), HashToken(tok))
	if errors.Is(err, pgx.ErrNoRows) {
		return dbq.Device{}, false, nil
	}
	if err != nil {
		return dbq.Device{}, false, err
	}
	return d, true, nil
}

// RequireDevice는 유효한 기기 토큰이 없으면 401을 반환한다.
func RequireDevice(q DeviceLookup, logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d, ok, err := AuthenticateDevice(r, q)
		if err != nil {
			logger.Error("기기 인증 조회 실패", "err", err)
			httpx.Error(w, http.StatusInternalServerError, "internal error")
			return
		}
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="myhome"`)
			httpx.Error(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if err := q.TouchDevice(r.Context(), d.ID); err != nil {
			logger.Warn("기기 last_seen 갱신 실패", "err", err, "device_id", d.ID)
		}
		next.ServeHTTP(w, r.WithContext(WithDevice(r.Context(), d)))
	})
}
