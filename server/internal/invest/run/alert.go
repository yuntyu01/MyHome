package run

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuntyu01/MyHome/server/internal/dbq"
)

// 실패 알림 — 실패할 때만 남긴다 (TQ claude.md §7.0). 메일 대신 앱 알림으로 받는다(2026-11-08 사용자 결정).
//
// 알림이 잦으면 자주 열게 되고, 그것이 이 앱이 막으려는 것이다. 정상 흐름은 화면이 말한다.
// systemd가 실패한 유닛의 OnFailure=로 `myhome invest alert --unit <유닛>`을 부르면 invest_alerts에 한 줄을 쓰고,
// 앱이 주기 작업에서 그 줄을 읽어 폰 알림을 띄운다. 이 프로세스 밖에서 일어나는 죽음(VM 정지)은 외부 하트비트가 잡는다.

// alertBodyMax는 알림 본문 글자 수 상한이다. 전체 출력은 실행 기록(invest_runs)에 그대로 있다.
const alertBodyMax = 1500

// AlertOpts는 alert 옵션이다.
type AlertOpts struct {
	Unit   string        // 실패한 systemd 유닛 이름
	Within time.Duration // 이 시간 안의 실패 실행만 본문에 싣는다
}

// Alert는 실패 알림을 DB에 남긴다. 실패한 유닛의 마지막 실패 실행 출력을 본문에 싣는다.
func (a *App) Alert(ctx context.Context, o AlertOpts) int {
	if o.Within == 0 {
		o.Within = 6 * time.Hour
	}
	unit := strings.TrimSpace(o.Unit)
	cmd := unitCmd(unit)
	p := dbq.InsertInvestAlertParams{Unit: unit, Cmd: cmd, Title: "자동매매 " + cmdLabel(cmd, unit) + " 실패"}
	var body strings.Builder
	runs, err := a.Store.LatestRuns(ctx)
	found := false
	if err == nil {
		for _, r := range runs {
			if (cmd == "" || r.Cmd == cmd || (cmd == "judge" && r.Cmd == "refresh")) && r.ExitCode != nil && *r.ExitCode != 0 && a.now().Sub(r.StartedAt) <= o.Within {
				found = true
				id, code := r.ID, *r.ExitCode
				p.RunID, p.ExitCode = &id, &code
				p.Title += fmt.Sprintf(" (exit %d)", code)
				fmt.Fprintf(&body, "%s 시작 · myhome invest %s %s\n%s", r.StartedAt.In(kst).Format("1월 2일 15:04"), r.Cmd, r.Args, lastLines(r.Output, alertBodyMax))
				break
			}
		}
	}
	if !found {
		fmt.Fprintf(&body, "실행 기록에 최근 실패가 없다. DB에 닿기 전에 죽었을 수 있다.\nVM에서: journalctl -u %s -n 100", orDefault(unit, "<유닛>"))
		if err != nil {
			fmt.Fprintf(&body, "\n실행 기록 조회 실패: %v", err)
		}
	}
	p.Body = strings.TrimSpace(body.String())
	if _, err := a.Store.AddAlert(ctx, p); err != nil {
		return a.fail(ExitFail, "실패 알림 기록 실패: %v\n%s\n%s", err, p.Title, p.Body)
	}
	a.printf("실패 알림 기록 — %s", p.Title)
	return ExitOK
}

// unitCmd는 myhome-invest-<명령>.service에서 명령을 꺼낸다.
func unitCmd(unit string) string {
	u := strings.TrimSuffix(unit, ".service")
	if c, ok := strings.CutPrefix(u, "myhome-invest-"); ok {
		return c
	}
	return ""
}

// cmdLabel은 알림 제목에 쓸 사람 말이다(웹 투자 화면과 같은 이름).
func cmdLabel(cmd, unit string) string {
	switch cmd {
	case "refresh":
		return "가격 갱신"
	case "judge":
		return "월말 판정"
	case "trade":
		return "주문"
	case "status":
		return "상태 점검"
	}
	return orDefault(cmd, orDefault(unit, "작업"))
}

// lastLines는 출력의 끝쪽 max 글자다. 실패 이유는 보통 마지막 줄에 있다.
func lastLines(s string, max int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	cut := string(r[len(r)-max:])
	if i := strings.IndexByte(cut, '\n'); i >= 0 && i < len(cut)-1 {
		cut = cut[i+1:]
	}
	return "…\n" + cut
}
