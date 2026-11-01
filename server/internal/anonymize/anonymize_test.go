package anonymize

import (
	"strings"
	"testing"
	"time"

	"github.com/yuntyu01/MyHome/server/internal/parser"
)

// 합성 데이터
func TestTextMasksPersonalInfo(t *testing.T) {
	in := "홍*동님 1234*5678 카드 12,345원 일시불 스타벅스 누적 1,234,567원 잔액: 500,000원 계좌 110-123-456789 문의 010-1234-5678 김철수님"
	got := Text(in)
	for _, leak := range []string{"홍*동", "1234*5678", "1,234,567", "500,000", "110-123-456789", "010-1234-5678", "김철수"} {
		if strings.Contains(got, leak) {
			t.Errorf("%q가 남아 있음: %s", leak, got)
		}
	}
	for _, keep := range []string{"12,345원", "스타벅스", "일시불"} {
		if !strings.Contains(got, keep) {
			t.Errorf("%q는 남아야 함: %s", keep, got)
		}
	}
}

func TestExportMasksCounterparty(t *testing.T) {
	reg, _ := parser.NewRegistry("toss=t")
	ev := parser.Event{Package: "t", Title: "토스", Text: "박지성에게 25,000원을 보냈어요", PostedAt: time.Now()}
	rec := Export(1, ev, nil, reg)
	if strings.Contains(rec.Text, "박지성") {
		t.Errorf("송금 상대 이름이 남음: %s", rec.Text)
	}
	if rec.Parsed == nil || rec.Parsed.Amount != 25000 || rec.Parsed.Merchant != "[상대방]" {
		t.Errorf("파서 결과 요약: %+v", rec.Parsed)
	}
}
