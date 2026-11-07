package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/config"
	"github.com/yuntyu01/MyHome/server/internal/db"
	"github.com/yuntyu01/MyHome/server/internal/invest/broker"
	"github.com/yuntyu01/MyHome/server/internal/invest/run"
	"github.com/yuntyu01/MyHome/server/internal/invest/store"
	"github.com/yuntyu01/MyHome/server/internal/invest/toss"
	"github.com/yuntyu01/MyHome/server/internal/invest/yahoo"
)

const investUsage = `사용법: myhome invest <명령> [옵션]

자동 (systemd timer)
  refresh [--retries N --wait 15m]   가격 마스터 갱신 (야후, 원종가)
  judge [--dry-run]                  월말 판정 — 매매 없음
  trade [--dry-run] [--fx F]         자동 매매 한 단계 (매일 호출, 멱등)
  status                             현재 상태 / deadman (이상이면 종료 코드 1)

수동 — 복구·교정 전용
  order                              주문서 + preflight
  execute --fill SYM=PX [--fx F] [--shares N] [--paid KRW] [--dry-run]
  amend --fill SYM=PX --fx F [--dry-run]
  adjust (--auto | --shares N --paid KRW) [--symbol S] [--fx F] --reason "..." [--dry-run]
  tax [--year Y] [--paid Y] [--dry-run]
  contribute [--amount KRW --from YYYY-MM --reason "..."]
  deviate --symbol S [--reason "..."]   규칙 이탈 (24시간 간격 2회)
  cancel                             이탈 요청 취소
  ledger                             원금 내역 (총액 대조)
  verify                             판정 검산
  why                                확신이 흔들릴 때
  reconcile [--apply-cash]           증권사 계좌 대조 (읽기 전용)
  alert --unit U                     실패 알림 메일 (systemd OnFailure가 부른다)

마이그레이션
  import-tq --state DIR --master CSV [--dry-run]

종료 코드: 0 정상, 1 실패(기다려도 안 풀림), 75 기다리면 풀리는 실패
환경변수: DATABASE_URL, TOSS_APP_KEY, TOSS_APP_SECRET, TOSS_ACCOUNT, TOSS_TOKEN_FILE(선택),
          MAIL_FROM, MAIL_TO, GMAIL_APP_PASSWORD (실패 알림), INVEST_HEARTBEAT_URL (status 성공 시 ping)`

// readOnlyCmds는 상태를 쓰지 않는 명령이다. 잠금을 잡지 않는다 — 매매 중에도 볼 수 있게.
var readOnlyCmds = map[string]bool{"order": true, "ledger": true, "verify": true, "why": true, "status": true, "alert": true}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

type optInt struct{ v *int }

func (o *optInt) String() string { return "" }
func (o *optInt) Set(s string) error {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return err
	}
	o.v = &n
	return nil
}

type optFloat struct{ v *float64 }

func (o *optFloat) String() string { return "" }
func (o *optFloat) Set(s string) error {
	var f float64
	if _, err := fmt.Sscanf(s, "%g", &f); err != nil {
		return err
	}
	o.v = &f
	return nil
}

// investCmd는 `myhome invest ...`를 실행하고 종료 코드를 돌려준다.
// 출력은 그대로 터미널에 내보내면서 invest_runs에도 남긴다 — 실패 알림과 화면이 읽는다.
func investCmd(ctx context.Context, cfg config.Config, args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Println(investUsage)
		return 0
	}
	if err := cfg.RequireDB(); err != nil {
		fmt.Fprintln(os.Stderr, "✗", err)
		return run.ExitFail
	}
	name, rest := args[0], args[1:]
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗", err)
		return run.ExitRetry // DB가 잠깐 안 뜬 것일 수 있다
	}
	defer pool.Close()

	st := store.New(pool, "invest "+name)
	var buf bytes.Buffer
	app := &run.App{
		Store:      st,
		Out:        io.MultiWriter(os.Stdout, &buf),
		Err:        io.MultiWriter(os.Stderr, &buf),
		Prices:     &yahoo.Client{},
		UnsafeDate: os.Getenv("INVEST_UNSAFE_DATE") == "1",
	}
	app.Broker = func(ctx context.Context) (broker.Broker, error) {
		c, err := toss.FromEnv()
		if err != nil {
			return nil, err
		}
		return &toss.Client{Config: c, TokenFile: os.Getenv("TOSS_TOKEN_FILE"), Log: app.Err}, nil
	}

	if !readOnlyCmds[name] {
		unlock, err := st.Lock(ctx, 10*time.Minute)
		if err != nil {
			fmt.Fprintln(os.Stderr, "✗", err)
			return run.ExitRetry
		}
		defer unlock()
	}
	runID, err := st.StartRun(ctx, name, strings.Join(rest, " "))
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗ 실행 기록 실패:", err)
		return run.ExitFail
	}
	code := dispatchInvest(ctx, app, name, rest)
	if err := st.FinishRun(context.Background(), runID, code, buf.String()); err != nil {
		fmt.Fprintln(os.Stderr, "⚠ 실행 결과 기록 실패:", err)
	}
	// deadman: status가 정상이면 외부 하트비트에 신호를 보낸다. 신호가 끊기면 그쪽에서 알린다 —
	// VM 자체가 죽은 것은 이 프로세스 안에서는 알 수 없다
	if name == "status" && code == run.ExitOK {
		pingHeartbeat(ctx, os.Getenv("INVEST_HEARTBEAT_URL"))
	}
	return code
}

func pingHeartbeat(ctx context.Context, url string) {
	if url == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "⚠ 하트비트 URL 이상:", err)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "⚠ 하트비트 전송 실패:", err)
		return
	}
	resp.Body.Close()
}

func dispatchInvest(ctx context.Context, app *run.App, name string, args []string) int {
	fs := flag.NewFlagSet("invest "+name, flag.ContinueOnError)
	fs.SetOutput(app.Err)
	date := fs.String("date", "", "YYYY-MM-DD (테스트 전용, INVEST_UNSAFE_DATE=1 필요)")
	dry := fs.Bool("dry-run", false, "아무것도 쓰지 않는다")
	var fills multiFlag
	fs.Var(&fills, "fill", "실제 체결단가 SYM=PRICE (여러 번)")
	fx := fs.Float64("fx", 0, "증권사 실제 적용 환율. 생략 시 시장 환율로 추정")
	var shares optInt
	fs.Var(&shares, "shares", "실제 체결 수량")
	var paid optFloat
	fs.Var(&paid, "paid", "실제로 넣은 원화 (수수료 포함)")
	symbol := fs.String("symbol", "", "종목")
	reason := fs.String("reason", "", "사유")
	auto := fs.Bool("auto", false, "증권사 잔고에서 수량·매입금액을 읽어 채움")
	retries := fs.Int("retries", 0, "결측·빈 응답일 때 다시 받을 횟수")
	wait := fs.Duration("wait", 15*time.Minute, "재시도 간격")
	amount := fs.Int64("amount", 0, "월 적립액(원)")
	from := fs.String("from", "", "적용 시작월 YYYY-MM")
	year := fs.Int("year", 0, "특정 연도만")
	paidYear := fs.Int("paid-year", 0, "그 연도분을 납부했다고 기록")
	applyCash := fs.Bool("apply-cash", false, "현금 차이만 앱에 반영")
	stateDir := fs.String("state", "", "TQ state/ 디렉터리")
	master := fs.String("master", "", "master.csv (scripts/tq_export.py)")
	unit := fs.String("unit", "", "실패한 systemd 유닛 이름")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return run.ExitFail
	}
	parsedFills, err := run.ParseFills(fills)
	if err != nil {
		fmt.Fprintln(app.Err, "✗", err)
		return run.ExitFail
	}

	switch name {
	case "refresh":
		return app.Refresh(ctx, run.RefreshOpts{Retries: *retries, Wait: *wait})
	case "judge":
		return app.Judge(ctx, run.JudgeOpts{Date: *date, DryRun: *dry})
	case "order":
		return app.Order(ctx, run.OrderOpts{Date: *date})
	case "trade":
		return app.Trade(ctx, run.TradeOpts{Date: *date, DryRun: *dry, FX: *fx})
	case "execute":
		return app.Execute(ctx, run.ExecuteOpts{Date: *date, DryRun: *dry, Fills: parsedFills, FX: *fx, Shares: shares.v, Paid: paid.v})
	case "amend":
		return app.Amend(ctx, run.AmendOpts{Date: *date, DryRun: *dry, Fills: parsedFills, FX: *fx})
	case "adjust":
		return app.Adjust(ctx, run.AdjustOpts{Date: *date, DryRun: *dry, Shares: shares.v, Paid: paid.v, Symbol: *symbol, Auto: *auto, FX: *fx, Reason: *reason})
	case "tax":
		return app.Tax(ctx, run.TaxOpts{Year: *year, Paid: *paidYear, DryRun: *dry})
	case "contribute":
		return app.Contribute(ctx, run.ContributeOpts{Date: *date, Amount: *amount, EffectiveFrom: *from, Reason: *reason})
	case "deviate":
		return app.Deviate(ctx, run.DeviateOpts{Date: *date, Symbol: *symbol, Reason: *reason})
	case "cancel":
		return app.Cancel(ctx)
	case "ledger":
		return app.Ledger(ctx)
	case "status":
		return app.Status(ctx, run.StatusOpts{Date: *date})
	case "verify":
		return app.Verify(ctx, *date)
	case "why":
		return app.Why(ctx, *date)
	case "reconcile":
		return app.Reconcile(ctx, run.ReconcileOpts{ApplyCash: *applyCash})
	case "alert":
		var m run.Mailer
		if c, err := run.SMTPFromEnv(); err != nil {
			fmt.Fprintln(app.Err, "⚠", err)
		} else {
			m = c
		}
		return app.Alert(ctx, m, run.AlertOpts{Unit: *unit})
	case "import-tq":
		return app.ImportTQ(ctx, run.ImportOpts{StateDir: *stateDir, MasterCSV: *master, DryRun: *dry})
	}
	fmt.Fprintf(app.Err, "✗ 알 수 없는 명령: %s\n%s\n", name, investUsage)
	return run.ExitFail
}
