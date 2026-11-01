// Command myhome는 가계부 서버 바이너리다.
//
// 서브커맨드:
//
//	serve                        HTTP 서버 실행 (기본값)
//	migrate [up|down|status|...] DB 마이그레이션 (goose)
//	token issue --name 이름       기기 토큰 발급 (비상용, CLI_TOKEN_ISSUE_ENABLED=false로 끔)
//	token list                   기기 목록
//	token revoke --id N          기기 토큰 폐기
//	healthcheck                  로컬 /healthz 확인 (Docker HEALTHCHECK용)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // scratch 이미지에서도 Asia/Seoul을 쓰기 위해 시간대 DB를 내장한다.

	"github.com/yuntyu01/MyHome/server/internal/config"
	"github.com/yuntyu01/MyHome/server/internal/db"
	"github.com/yuntyu01/MyHome/server/internal/server"
)

func main() {
	cfg, cfgErr := config.Load()
	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)
	if cfgErr != nil {
		logger.Error("설정 오류", "err", cfgErr)
		os.Exit(2)
	}

	cmd, args := "serve", []string(nil)
	if len(os.Args) > 1 {
		cmd, args = os.Args[1], os.Args[2:]
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch cmd {
	case "serve":
		err = serve(ctx, cfg, logger)
	case "migrate":
		err = migrate(ctx, cfg, args)
	case "token":
		err = tokenCmd(ctx, cfg, args, os.Stdout)
	case "healthcheck":
		err = healthcheck(cfg)
	case "help", "-h", "--help":
		fmt.Println(usage)
	default:
		err = fmt.Errorf("알 수 없는 명령: %s\n%s", cmd, usage)
	}
	if err != nil {
		logger.Error("실패", "cmd", cmd, "err", err)
		os.Exit(1)
	}
}

const usage = `사용법: myhome <명령> [옵션]
  serve                         HTTP 서버 실행
  migrate [up|down|status|...]  DB 마이그레이션
  token issue --name 이름        기기 토큰 발급(비상용)
  token list                    기기 목록
  token revoke --id N           기기 토큰 폐기
  healthcheck                   /healthz 확인`

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv}))
}

func serve(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	if err := cfg.RequireDB(); err != nil {
		return err
	}
	if cfg.MigrateOnStart {
		if err := db.Migrate(ctx, cfg.DatabaseURL, "up"); err != nil {
			return fmt.Errorf("마이그레이션 실패: %w", err)
		}
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	handler := server.New(server.Deps{Config: cfg, Pool: pool, Logger: logger})
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		logger.Info("서버 시작", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	logger.Info("서버 종료 중")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func migrate(ctx context.Context, cfg config.Config, args []string) error {
	if err := cfg.RequireDB(); err != nil {
		return err
	}
	command := "up"
	if len(args) > 0 {
		command, args = args[0], args[1:]
	}
	return db.Migrate(ctx, cfg.DatabaseURL, command, args...)
}

// healthcheck는 scratch 이미지에서 curl 없이 헬스체크를 하기 위한 명령이다.
func healthcheck(cfg config.Config) error {
	addr := cfg.HTTPAddr
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz status %d", resp.StatusCode)
	}
	return nil
}
