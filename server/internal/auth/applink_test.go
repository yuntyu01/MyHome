package auth

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// RFC 7636 부록 B의 예시 값.
func TestS256RFC7636(t *testing.T) {
	if got := S256("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"); got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("S256 = %s", got)
	}
}

func TestLinkParamValidation(t *testing.T) {
	good := S256("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	for c, want := range map[string]bool{
		good:                          true,
		good[:42]:                     false,
		good + "A":                    false,
		strings.Repeat("=", 43):       false,
		good[:42] + "+":               false,
		"E9Melhoa2OwvFrEMTJguCHaoeK1": false,
	} {
		if ValidChallenge(c) != want {
			t.Errorf("ValidChallenge(%q) != %v", c, want)
		}
	}
	for s, want := range map[string]bool{
		strings.Repeat("a", 16): true, strings.Repeat("a", 64): true, "abcDEF-_0123456789": true,
		strings.Repeat("a", 15): false, strings.Repeat("a", 65): false, "abcdefgh/ijklmnop": false, "": false,
	} {
		if ValidState(s) != want {
			t.Errorf("ValidState(%q) != %v", s, want)
		}
	}
	for v, want := range map[string]bool{
		strings.Repeat("a", 43): true, strings.Repeat("a", 128): true, strings.Repeat("a", 40) + "-._~": true,
		strings.Repeat("a", 42): false, strings.Repeat("a", 129): false, strings.Repeat("a", 42) + "+": false,
	} {
		if validVerifier(v) != want {
			t.Errorf("validVerifier(len %d) != %v", len(v), want)
		}
	}
}

func TestDeviceName(t *testing.T) {
	if got := deviceName("  갤럭시  "); got != "갤럭시" {
		t.Errorf("trim: %q", got)
	}
	if got := deviceName(""); !strings.HasPrefix(got, "android-") || len(got) != len("android-20261108") {
		t.Errorf("기본 이름: %q", got)
	}
	if got := deviceName(strings.Repeat("가", 70)); utf8.RuneCountInString(got) != 60 {
		t.Errorf("60자로 자르기: %d", utf8.RuneCountInString(got))
	}
}
