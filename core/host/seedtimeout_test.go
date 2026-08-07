package host

import (
	"testing"
	"time"
)

func noEnv(string) string { return "" }

func TestResolveSeedTimeoutUnsetMeansZero(t *testing.T) {
	got, err := resolveSeedTimeout("", noEnv)
	if err != nil {
		t.Fatalf("resolveSeedTimeout: %v", err)
	}
	if got != 0 {
		t.Fatalf("got %v, want 0 — an unset timeout must leave the historical cap alone", got)
	}
}

func TestResolveSeedTimeoutParamWins(t *testing.T) {
	env := func(k string) string {
		if k == EnvSeedTimeout {
			return "5s"
		}
		return ""
	}
	got, err := resolveSeedTimeout("30m", env)
	if err != nil {
		t.Fatalf("resolveSeedTimeout: %v", err)
	}
	if got != 30*time.Minute {
		t.Fatalf("got %v, want 30m", got)
	}
}

func TestResolveSeedTimeoutFallsBackToEnv(t *testing.T) {
	env := func(k string) string {
		if k == EnvSeedTimeout {
			return "45s"
		}
		return ""
	}
	got, err := resolveSeedTimeout("", env)
	if err != nil {
		t.Fatalf("resolveSeedTimeout: %v", err)
	}
	if got != 45*time.Second {
		t.Fatalf("got %v, want 45s", got)
	}
}

func TestResolveSeedTimeoutRefusesBadValues(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		env  string
	}{
		{"param illisible", "bientôt", ""},
		{"param nul", "0s", ""},
		{"param négatif", "-1m", ""},
		{"env illisible", "", "bientôt"},
		{"env nul", "", "0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := func(k string) string {
				if k == EnvSeedTimeout {
					return tc.env
				}
				return ""
			}
			if _, err := resolveSeedTimeout(tc.raw, env); err == nil {
				t.Fatalf("resolveSeedTimeout(%q, env=%q) accepted a value it must refuse", tc.raw, tc.env)
			}
		})
	}
}
