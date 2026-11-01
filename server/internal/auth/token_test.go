package auth

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewTokenUniqueAndPrefixed(t *testing.T) {
	a, err := NewToken(TokenPrefix)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewToken(TokenPrefix)
	if a == b {
		t.Fatal("토큰이 같으면 안 됨")
	}
	if !strings.HasPrefix(a, TokenPrefix) || len(a) < 40 {
		t.Fatalf("토큰 형식 이상: %q", a)
	}
	if bytes.Equal(HashToken(a), HashToken(b)) || len(HashToken(a)) != 32 {
		t.Fatal("해시 이상")
	}
}

func TestBearerToken(t *testing.T) {
	cases := map[string]struct {
		tok string
		ok  bool
	}{
		"Bearer abc":   {"abc", true},
		"bearer  abc ": {"abc", true},
		"Basic abc":    {"", false},
		"Bearer ":      {"", false},
		"":             {"", false},
	}
	for h, want := range cases {
		tok, ok := BearerToken(h)
		if tok != want.tok || ok != want.ok {
			t.Errorf("BearerToken(%q) = %q,%v want %q,%v", h, tok, ok, want.tok, want.ok)
		}
	}
}
