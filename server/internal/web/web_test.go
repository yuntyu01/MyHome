package web

import "testing"

func TestWonShort(t *testing.T) {
	cases := map[int64]string{
		9800:        "9,800원",
		99_999:      "99,999원",
		108_600:     "108,600원",
		3_200_000:   "320만",
		120_000_000: "1억 2,000만",
		100_000_000: "1억",
		-5_000_000:  "-500만",
	}
	for in, want := range cases {
		if got := wonShort(in); got != want {
			t.Errorf("wonShort(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestBarClass(t *testing.T) {
	if barClass(0.5) != "ok" || barClass(0.8) != "warn" || barClass(1.01) != "over" {
		t.Error("80% 경고, 100% 초과 빨강")
	}
}
