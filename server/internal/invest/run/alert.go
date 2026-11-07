package run

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"
)

// 실패 알림 — 메일은 실패할 때만 보낸다 (TQ claude.md §7.0).
//
// 알림이 잦으면 자주 열게 되고, 그것이 이 앱이 막으려는 것이다. 정상 흐름은 화면이 말한다.
// systemd가 실패한 유닛의 OnFailure=로 `myhome invest alert --unit <유닛>`을 부른다.
// 이 프로세스 밖에서 일어나는 죽음(VM 정지)은 외부 하트비트가 잡는다.

// Mailer는 메일 발송이다. 테스트가 바꿔 끼운다.
type Mailer interface {
	Send(subject, body string) error
}

// SMTPConfig는 실패 알림 메일 설정이다 (TQ와 같은 환경변수 이름).
type SMTPConfig struct {
	From, Password string
	To             []string
	Host           string
	Port           int
}

// SMTPFromEnv는 MAIL_FROM·MAIL_TO·GMAIL_APP_PASSWORD(+선택 SMTP_HOST·SMTP_PORT)를 읽는다.
func SMTPFromEnv() (SMTPConfig, error) {
	c := SMTPConfig{From: os.Getenv("MAIL_FROM"), Password: os.Getenv("GMAIL_APP_PASSWORD"), Host: os.Getenv("SMTP_HOST"), Port: 465}
	var missing []string
	for k, v := range map[string]string{"MAIL_FROM": c.From, "MAIL_TO": os.Getenv("MAIL_TO"), "GMAIL_APP_PASSWORD": c.Password} {
		if strings.TrimSpace(v) == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return c, fmt.Errorf("메일 설정 누락: %s — 메일 없이는 발송하지 않는다", strings.Join(missing, ", "))
	}
	for _, r := range strings.FieldsFunc(os.Getenv("MAIL_TO"), func(r rune) bool { return r == ',' || r == ';' }) {
		if r = strings.TrimSpace(r); r != "" {
			c.To = append(c.To, r)
		}
	}
	if c.Host == "" {
		c.Host = "smtp.gmail.com"
	}
	if p := os.Getenv("SMTP_PORT"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			return c, fmt.Errorf("SMTP_PORT가 정수가 아니다: %q", p)
		}
		c.Port = n
	}
	return c, nil
}

// Send는 SMTP over TLS(465)로 평문 메일을 보낸다.
func (c SMTPConfig) Send(subject, body string) error {
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 20 * time.Second}, "tcp", addr, &tls.Config{ServerName: c.Host})
	if err != nil {
		return err
	}
	cl, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return err
	}
	defer cl.Close()
	if err := cl.Auth(smtp.PlainAuth("", c.From, c.Password, c.Host)); err != nil {
		return err
	}
	if err := cl.Mail(c.From); err != nil {
		return err
	}
	for _, r := range c.To {
		if err := cl.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := cl.Data()
	if err != nil {
		return err
	}
	msg := "From: " + c.From + "\r\nTo: " + strings.Join(c.To, ", ") +
		"\r\nSubject: " + mimeHeader(subject) +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n" +
		strings.ReplaceAll(body, "\n", "\r\n")
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return cl.Quit()
}

func mimeHeader(s string) string {
	return "=?UTF-8?B?" + b64(s) + "?="
}

// AlertOpts는 alert 옵션이다.
type AlertOpts struct {
	Unit   string        // 실패한 systemd 유닛 이름
	Within time.Duration // 이 시간 안의 실패 실행만 본문에 싣는다
}

// Alert는 실패 알림 메일을 보낸다. 실패한 유닛의 마지막 실패 실행 출력을 본문에 싣는다.
func (a *App) Alert(ctx context.Context, m Mailer, o AlertOpts) int {
	if o.Within == 0 {
		o.Within = 6 * time.Hour
	}
	unit := strings.TrimSpace(o.Unit)
	cmd := unitCmd(unit)
	var body strings.Builder
	subject := "🚨 [myhome] 자동매매 " + orDefault(cmd, unit) + " 실패"
	runs, err := a.Store.LatestRuns(ctx)
	found := false
	if err == nil {
		for _, r := range runs {
			if (cmd == "" || r.Cmd == cmd || (cmd == "judge" && r.Cmd == "refresh")) && r.ExitCode != nil && *r.ExitCode != 0 && a.now().Sub(r.StartedAt) <= o.Within {
				found = true
				subject += fmt.Sprintf(" (exit %d)", *r.ExitCode)
				fmt.Fprintf(&body, "명령    myhome invest %s %s\n시작    %s\n종료 코드 %d\n\n%s\n", r.Cmd, r.Args, r.StartedAt.In(kst).Format("2006-01-02 15:04:05 KST"), *r.ExitCode, r.Output)
				break
			}
		}
	}
	if !found {
		fmt.Fprintf(&body, "실행 기록에 최근 실패가 없다 — DB에 닿기 전에 죽었을 수 있다.\n확인: journalctl -u %s -n 100\n", orDefault(unit, "<유닛>"))
		if err != nil {
			fmt.Fprintf(&body, "실행 기록 조회 실패: %v\n", err)
		}
	}
	fmt.Fprintf(&body, "\n유닛 %s\n상태: myhome invest status · 원금: myhome invest ledger\n", orDefault(unit, "(모름)"))
	if m == nil {
		return a.fail(ExitFail, "메일 설정이 없어 실패 알림을 보내지 못했다:\n%s", body.String())
	}
	if err := m.Send(subject, body.String()); err != nil {
		return a.fail(ExitFail, "실패 알림 발송 실패: %v", err)
	}
	a.printf("실패 알림 발송 — %s", subject)
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

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
