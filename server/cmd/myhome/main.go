// Command myhome는 가계부 서버 바이너리다.
//
// 서브커맨드:
//
//	serve                        HTTP 서버 실행 (기본값)
//	migrate [up|down|status|...] DB 마이그레이션 (goose)
//	token issue --name 이름       기기 토큰 발급 (비상용, CLI_TOKEN_ISSUE_ENABLED=false로 끔)
//	token list                   기기 목록
//	token revoke --id N          기기 토큰 폐기
//	reprocess --from --to        기간 원문 재처리
//	process-pending              처리 안 된 원문 처리
//	export-raw --from --to       익명화한 원문 내보내기
//	healthcheck                  로컬 /healthz 확인 (Docker HEALTHCHECK용)
//	invest <명령>                 자동매매 (TQ 이식). `myhome invest help`
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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/ai"
	"github.com/yuntyu01/MyHome/server/internal/config"
	"github.com/yuntyu01/MyHome/server/internal/db"
	"github.com/yuntyu01/MyHome/server/internal/pipeline"
	"github.com/yuntyu01/MyHome/server/internal/server"
	"github.com/yuntyu01/MyHome/server/internal/suggest"
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

	// invest는 종료 코드(0·1·75)가 의미를 가진다 — systemd가 75를 '나중에 다시'로 읽는다
	if cmd == "invest" {
		code := investCmd(ctx, cfg, args)
		stop()
		os.Exit(code)
	}

	var err error
	switch cmd {
	case "serve":
		err = serve(ctx, cfg, logger)
	case "migrate":
		err = migrate(ctx, cfg, args)
	case "token":
		err = tokenCmd(ctx, cfg, args, os.Stdout)
	case "reprocess":
		err = reprocessCmd(ctx, cfg, logger, args, os.Stdout)
	case "process-pending":
		err = processPendingCmd(ctx, cfg, logger, os.Stdout)
	case "export-raw":
		err = exportRawCmd(ctx, cfg, args, os.Stdout)
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
  reprocess --from D --to D [--apply]   기간 원문 재처리 (기본은 계획만 출력)
  process-pending               처리 안 된 원문 처리
  export-raw --from D --to D [--out F] [--failed]  익명화한 원문 내보내기(JSONL)
  healthcheck                   /healthz 확인
  invest <명령>                  자동매매 (myhome invest help)`

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
	// 운영에서 Cloudflare Access 설정이 빠졌으면 시작하지 않는다(웹이 열린 채로 뜨지 않게).
	if err := cfg.ValidateServe(); err != nil {
		return err
	}
	if len(cfg.Obsolete) > 0 {
		logger.Warn("더 이상 쓰지 않는 환경변수(카카오 로그인 제거, 지워도 됨)", "names", cfg.Obsolete)
	}
	if !cfg.AccessConfigured() {
		logger.Warn("Cloudflare Access 검사 꺼짐: 개발 모드(APP_ENV=development)에서만 허용")
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

	proc, err := newProcessor(cfg, pool, logger)
	if err != nil {
		return err
	}
	// 지난번에 처리 도중 멈춘 원문이 있으면 먼저 처리한다.
	if n, err := proc.ProcessPending(ctx, 1000); err != nil {
		logger.Warn("미처리 원문 처리 실패", "err", err)
	} else if n > 0 {
		logger.Info("미처리 원문 처리", "count", n)
	}

	// AI 분류 추천: 수집은 그대로 동기 처리하고, 추천은 뒤에서 따로 돈다(serve가 끝나면 같이 끝난다).
	workerCtx, stopWorker := context.WithCancel(ctx)
	workerDone := startSuggestWorker(workerCtx, cfg, pool, proc, logger)
	// 서버가 어떤 이유로 끝나든 작업을 멈추고, pool.Close보다 먼저 끝나기를 기다린다(defer는 뒤에서부터)
	defer func() { stopWorker(); <-workerDone }()

	// 수집 때 처리에 실패한 원문(응답은 201)을 10분마다 다시 처리한다. 위 작업과 같이 멈춘다.
	pendingDone := make(chan struct{})
	go func() { defer close(pendingDone); proc.RunPending(workerCtx, 10*time.Minute, 1000) }()
	defer func() { stopWorker(); <-pendingDone }()

	handler := server.New(server.Deps{Config: cfg, Pool: pool, Logger: logger, Processor: proc})
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

// startSuggestWorker는 AI 분류 추천 작업을 띄운다. AI_CLASSIFY=off면 띄우지 않는다. 돌려준 채널은 작업이 끝나면 닫힌다.
// AI_PROVIDER=mock이면 키 없이 낱말 표로 늘 같은 추천을 준다(로컬에서 화면 확인용).
func startSuggestWorker(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, proc *pipeline.Processor, logger *slog.Logger) <-chan struct{} {
	done := make(chan struct{})
	if !cfg.AIClassify {
		logger.Info("AI 분류 추천 꺼짐(AI_CLASSIFY=off)")
		close(done)
		return done
	}
	cl, err := ai.NewMerchantClassifier(cfg.AIProvider, cfg.GeminiAPIKey, cfg.GeminiModel)
	if err != nil { // config.Load가 먼저 거르므로 여기 오면 설정 오류다. 추천만 끄고 서버는 띄운다
		logger.Error("AI 분류 추천을 켜지 못함", "err", err)
		close(done)
		return done
	}
	w := &suggest.Worker{Pool: pool, Classifier: cl, Logger: logger, Auto: true}
	proc.OnNoRule = w.Notify
	logger.Info("AI 분류 추천 켜짐", "provider", cfg.AIProvider, "model", cl.Model())
	go func() {
		defer close(done)
		w.Run(ctx)
	}()
	return done
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
