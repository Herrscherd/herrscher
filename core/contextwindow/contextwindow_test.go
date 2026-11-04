package contextwindow

import "testing"

func TestLimitKnownVendors(t *testing.T) {
	t.Setenv(EnvVar(), "")
	cases := map[string]int{"claude": 200_000, "Codex": 200_000, "cursor": 200_000}
	for vendor, want := range cases {
		if got := Limit(vendor); got != want {
			t.Fatalf("Limit(%q) = %d, want %d", vendor, got, want)
		}
	}
}

func TestLimitCodexIsTheCautiousWindow(t *testing.T) {
	t.Setenv(EnvVar(), "")
	if got := Limit("codex"); got != 200_000 {
		t.Fatalf("Limit(codex) = %d, want the cautious 200000", got)
	}
}

func TestLimitCodexIsRaisedByTheEnv(t *testing.T) {
	t.Setenv(EnvVar(), "400000")
	if got := Limit("codex"); got != 400_000 {
		t.Fatalf("Limit(codex) with HERRSCHER_CONTEXT_LIMIT=400000 = %d, want 400000", got)
	}
}

func TestLimitFallsBackForUnknownVendor(t *testing.T) {
	t.Setenv(EnvVar(), "")
	if got := Limit("nobody-ships-this"); got != Fallback() {
		t.Fatalf("Limit = %d, want fallback %d", got, Fallback())
	}
}

func TestLimitEnvOverridesWhenPositive(t *testing.T) {
	t.Setenv(EnvVar(), "12345")
	if got := Limit("claude"); got != 12345 {
		t.Fatalf("Limit = %d, want 12345", got)
	}
}

func TestLimitIgnoresInvalidEnv(t *testing.T) {
	for _, raw := range []string{"0", "-3", "many"} {
		t.Setenv(EnvVar(), raw)
		if got := Limit("claude"); got != 200_000 {
			t.Fatalf("Limit with %q = %d, want 200000", raw, got)
		}
	}
}

func TestCheckOverrideReportsOnlyInvalidValues(t *testing.T) {
	cases := map[string]bool{"": false, "12345": false, "0": true, "-3": true, "many": true}
	for raw, wantErr := range cases {
		if err := CheckOverride(raw); (err != nil) != wantErr {
			t.Fatalf("CheckOverride(%q) = %v, want error %t", raw, err, wantErr)
		}
	}
}
