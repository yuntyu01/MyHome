// Package testdb는 DB 통합 테스트용 격리 스키마를 만든다.
//
// TEST_DATABASE_URL이 비어 있으면 테스트를 건너뛴다(Skip).
// 테스트마다 임의 이름의 스키마를 만들고 마이그레이션을 적용한 뒤, 끝나면 그 스키마만 지운다.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/db"
)

// New는 마이그레이션이 적용된 격리 스키마에 연결된 풀을 반환한다.
func New(t testing.TB) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL 미설정: DB 테스트 건너뜀")
	}
	ctx := context.Background()

	b := make([]byte, 6)
	_, _ = rand.Read(b)
	schema := "t_" + hex.EncodeToString(b)

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("테스트 DB 연결 실패: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("스키마 생성 실패: %v", err)
	}
	_ = admin.Close(ctx)

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("TEST_DATABASE_URL 해석 실패: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	scoped := u.String()

	if err := db.Migrate(ctx, scoped, "up"); err != nil {
		t.Fatalf("마이그레이션 실패: %v", err)
	}
	pool, err := db.Connect(ctx, scoped)
	if err != nil {
		t.Fatalf("풀 생성 실패: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		c, err := pgx.Connect(context.Background(), base)
		if err != nil {
			return
		}
		defer c.Close(context.Background())
		_, _ = c.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	})
	return pool
}
