package config

import "testing"

func TestNormalizeDatabaseURL(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"postgresql+psycopg2://app:pass@postgres:5432/app?sslmode=disable", "postgresql://app:pass@postgres:5432/app?sslmode=disable"},
		{"postgres://user:pass@host/db", "postgres://user:pass@host/db"},
		{"host=localhost user=app", "host=localhost user=app"},
	} {
		if got := normalizeDatabaseURL(tc.input); got != tc.want {
			t.Errorf("normalizeDatabaseURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

