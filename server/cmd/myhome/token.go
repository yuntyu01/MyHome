package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/auth"
	"github.com/yuntyu01/MyHome/server/internal/config"
	"github.com/yuntyu01/MyHome/server/internal/db"
	"github.com/yuntyu01/MyHome/server/internal/dbq"
)

// tokenCmd는 기기 토큰을 관리한다. 앱 연결(/app/link, Cloudflare Access 로그인)이 정상 경로이고, 이 명령은 비상용이다.
func tokenCmd(ctx context.Context, cfg config.Config, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("token issue|list|revoke 중 하나를 지정하세요")
	}
	if err := cfg.RequireDB(); err != nil {
		return err
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	q := dbq.New(pool)

	switch args[0] {
	case "issue":
		if !cfg.CLITokenIssueEnabled {
			return errors.New("CLI 토큰 발급이 꺼져 있습니다(CLI_TOKEN_ISSUE_ENABLED=false)")
		}
		fs := flag.NewFlagSet("token issue", flag.ContinueOnError)
		name := fs.String("name", "", "기기 이름 (예: galaxy-s24)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *name == "" {
			return errors.New("--name이 필요합니다")
		}
		tok, d, err := auth.IssueDeviceToken(ctx, q, *name)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "기기 #%d (%s) 토큰을 발급했습니다. 지금 한 번만 표시됩니다:\n%s\n", d.ID, d.Name, tok)
		return nil
	case "list":
		devs, err := q.ListDevices(ctx)
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\t이름\t생성\t마지막 사용\t폐기")
		for _, d := range devs {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", d.ID, d.Name, fmtTime(&d.CreatedAt), fmtTime(d.LastSeenAt), fmtTime(d.RevokedAt))
		}
		return tw.Flush()
	case "revoke":
		fs := flag.NewFlagSet("token revoke", flag.ContinueOnError)
		id := fs.Int64("id", 0, "기기 ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *id <= 0 {
			return errors.New("--id가 필요합니다")
		}
		d, err := q.RevokeDevice(ctx, *id)
		if err != nil {
			return fmt.Errorf("폐기 실패: %w", err)
		}
		fmt.Fprintf(out, "기기 #%d (%s) 토큰을 폐기했습니다.\n", d.ID, d.Name)
		return nil
	default:
		return fmt.Errorf("알 수 없는 token 명령: %s", args[0])
	}
}

func fmtTime(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.In(kst).Format("2006-01-02 15:04")
}

var kst = mustLoc("Asia/Seoul")

func mustLoc(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}
