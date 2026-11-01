package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yuntyu01/MyHome/server/internal/anonymize"
	"github.com/yuntyu01/MyHome/server/internal/config"
	"github.com/yuntyu01/MyHome/server/internal/db"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
	"github.com/yuntyu01/MyHome/server/internal/parser"
	"github.com/yuntyu01/MyHome/server/internal/period"
	"github.com/yuntyu01/MyHome/server/internal/pipeline"
)

func newProcessor(cfg config.Config, pool *pgxpool.Pool, logger *slog.Logger) (*pipeline.Processor, error) {
	reg, err := parser.NewRegistry(cfg.ParserPackages)
	if err != nil {
		return nil, err
	}
	return &pipeline.Processor{Pool: pool, Registry: reg, Logger: logger}, nil
}

// dateRange는 --from, --to(YYYY-MM-DD, KST, to 포함)를 [from, to+1일)로 바꾼다.
func dateRange(from, to string) (time.Time, time.Time, error) {
	f, err := time.ParseInLocation("2006-01-02", from, period.KST)
	if err != nil {
		return f, f, errors.New("--from은 YYYY-MM-DD")
	}
	t, err := time.ParseInLocation("2006-01-02", to, period.KST)
	if err != nil {
		return f, t, errors.New("--to는 YYYY-MM-DD")
	}
	return f, t.AddDate(0, 0, 1), nil
}

func reprocessCmd(ctx context.Context, cfg config.Config, logger *slog.Logger, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("reprocess", flag.ContinueOnError)
	from := fs.String("from", "", "시작일 YYYY-MM-DD (KST)")
	to := fs.String("to", "", "종료일 YYYY-MM-DD (KST, 포함)")
	apply := fs.Bool("apply", false, "실제로 적용 (없으면 계획만 출력)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	f, t, err := dateRange(*from, *to)
	if err != nil {
		return err
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
	rep, err := proc.Reprocess(ctx, f, t, *apply)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "기간 %s ~ %s\n원문 %d건 재처리%s, 수동 수정으로 건너뜀 %d건, 자동 거래 %d건 삭제 후 재생성\n",
		*from, *to, rep.RawEvents, map[bool]string{true: "함", false: " 예정"}[rep.Applied], rep.SkippedManual, rep.DeletedDerived)
	if rep.Applied {
		fmt.Fprintf(out, "새 거래 %d건, 해석 실패 %d건\n", rep.Created, rep.ParseErrors)
	} else {
		fmt.Fprintln(out, "적용하려면 --apply를 붙이세요.")
	}
	return nil
}

func processPendingCmd(ctx context.Context, cfg config.Config, logger *slog.Logger, out io.Writer) error {
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	proc, err := newProcessor(cfg, pool, logger)
	if err != nil {
		return err
	}
	n, err := proc.ProcessPending(ctx, 100000)
	fmt.Fprintf(out, "%d건 처리\n", n)
	return err
}

// exportRawCmd는 원문을 익명화해서 JSONL로 내보낸다. 파서 보정용 픽스처를 만들 때 쓴다.
func exportRawCmd(ctx context.Context, cfg config.Config, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("export-raw", flag.ContinueOnError)
	from := fs.String("from", "", "시작일 YYYY-MM-DD (KST)")
	to := fs.String("to", "", "종료일 YYYY-MM-DD (KST, 포함)")
	outPath := fs.String("out", "", "출력 파일(없으면 표준출력)")
	failedOnly := fs.Bool("failed", false, "해석 실패한 원문만")
	if err := fs.Parse(args); err != nil {
		return err
	}
	f, t, err := dateRange(*from, *to)
	if err != nil {
		return err
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	reg, err := parser.NewRegistry(cfg.ParserPackages)
	if err != nil {
		return err
	}
	raws, err := dbq.New(pool).ListRawEventsInRange(ctx, dbq.ListRawEventsInRangeParams{FromAt: f, ToAt: t})
	if err != nil {
		return err
	}
	w := stdout
	if *outPath != "" {
		file, err := os.OpenFile(*outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		defer file.Close()
		w = file
	}
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	enc.SetEscapeHTML(false)
	n := 0
	for _, r := range raws {
		if *failedOnly && r.ParseError == nil {
			continue
		}
		ev := parser.Event{Package: r.SourcePackage, Title: r.Title, Text: r.Text, BigText: r.BigText, PostedAt: r.PostedAt}
		if err := enc.Encode(anonymize.Export(r.ID, ev, r.ParseError, reg)); err != nil {
			return err
		}
		n++
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d건 내보냄. 공유 전에 내용을 직접 한 번 더 확인하세요.\n", n)
	return nil
}
