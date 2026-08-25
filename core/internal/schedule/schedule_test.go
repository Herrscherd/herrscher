package schedule

import (
	"testing"
	"time"
)

func mkTime(t *testing.T, s string) time.Time {
	t.Helper()
	got, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	if err != nil {
		t.Fatalf("bad fixture %q: %v", s, err)
	}
	return got
}

// mkUTC lit une date en UTC, pour les cas dont les tampons RFC3339 sont eux
// aussi en UTC : les melanger ferait dependre le test du fuseau de la machine.
func mkUTC(t *testing.T, s string) time.Time {
	t.Helper()
	got, err := time.ParseInLocation("2006-01-02 15:04", s, time.UTC)
	if err != nil {
		t.Fatalf("bad fixture %q: %v", s, err)
	}
	return got
}

func TestValidateRefusesAnUnusableSchedule(t *testing.T) {
	ok := Schedule{Name: "n", Agent: "a", Every: "30m", Task: "t"}
	cases := []struct {
		name string
		s    Schedule
	}{
		{"no name", Schedule{Agent: "a", Every: "30m", Task: "t"}},
		{"no task", Schedule{Name: "n", Agent: "a", Every: "30m"}},
		{"no target", Schedule{Name: "n", Every: "30m", Task: "t"}},
		{"both targets", Schedule{Name: "n", Agent: "a", Session: "s", Every: "30m", Task: "t"}},
		{"no cadence", Schedule{Name: "n", Agent: "a", Task: "t"}},
		{"both cadences", Schedule{Name: "n", Agent: "a", Every: "30m", Cron: "0 9 * * *", Task: "t"}},
		{"unreadable every", Schedule{Name: "n", Agent: "a", Every: "half an hour", Task: "t"}},
		{"non-positive every", Schedule{Name: "n", Agent: "a", Every: "0s", Task: "t"}},
		{"unreadable cron", Schedule{Name: "n", Agent: "a", Cron: "0 9 * *", Task: "t"}},
		{"unreadable grace", Schedule{Name: "n", Agent: "a", Every: "30m", Task: "t", Grace: "soon"}},
		// Sous la minute, la grace declarerait perimee chaque fenetre servie par
		// une boucle qui a le grain de la minute.
		{"grace under a minute", Schedule{Name: "n", Agent: "a", Every: "30m", Task: "t", Grace: "10s"}},
		{"negative grace", Schedule{Name: "n", Agent: "a", Every: "30m", Task: "t", Grace: "-1h"}},
		// --project decrit la session que l'horaire ouvre : sans --agent il
		// n'ouvre rien, et l'accepter le ferait croire actif.
		{"project without an agent", Schedule{Name: "n", Session: "s", Project: "p", Every: "30m", Task: "t"}},
		// Une expression que le calendrier ne satisfait jamais doit etre refusee a
		// la creation, pas boucler au premier tick.
		{"unsatisfiable cron", Schedule{Name: "n", Agent: "a", Cron: "0 0 30 2 *", Task: "t"}},
	}
	now := mkTime(t, "2026-08-25 09:00")
	if err := Validate(ok, now); err != nil {
		t.Fatalf("Validate(ok) = %v, want nil", err)
	}
	for _, tc := range cases {
		if err := Validate(tc.s, now); err == nil {
			t.Errorf("Validate(%s) accepted, want an error", tc.name)
		}
	}
}

func TestNextWalksForwardFromTheAnchor(t *testing.T) {
	after := mkTime(t, "2026-08-25 09:30")
	cases := []struct {
		name string
		s    Schedule
		want string
	}{
		{"every", Schedule{Every: "30m"}, "2026-08-25 10:00"},
		{"cron later today", Schedule{Cron: "0 11 * * *"}, "2026-08-25 11:00"},
		{"cron tomorrow", Schedule{Cron: "0 9 * * *"}, "2026-08-26 09:00"},
		// Strictement apres : une fenetre pile sur l'ancre est celle qui vient
		// d'etre tiree, pas la suivante.
		{"cron on the anchor", Schedule{Cron: "30 9 * * *"}, "2026-08-26 09:30"},
	}
	for _, tc := range cases {
		got, err := Next(tc.s, after)
		if err != nil {
			t.Fatalf("Next(%s): %v", tc.name, err)
		}
		if want := mkTime(t, tc.want); !got.Equal(want) {
			t.Errorf("Next(%s) = %s, want %s", tc.name, got, want)
		}
	}
}

func TestNextRefusesAnExpressionTheCalendarNeverSatisfies(t *testing.T) {
	if _, err := Next(Schedule{Cron: "0 0 30 2 *"}, mkTime(t, "2026-08-25 09:30")); err == nil {
		t.Error("Next on 30 February returned a window, want an error")
	}
}

func TestDueAtBothEdgesOfTheWindow(t *testing.T) {
	s := Schedule{Every: "30m", LastRun: mkTime(t, "2026-08-25 09:00").UTC().Format(time.RFC3339)}
	if Due(s, mkTime(t, "2026-08-25 09:29")) {
		t.Error("due one minute early")
	}
	if !Due(s, mkTime(t, "2026-08-25 09:30")) {
		t.Error("not due on the window")
	}
	if !Due(s, mkTime(t, "2026-08-25 09:31")) {
		t.Error("not due one minute late")
	}
}

func TestDueFallsBackToCreatedAtBeforeTheFirstRun(t *testing.T) {
	// LastRun vide veut dire jamais tire. L'ancre est alors CreatedAt, sinon un
	// horaire tout neuf considererait toutes les fenetres depuis l'epoch comme
	// ratees et tirerait immediatement.
	s := Schedule{Every: "30m", CreatedAt: mkTime(t, "2026-08-25 09:00").UTC().Format(time.RFC3339)}
	if Due(s, mkTime(t, "2026-08-25 09:10")) {
		t.Error("a fresh schedule fired before its first window")
	}
	if !Due(s, mkTime(t, "2026-08-25 09:30")) {
		t.Error("a fresh schedule did not fire on its first window")
	}
}

func TestDueOnAScheduleWithNoAnchorAtAll(t *testing.T) {
	// Ni LastRun ni CreatedAt : rien d'ou compter, donc rien ne part. Un horaire
	// ecrit a la main dans state.json ne doit pas declencher une rafale.
	if Due(Schedule{Every: "30m"}, mkTime(t, "2026-08-25 09:30")) {
		t.Error("an anchorless schedule fired")
	}
	if fire, _ := CatchUp(Schedule{Every: "30m"}, mkTime(t, "2026-08-25 09:30")); fire {
		t.Error("an anchorless schedule caught up")
	}
}

func TestCatchUpAtBothEdgesOfTheGrace(t *testing.T) {
	base := Schedule{Cron: "0 9 * * *", LastRun: mkTime(t, "2026-08-24 09:00").UTC().Format(time.RFC3339)}
	cases := []struct {
		name     string
		grace    string
		now      string
		wantFire bool
		wantLate time.Duration
	}{
		{"inside the default grace", "", "2026-08-25 09:45", true, 45 * time.Minute},
		{"on the default grace", "", "2026-08-25 10:00", true, time.Hour},
		{"past the default grace", "", "2026-08-25 10:01", false, 0},
		{"a widened grace still catches it", "24h", "2026-08-25 18:00", true, 9 * time.Hour},
		{"no window missed at all", "", "2026-08-24 23:00", false, 0},
	}
	for _, tc := range cases {
		s := base
		s.Grace = tc.grace
		fire, late := CatchUp(s, mkTime(t, tc.now))
		if fire != tc.wantFire {
			t.Errorf("CatchUp(%s) fire = %v, want %v", tc.name, fire, tc.wantFire)
		}
		if fire && late != tc.wantLate {
			t.Errorf("CatchUp(%s) late = %s, want %s", tc.name, late, tc.wantLate)
		}
	}
}

func TestCatchUpTakesTheMostRecentMissedWindow(t *testing.T) {
	// Le daemon a rate quatre fenetres. C'est la derniere qui compte : la plus
	// ancienne est trop en retard pour servir, et rejouer les quatre facturerait
	// quatre tours sur un monde qui n'existe plus.
	s := Schedule{
		Every:   "30m",
		Grace:   "45m",
		LastRun: mkTime(t, "2026-08-25 07:00").UTC().Format(time.RFC3339),
	}
	fire, late := CatchUp(s, mkTime(t, "2026-08-25 09:10"))
	if !fire {
		t.Fatal("no catch-up fire, want one")
	}
	if want := 10 * time.Minute; late != want {
		t.Errorf("late = %s, want %s (the 09:00 window, not the 07:30 one)", late, want)
	}
}

func TestCatchUpSkipsAPausedSchedule(t *testing.T) {
	s := Schedule{
		Cron: "0 9 * * *", Paused: true,
		LastRun: mkTime(t, "2026-08-24 09:00").UTC().Format(time.RFC3339),
	}
	if fire, _ := CatchUp(s, mkTime(t, "2026-08-25 09:45")); fire {
		t.Error("a paused schedule caught up")
	}
}

// La grace ne sert a rien si Due tire quand meme la fenetre qu'elle vient de
// refuser : c'est le trou que Stale bouche, et c'est ce que ce test tient.
func TestStaleMarksTheWindowsTheGraceRefuses(t *testing.T) {
	base := Schedule{Cron: "0 9 * * *", LastRun: mkTime(t, "2026-08-24 09:00").UTC().Format(time.RFC3339)}
	cases := []struct {
		name  string
		grace string
		now   string
		want  bool
	}{
		{"before the window", "", "2026-08-25 08:00", false},
		{"inside the grace", "", "2026-08-25 09:45", false},
		{"past the grace", "", "2026-08-25 10:01", true},
		{"long past the grace", "", "2026-08-25 23:00", true},
		{"a widened grace still covers it", "24h", "2026-08-25 18:00", false},
	}
	for _, tc := range cases {
		s := base
		s.Grace = tc.grace
		if got := Stale(s, mkTime(t, tc.now)); got != tc.want {
			t.Errorf("Stale(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
	// Une pause n'est pas une peremption : l'horaire reprendra ou l'operateur
	// decide, et rien ne doit bouger son ancre entre-temps.
	paused := base
	paused.Paused = true
	if Stale(paused, mkTime(t, "2026-08-25 23:00")) {
		t.Error("a paused schedule went stale")
	}
}

// La question de `schedule list` : quand cet horaire se reveille-t-il. Elle se
// compte depuis l'ancre, sinon un --every de 24h ancre a 9h repond "dans
// vingt-quatre heures" quel que soit le moment ou on la pose.
func TestNextWindowCountsFromTheAnchorAndNotFromNow(t *testing.T) {
	cases := []struct {
		name string
		s    Schedule
		want string
	}{
		{
			"every counts from the last run",
			Schedule{Every: "24h", LastRun: "2026-08-25T09:00:00Z", CreatedAt: "2026-08-01T09:00:00Z"},
			"2026-08-26 09:00",
		},
		{
			"every falls back on the creation",
			Schedule{Every: "6h", CreatedAt: "2026-08-25T09:00:00Z"},
			"2026-08-25 15:00",
		},
	}
	for _, tc := range cases {
		got, err := NextWindow(tc.s)
		if err != nil {
			t.Errorf("%s: NextWindow = %v", tc.name, err)
			continue
		}
		if want := mkUTC(t, tc.want); !got.Equal(want) {
			t.Errorf("%s: NextWindow = %s, want %s", tc.name, got.UTC(), want)
		}
	}
	// Le cron se lit dans l'heure locale du daemon, donc son cas se pose dans le
	// meme fuseau que l'expression.
	cron := Schedule{Cron: "0 9 * * *", LastRun: mkTime(t, "2026-08-25 09:00").Format(time.RFC3339)}
	got, err := NextWindow(cron)
	if err != nil {
		t.Fatalf("NextWindow(cron) = %v", err)
	}
	if want := mkTime(t, "2026-08-26 09:00"); !got.Equal(want) {
		t.Errorf("NextWindow(cron) = %s, want %s", got, want)
	}

	if _, err := NextWindow(Schedule{Every: "1h"}); err == nil {
		t.Error("NextWindow on a schedule with no anchor at all accepted, want an error")
	}
}

// Le tampon retient la fenetre servie et non l'instant de la livraison. Sans ca
// la cadence glisse d'un tick a chaque tir : la fenetre suivante se compte
// depuis une livraison qui arrive apres elle, donc elle tombe un tick plus loin,
// et le retard s'ajoute a lui-meme.
func TestWindowNamesTheOneBeingServedAndNotTheDelivery(t *testing.T) {
	s := Schedule{Every: "30m", LastRun: "2026-08-25T09:00:00Z", CreatedAt: "2026-08-25T09:00:00Z"}
	// La boucle decouvre la fenetre de 9h30 a 9h30m40s, et le tir lui-meme
	// prendra encore un peu de temps.
	got, ok := Window(s, mkUTC(t, "2026-08-25 09:30").Add(40*time.Second))
	if !ok {
		t.Fatal("Window found nothing on a schedule whose window has passed")
	}
	if want := mkUTC(t, "2026-08-25 09:30"); !got.Equal(want) {
		t.Fatalf("Window = %s, want %s", got.UTC(), want)
	}

	// Vingt-quatre heures de tirs consecutifs, chacun decouvert en retard et
	// livre plus tard encore : la cadence ne doit pas avoir bouge d'une seconde.
	start := mkUTC(t, "2026-08-25 09:00")
	cur := s
	fires := 0
	for tick := start; tick.Before(start.Add(24 * time.Hour)); tick = tick.Add(time.Minute) {
		if !Due(cur, tick) {
			continue
		}
		w, ok := Window(cur, tick)
		if !ok {
			t.Fatalf("Window found nothing at %s", tick)
		}
		cur.LastRun = w.UTC().Format(time.RFC3339)
		fires++
	}
	// La premiere fenetre tombe une demi-heure apres l'ancre et la derniere une
	// minute avant la fin du parcours, d'ou quarante-sept et non quarante-huit.
	if fires != 47 {
		t.Errorf("fired %d times in 24h, want 47", fires)
	}
	if want := start.Add(24 * time.Hour).Add(-30 * time.Minute).UTC().Format(time.RFC3339); cur.LastRun != want {
		t.Errorf("LastRun after a day = %s, want %s (the cadence drifted)", cur.LastRun, want)
	}
}

func TestSessionNameIsStableAcrossTicks(t *testing.T) {
	s := Schedule{Name: "morning-digest", Agent: "scout"}
	if got, want := SessionName(s), "schedule-morning-digest"; got != want {
		t.Errorf("SessionName = %q, want %q", got, want)
	}
}
