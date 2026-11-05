package contextwindow

import "testing"

func TestLimitKnownVendorsDefaultWindow(t *testing.T) {
	cases := map[string]int{"claude": 200_000, "Codex": 200_000, "cursor": 200_000}
	for vendor, want := range cases {
		if got := (Limits{}).Limit(vendor); got != want {
			t.Fatalf("Limit(%q) = %d, want %d", vendor, got, want)
		}
	}
}

func TestLimitFallsBackForUnknownVendor(t *testing.T) {
	if got := (Limits{}).Limit("nobody-ships-this"); got != Fallback() {
		t.Fatalf("Limit = %d, want fallback %d", got, Fallback())
	}
}

func TestLimitTableOverridesTheDefaultForItsVendor(t *testing.T) {
	limits := NewLimits("", map[string]int{"codex": 400_000})
	if got := limits.Limit("codex"); got != 400_000 {
		t.Fatalf("Limit(codex) = %d, want the table's 400000", got)
	}
	if got := limits.Limit("Codex"); got != 400_000 {
		t.Fatalf("Limit(Codex) = %d, want the table's 400000 whatever the case", got)
	}
	if got := limits.Limit("claude"); got != 200_000 {
		t.Fatalf("Limit(claude) = %d, want the default 200000 untouched by codex's entry", got)
	}
}

func TestLimitEnvWinsOverTheTableForEveryVendor(t *testing.T) {
	limits := NewLimits("12345", map[string]int{"codex": 400_000})
	for _, vendor := range []string{"codex", "claude", "nobody-ships-this"} {
		if got := limits.Limit(vendor); got != 12345 {
			t.Fatalf("Limit(%s) = %d, want the env's 12345", vendor, got)
		}
	}
}

func TestLimitIgnoresInvalidEnv(t *testing.T) {
	for _, raw := range []string{"0", "-3", "many"} {
		limits := NewLimits(raw, map[string]int{"codex": 400_000})
		if got := limits.Limit("codex"); got != 400_000 {
			t.Fatalf("Limit(codex) with env %q = %d, want the table's 400000", raw, got)
		}
		if got := limits.Limit("claude"); got != 200_000 {
			t.Fatalf("Limit(claude) with env %q = %d, want 200000", raw, got)
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

func TestLimitForPrefersOverrideAndTableThenModelWindow(t *testing.T) {
	if got := (Limits{}).LimitFor("claude", 350_000); got != 350_000 {
		t.Fatalf("model window ignored: got %d, want 350000", got)
	}
	if got := (Limits{}).LimitFor("claude", 0); got != 200_000 {
		t.Fatalf("zero model window must fall back to the built-in 200000, got %d", got)
	}
	table := NewLimits("", map[string]int{"claude": 123_000})
	if got := table.LimitFor("claude", 350_000); got != 123_000 {
		t.Fatalf("config table must win over the model window, got %d", got)
	}
	env := NewLimits("999", nil)
	if got := env.LimitFor("claude", 350_000); got != 999 {
		t.Fatalf("env override must win over the model window, got %d", got)
	}
}
