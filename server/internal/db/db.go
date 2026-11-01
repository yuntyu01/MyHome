// Package db는 DB 연결, 마이그레이션(goose), 트랜잭션 도우미를 담는다.
// 쿼리 코드는 sqlc가 internal/dbq에 생성한다.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Connect는 커넥션 풀을 만들고 연결을 확인한다.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_URL 해석 실패: %w", err)
	}
	// 세션 시간대를 KST로 맞춘다. 저장은 timestamptz라 값 자체는 시간대와 무관하다.
	if _, ok := cfg.ConnConfig.RuntimeParams["timezone"]; !ok {
		cfg.ConnConfig.RuntimeParams["timezone"] = "Asia/Seoul"
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("DB 연결 실패: %w", err)
	}
	return pool, nil
}

// Migrate는 goose 명령(up, down, status, version, redo 등)을 실행한다.
func Migrate(ctx context.Context, url string, command string, args ...string) error {
	connCfg, err := pgx.ParseConfig(url)
	if err != nil {
		return fmt.Errorf("DATABASE_URL 해석 실패: %w", err)
	}
	sqlDB := stdlib.OpenDB(*connCfg)
	defer sqlDB.Close()
	return migrateDB(ctx, sqlDB, command, args...)
}

func migrateDB(ctx context.Context, sqlDB *sql.DB, command string, args ...string) error {
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.RunContext(ctx, command, sqlDB, "migrations", args...)
}

// InTx는 fn을 트랜잭션 안에서 실행한다. fn이 에러를 반환하면 롤백한다.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
