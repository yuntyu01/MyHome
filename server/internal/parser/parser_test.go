package parser

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

type fixtureFile struct {
	Note  string    `json:"_note"`
	Cases []fixture `json:"cases"`
}

type fixture struct {
	Name     string `json:"name"`
	Package  string `json:"package"`
	Title    string `json:"title"`
	Text     string `json:"text"`
	BigText  string `json:"big_text"`
	PostedAt string `json:"posted_at"`
	Want     struct {
		Error       bool    `json:"error"`
		Source      string  `json:"source"`
		Kind        Kind    `json:"kind"`
		Amount      int64   `json:"amount"`
		Merchant    *string `json:"merchant"`
		Balance     *int64  `json:"balance"`
		Installment int     `json:"installment"`
		OccurredAt  string  `json:"occurred_at"`
	} `json:"want"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	b, err := os.ReadFile("testdata/synthetic/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var f fixtureFile
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if f.Note == "" {
		t.Fatal("합성 픽스처에는 실제 형식이 아니라는 _note가 있어야 함")
	}
	return f.Cases
}

func TestSyntheticFixtures(t *testing.T) {
	// 테스트용 패키지명 매핑. 실제 패키지명은 PARSER_PACKAGES 환경변수로 준다.
	reg, err := NewRegistry("kb=test.kb,test.kbpay;toss=test.toss")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range loadFixtures(t) {
		t.Run(c.Name, func(t *testing.T) {
			posted, err := time.Parse(time.RFC3339, c.PostedAt)
			if err != nil {
				t.Fatal(err)
			}
			got, err := reg.Parse(Event{Package: c.Package, Title: c.Title, Text: c.Text, BigText: c.BigText, PostedAt: posted})
			if c.Want.Error {
				if !errors.Is(err, ErrNoAmount) {
					t.Fatalf("해석 실패를 기대했지만 err=%v, got=%+v", err, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got.Source != c.Want.Source {
				t.Errorf("source = %q, want %q", got.Source, c.Want.Source)
			}
			if got.Kind != c.Want.Kind {
				t.Errorf("kind = %q, want %q", got.Kind, c.Want.Kind)
			}
			if got.Amount != c.Want.Amount {
				t.Errorf("amount = %d, want %d", got.Amount, c.Want.Amount)
			}
			if c.Want.Merchant != nil && got.Merchant != *c.Want.Merchant {
				t.Errorf("merchant = %q, want %q", got.Merchant, *c.Want.Merchant)
			}
			if c.Want.Balance != nil && (got.Balance == nil || *got.Balance != *c.Want.Balance) {
				t.Errorf("balance = %v, want %d", got.Balance, *c.Want.Balance)
			}
			if got.Installment != c.Want.Installment {
				t.Errorf("installment = %d, want %d", got.Installment, c.Want.Installment)
			}
			if c.Want.OccurredAt != "" {
				want, _ := time.Parse(time.RFC3339, c.Want.OccurredAt)
				if !got.OccurredAt.Equal(want) {
					t.Errorf("occurred_at = %v, want %v", got.OccurredAt, want)
				}
			}
		})
	}
}

func TestRegistryDetectsByContent(t *testing.T) {
	reg, _ := NewRegistry("")
	ev := Event{Package: "unknown.pkg", Title: "KB국민카드", Text: "1,000원 승인"}
	if reg.For(ev).Name() != "kb" {
		t.Errorf("KB 내용 감지 실패: %s", reg.For(ev).Name())
	}
	ev = Event{Package: "unknown.pkg", Title: "토스", Text: "1,000원"}
	if reg.For(ev).Name() != "toss" {
		t.Errorf("토스 내용 감지 실패")
	}
	ev = Event{Package: "unknown.pkg", Title: "알림", Text: "1,000원"}
	if reg.For(ev).Name() != "generic" {
		t.Errorf("범용 파서 기대")
	}
}

func TestRegistryMapping(t *testing.T) {
	reg, err := NewRegistry(" kakaopay = a.b , c.d ; kb=e.f ")
	if err != nil {
		t.Fatal(err)
	}
	if reg.For(Event{Package: "c.d"}).Name() != "kakaopay" {
		t.Error("범용 파서 별칭 매핑 실패")
	}
	if reg.For(Event{Package: "e.f"}).Name() != "kb" {
		t.Error("내장 파서 매핑 실패")
	}
	if _, err := NewRegistry("noequals"); err == nil {
		t.Error("형식 오류를 잡아야 함")
	}
}
