package schedule

import (
	"testing"
	"time"
)

func TestParseCronRejectsWhatIsNotAnExpression(t *testing.T) {
	for _, expr := range []string{
		"",
		"0 9 * *",
		"0 9 * * * *",
		"60 9 * * *",
		"0 24 * * *",
		"0 9 0 * *",
		"0 9 * 13 *",
		"0 9 * * 7",
		"*/0 9 * * *",
		"30-10 9 * * *",
		"a 9 * * *",
		"5/2 9 * * *",
	} {
		if _, err := parseCron(expr); err == nil {
			t.Errorf("parseCron(%q) accepted, want an error", expr)
		}
	}
}

func TestCronMatchesTheMinutesItDescribes(t *testing.T) {
	at := func(s string) time.Time {
		got, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
		if err != nil {
			t.Fatalf("bad fixture %q: %v", s, err)
		}
		return got
	}
	cases := []struct {
		expr string
		when string
		want bool
	}{
		{"* * * * *", "2026-08-25 13:37", true},
		{"0 9 * * *", "2026-08-25 09:00", true},
		{"0 9 * * *", "2026-08-25 09:01", false},
		{"*/15 * * * *", "2026-08-25 09:30", true},
		{"*/15 * * * *", "2026-08-25 09:31", false},
		{"0 9 * * 1-5", "2026-08-25 09:00", true},
		{"0 9 * * 1-5", "2026-08-29 09:00", false},
		{"0 0,12 * * *", "2026-08-25 12:00", true},
		{"0 0,12 * * *", "2026-08-25 06:00", false},
		{"0 9 1 * 1", "2026-08-01 09:00", true},
		{"0 9 1 * 1", "2026-08-24 09:00", true},
		{"0 9 1 * 1", "2026-08-25 09:00", false},
		{"0 9 */2 * 1-5", "2026-08-29 09:00", false},
		{"0 9 */2 * 1-5", "2026-08-25 09:00", true},
		{"0 9 */2 * 1-5", "2026-08-26 09:00", false},
	}
	for _, tc := range cases {
		spec, err := parseCron(tc.expr)
		if err != nil {
			t.Fatalf("parseCron(%q): %v", tc.expr, err)
		}
		if got := spec.matches(at(tc.when)); got != tc.want {
			t.Errorf("parseCron(%q).matches(%s) = %v, want %v", tc.expr, tc.when, got, tc.want)
		}
	}
}
