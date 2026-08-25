package schedule

import (
	"testing"
	"time"
)

func TestParseCronRejectsWhatIsNotAnExpression(t *testing.T) {
	for _, expr := range []string{
		"",              // rien
		"0 9 * *",       // quatre champs
		"0 9 * * * *",   // six champs
		"60 9 * * *",    // minute hors bornes
		"0 24 * * *",    // heure hors bornes
		"0 9 0 * *",     // jour du mois a 0
		"0 9 * 13 *",    // mois hors bornes
		"0 9 * * 7",     // jour de semaine hors bornes
		"*/0 9 * * *",   // pas nul
		"30-10 9 * * *", // plage inversee
		"a 9 * * *",     // pas un nombre
		"5/2 9 * * *",   // un pas sur une valeur seule, lu differemment selon les crons
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
		{"0 9 * * 1-5", "2026-08-25 09:00", true},  // 25/08/2026 est un mardi
		{"0 9 * * 1-5", "2026-08-29 09:00", false}, // un samedi
		{"0 0,12 * * *", "2026-08-25 12:00", true},
		{"0 0,12 * * *", "2026-08-25 06:00", false},
		// Regle Vixie : jour-du-mois et jour-de-semaine tous deux restreints se
		// lisent en OU, pas en ET.
		{"0 9 1 * 1", "2026-08-01 09:00", true}, // le 1er, un samedi
		{"0 9 1 * 1", "2026-08-24 09:00", true}, // un lundi qui n'est pas le 1er
		{"0 9 1 * 1", "2026-08-25 09:00", false},
		// "*/2" commence par une etoile, donc il compte comme une etoile pour la
		// regle des deux jours : le ET s'applique, pas le OU.
		{"0 9 */2 * 1-5", "2026-08-29 09:00", false}, // un samedi, jour impair
		{"0 9 */2 * 1-5", "2026-08-25 09:00", true},  // un mardi, jour impair
		{"0 9 */2 * 1-5", "2026-08-26 09:00", false}, // un mercredi, jour pair
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
