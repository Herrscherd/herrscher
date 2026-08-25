package host

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/schedule"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type fakeScheduleStore struct {
	rows      []schedule.Schedule
	stamped   []string
	stampedAt []string
}

func (f *fakeScheduleStore) SnapshotSchedules() []schedule.Schedule {
	return append([]schedule.Schedule(nil), f.rows...)
}

func (f *fakeScheduleStore) StampScheduleRun(name, at string) error {
	f.stamped = append(f.stamped, name)
	f.stampedAt = append(f.stampedAt, at)
	return nil
}

type fakeScheduleSessions struct{ rows []state.Session }

func (f *fakeScheduleSessions) SnapshotSessions() []state.Session {
	return append([]state.Session(nil), f.rows...)
}

type fakeScheduleCreator struct {
	created  []string
	sessions *fakeScheduleSessions
	err      error
}

func (f *fakeScheduleCreator) Create(_ context.Context, spec contracts.CreateSession) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.created = append(f.created, spec.Name)
	if f.sessions != nil {
		f.sessions.rows = append(f.sessions.rows, state.Session{Name: spec.Name})
	}
	return spec.Name, nil
}

func mkHostTime(t *testing.T, s string) time.Time {
	t.Helper()
	got, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	if err != nil {
		t.Fatalf("bad fixture %q: %v", s, err)
	}
	return got
}

func mustStamp(t *testing.T, s string) string {
	t.Helper()
	return mkHostTime(t, s).UTC().Format(time.RFC3339)
}

// newTestScheduler builds a scheduler over fakes, on a frozen clock.
func newTestScheduler(store *fakeScheduleStore, sessions *fakeScheduleSessions,
	creator *fakeScheduleCreator, seeded *[]string, now time.Time) *scheduler {
	return newScheduler(store, sessions, creator, func(sess, task string) bool {
		*seeded = append(*seeded, sess+"|"+task)
		return true
	}, func() time.Time { return now })
}

func TestTickSeedsTheNamedSession(t *testing.T) {
	store := &fakeScheduleStore{rows: []schedule.Schedule{{
		Name: "digest", Session: "live", Every: "30m", Task: "read the PRs",
		LastRun: mustStamp(t, "2026-08-25 09:00"),
	}}}
	sessions := &fakeScheduleSessions{rows: []state.Session{{Name: "live"}}}
	var seeded []string
	sch := newTestScheduler(store, sessions, &fakeScheduleCreator{}, &seeded, mkHostTime(t, "2026-08-25 09:30"))
	sch.tick(context.Background())
	sch.wait()
	if len(seeded) != 1 || seeded[0] != "live|read the PRs" {
		t.Fatalf("seeded = %v, want one seed into live", seeded)
	}
	if len(store.stamped) != 1 {
		t.Fatalf("stamped = %v, want LastRun advanced once", store.stamped)
	}
}

func TestTickLeavesAScheduleAloneBeforeItsWindow(t *testing.T) {
	store := &fakeScheduleStore{rows: []schedule.Schedule{{
		Name: "digest", Session: "live", Every: "30m", Task: "t",
		LastRun: mustStamp(t, "2026-08-25 09:00"),
	}}}
	sessions := &fakeScheduleSessions{rows: []state.Session{{Name: "live"}}}
	var seeded []string
	sch := newTestScheduler(store, sessions, &fakeScheduleCreator{}, &seeded, mkHostTime(t, "2026-08-25 09:10"))
	sch.tick(context.Background())
	sch.wait()
	if len(seeded) != 0 {
		t.Fatalf("seeded = %v, want nothing before the window", seeded)
	}
}

func TestTickDoesNotStackASecondTurnOnTheSameSchedule(t *testing.T) {
	store := &fakeScheduleStore{rows: []schedule.Schedule{{
		Name: "digest", Session: "live", Every: "1m", Task: "t",
		LastRun: mustStamp(t, "2026-08-25 09:00"),
	}}}
	sessions := &fakeScheduleSessions{rows: []state.Session{{Name: "live"}}}
	var seeded []string
	sch := newTestScheduler(store, sessions, &fakeScheduleCreator{}, &seeded, mkHostTime(t, "2026-08-25 09:30"))
	// Le premier tir est retenu en vol : le tick suivant doit passer son chemin
	// plutot que d'empiler un tour derriere le premier.
	if !sch.claim("digest") {
		t.Fatal("claim refused on a free schedule")
	}
	sch.tick(context.Background())
	sch.wait()
	if len(seeded) != 0 {
		t.Fatalf("seeded = %v, want nothing while the previous turn is in flight", seeded)
	}
	// Une fois le jeton rendu, la fenetre suivante repart normalement.
	sch.release("digest")
	sch.tick(context.Background())
	sch.wait()
	if len(seeded) != 1 {
		t.Fatalf("seeded = %v, want the next tick to fire once the token is free", seeded)
	}
}

func TestAnAbsentSessionTargetCreatesNothing(t *testing.T) {
	store := &fakeScheduleStore{rows: []schedule.Schedule{{
		Name: "digest", Session: "gone", Every: "30m", Task: "t",
		LastRun: mustStamp(t, "2026-08-25 09:00"),
	}}}
	creator := &fakeScheduleCreator{}
	var seeded []string
	sch := newTestScheduler(store, &fakeScheduleSessions{}, creator, &seeded, mkHostTime(t, "2026-08-25 09:30"))
	sch.tick(context.Background())
	sch.wait()
	if len(creator.created) != 0 {
		t.Errorf("created = %v, want nothing: the operator named a session", creator.created)
	}
	if len(seeded) != 0 {
		t.Errorf("seeded = %v, want nothing", seeded)
	}
	// La fenetre n'a rien livre, donc elle doit etre retentee, pas enregistree.
	if len(store.stamped) != 0 {
		t.Errorf("stamped = %v, want LastRun left alone", store.stamped)
	}
}

func TestAnArchivedSessionCountsAsAbsent(t *testing.T) {
	store := &fakeScheduleStore{rows: []schedule.Schedule{{
		Name: "digest", Session: "live", Every: "30m", Task: "t",
		LastRun: mustStamp(t, "2026-08-25 09:00"),
	}}}
	sessions := &fakeScheduleSessions{rows: []state.Session{{Name: "live", Archived: true}}}
	var seeded []string
	sch := newTestScheduler(store, sessions, &fakeScheduleCreator{}, &seeded, mkHostTime(t, "2026-08-25 09:30"))
	sch.tick(context.Background())
	sch.wait()
	if len(seeded) != 0 {
		t.Fatalf("seeded = %v, want nothing into an archived session", seeded)
	}
}

func TestAnAgentTargetCreatesItsSessionOnceThenReusesIt(t *testing.T) {
	store := &fakeScheduleStore{rows: []schedule.Schedule{{
		Name: "digest", Agent: "scout", Every: "30m", Task: "t",
		LastRun: mustStamp(t, "2026-08-25 09:00"),
	}}}
	sessions := &fakeScheduleSessions{}
	creator := &fakeScheduleCreator{sessions: sessions}
	var seeded []string
	sch := newTestScheduler(store, sessions, creator, &seeded, mkHostTime(t, "2026-08-25 09:30"))

	sch.tick(context.Background())
	sch.wait()
	sch.tick(context.Background())
	sch.wait()

	if len(creator.created) != 1 || creator.created[0] != "schedule-digest" {
		t.Fatalf("created = %v, want exactly one schedule-digest", creator.created)
	}
	if len(seeded) != 2 {
		t.Fatalf("seeded = %v, want both ticks delivered", seeded)
	}
}

func TestASessionThatWillNotOpenLeavesTheWindowUnstamped(t *testing.T) {
	store := &fakeScheduleStore{rows: []schedule.Schedule{{
		Name: "digest", Agent: "scout", Every: "30m", Task: "t",
		LastRun: mustStamp(t, "2026-08-25 09:00"),
	}}}
	creator := &fakeScheduleCreator{err: errors.New("no worktree today")}
	var seeded []string
	sch := newTestScheduler(store, &fakeScheduleSessions{}, creator, &seeded, mkHostTime(t, "2026-08-25 09:30"))
	sch.tick(context.Background())
	sch.wait()
	if len(seeded) != 0 || len(store.stamped) != 0 {
		t.Fatalf("seeded = %v, stamped = %v, want the window retried, not recorded", seeded, store.stamped)
	}
}

func TestAPausedScheduleNeverFires(t *testing.T) {
	store := &fakeScheduleStore{rows: []schedule.Schedule{{
		Name: "digest", Session: "live", Every: "30m", Task: "t", Paused: true,
		LastRun: mustStamp(t, "2026-08-25 09:00"),
	}}}
	sessions := &fakeScheduleSessions{rows: []state.Session{{Name: "live"}}}
	var seeded []string
	sch := newTestScheduler(store, sessions, &fakeScheduleCreator{}, &seeded, mkHostTime(t, "2026-08-25 23:00"))
	sch.tick(context.Background())
	sch.catchUp(context.Background())
	sch.wait()
	if len(seeded) != 0 {
		t.Fatalf("seeded = %v, want nothing from a paused schedule", seeded)
	}
}

func TestCatchUpAnnouncesThatTheTurnIsLate(t *testing.T) {
	store := &fakeScheduleStore{rows: []schedule.Schedule{{
		Name: "digest", Session: "live", Cron: "0 9 * * *", Task: "read the PRs",
		LastRun: mustStamp(t, "2026-08-24 09:00"),
	}}}
	sessions := &fakeScheduleSessions{rows: []state.Session{{Name: "live"}}}
	var seeded []string
	sch := newTestScheduler(store, sessions, &fakeScheduleCreator{}, &seeded, mkHostTime(t, "2026-08-25 09:45"))
	sch.catchUp(context.Background())
	sch.wait()
	if len(seeded) != 1 {
		t.Fatalf("seeded = %v, want one late fire", seeded)
	}
	if !strings.Contains(seeded[0], "45m") || !strings.HasSuffix(seeded[0], "read the PRs") {
		t.Fatalf("seeded = %q, want the task behind a lateness note", seeded[0])
	}
}

func TestCatchUpSaysNothingWhenTheTurnIsOnTime(t *testing.T) {
	store := &fakeScheduleStore{rows: []schedule.Schedule{{
		Name: "digest", Session: "live", Cron: "0 9 * * *", Task: "read the PRs",
		LastRun: mustStamp(t, "2026-08-24 09:00"),
	}}}
	sessions := &fakeScheduleSessions{rows: []state.Session{{Name: "live"}}}
	var seeded []string
	sch := newTestScheduler(store, sessions, &fakeScheduleCreator{}, &seeded, mkHostTime(t, "2026-08-25 09:00"))
	sch.catchUp(context.Background())
	sch.wait()
	if len(seeded) != 1 || seeded[0] != "live|read the PRs" {
		t.Fatalf("seeded = %v, want the bare task with no lateness note", seeded)
	}
}

func TestFireNowIgnoresTheCadenceAndLeavesLastRunAlone(t *testing.T) {
	store := &fakeScheduleStore{rows: []schedule.Schedule{{
		Name: "digest", Session: "live", Cron: "0 9 * * *", Task: "t",
		LastRun: mustStamp(t, "2026-08-25 09:00"),
	}}}
	sessions := &fakeScheduleSessions{rows: []state.Session{{Name: "live"}}}
	var seeded []string
	sch := newTestScheduler(store, sessions, &fakeScheduleCreator{}, &seeded, mkHostTime(t, "2026-08-25 09:05"))
	if err := sch.fireNow(context.Background(), "digest"); err != nil {
		t.Fatalf("fireNow: %v", err)
	}
	sch.wait()
	if len(seeded) != 1 {
		t.Fatalf("seeded = %v, want one out-of-band fire", seeded)
	}
	if len(store.stamped) != 0 {
		t.Fatalf("stamped = %v, want an out-of-band fire not to move the cadence", store.stamped)
	}
	if err := sch.fireNow(context.Background(), "nope"); err == nil {
		t.Error("fireNow(unknown) accepted, want an error")
	}
}

func TestFireNowRunsAPausedSchedule(t *testing.T) {
	// Une pause arrete la cadence, pas la main de l'operateur : c'est ainsi qu'on
	// verifie une tache avant de la relancer.
	store := &fakeScheduleStore{rows: []schedule.Schedule{{
		Name: "digest", Session: "live", Every: "30m", Task: "t", Paused: true,
		LastRun: mustStamp(t, "2026-08-25 09:00"),
	}}}
	sessions := &fakeScheduleSessions{rows: []state.Session{{Name: "live"}}}
	var seeded []string
	sch := newTestScheduler(store, sessions, &fakeScheduleCreator{}, &seeded, mkHostTime(t, "2026-08-25 09:05"))
	if err := sch.fireNow(context.Background(), "digest"); err != nil {
		t.Fatalf("fireNow: %v", err)
	}
	sch.wait()
	if len(seeded) != 1 {
		t.Fatalf("seeded = %v, want the paused schedule fired by hand", seeded)
	}
}

// Le trou que ce test tient : le rattrapage refuse une fenetre trop vieille,
// mais elle reste due, et sans realignement le tick suivant la tirerait quand
// meme, sans dire son retard.
func TestTickForgetsAWindowOlderThanItsGrace(t *testing.T) {
	store := &fakeScheduleStore{rows: []schedule.Schedule{{
		Name: "digest", Session: "live", Cron: "0 9 * * *", Task: "t",
		LastRun: mustStamp(t, "2026-08-24 09:00"),
	}}}
	sessions := &fakeScheduleSessions{rows: []state.Session{{Name: "live"}}}
	var seeded []string
	sch := newTestScheduler(store, sessions, &fakeScheduleCreator{}, &seeded, mkHostTime(t, "2026-08-25 18:00"))
	sch.tick(context.Background())
	sch.wait()
	if len(seeded) != 0 {
		t.Fatalf("seeded = %v, want a nine-o'clock window not served at six in the evening", seeded)
	}
	if len(store.stamped) != 1 {
		t.Fatalf("stamped = %v, want the cadence restarted from now", store.stamped)
	}
}

func TestTickLeavesAScheduleInFlightAlone(t *testing.T) {
	// Sa fenetre est servie a l'instant meme : la declarer perimee deplacerait
	// l'ancre sous le tir en cours.
	store := &fakeScheduleStore{rows: []schedule.Schedule{{
		Name: "digest", Session: "live", Cron: "0 9 * * *", Task: "t",
		LastRun: mustStamp(t, "2026-08-24 09:00"),
	}}}
	sessions := &fakeScheduleSessions{rows: []state.Session{{Name: "live"}}}
	var seeded []string
	sch := newTestScheduler(store, sessions, &fakeScheduleCreator{}, &seeded, mkHostTime(t, "2026-08-25 18:00"))
	if !sch.claim("digest") {
		t.Fatal("claim refused on a free schedule")
	}
	sch.tick(context.Background())
	sch.wait()
	if len(store.stamped) != 0 {
		t.Fatalf("stamped = %v, want nothing touched while a turn is in flight", store.stamped)
	}
}

func TestFireNowSaysWhenNoSessionTookTheTask(t *testing.T) {
	// L'operateur attend la reponse : « fired » sur une session jamais atteinte
	// vaut moins que pas de verbe du tout.
	store := &fakeScheduleStore{rows: []schedule.Schedule{{
		Name: "digest", Session: "gone", Every: "30m", Task: "t",
		LastRun: mustStamp(t, "2026-08-25 09:00"),
	}}}
	var seeded []string
	sch := newTestScheduler(store, &fakeScheduleSessions{}, &fakeScheduleCreator{}, &seeded, mkHostTime(t, "2026-08-25 09:05"))
	if err := sch.fireNow(context.Background(), "digest"); err == nil {
		t.Fatal("fireNow over an absent session reported success, want an error")
	}
}

func TestRunWaitsForTheFirstTickBeforeCatchingUp(t *testing.T) {
	// Les sessions restaurees au boot enregistrent leur driver depuis une
	// goroutine a elles : un rattrapage tire dans la foulee de `serve` ne
	// trouverait personne, et la fenetre ratee serait perdue pour de bon.
	store := &fakeScheduleStore{rows: []schedule.Schedule{{
		Name: "digest", Session: "live", Cron: "0 9 * * *", Task: "t",
		LastRun: mustStamp(t, "2026-08-24 09:00"),
	}}}
	sessions := &fakeScheduleSessions{rows: []state.Session{{Name: "live"}}}
	var seeded []string
	sch := newTestScheduler(store, sessions, &fakeScheduleCreator{}, &seeded, mkHostTime(t, "2026-08-25 09:45"))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { sch.Run(ctx); close(done) }()
	cancel()
	<-done
	sch.wait()
	if len(seeded) != 0 {
		t.Fatalf("seeded = %v, want nothing before the first tick", seeded)
	}
}

// Le tampon retient la fenetre servie, pas l'instant ou la boucle l'a
// decouverte. Sinon la fenetre suivante se compte depuis un point posterieur a
// elle, donc elle tombe un tick plus loin, et le decalage s'ajoute a lui-meme :
// un horaire aux trente minutes finit par tirer quarante-six fois par jour.
func TestTickStampsTheWindowAndNotTheMomentItWasNoticed(t *testing.T) {
	store := &fakeScheduleStore{rows: []schedule.Schedule{{
		Name: "digest", Session: "live", Every: "30m", Task: "t",
		LastRun: mustStamp(t, "2026-08-25 09:00"),
	}}}
	sessions := &fakeScheduleSessions{rows: []state.Session{{Name: "live"}}}
	var seeded []string
	// La boucle passe quarante secondes apres la fenetre de 9h30.
	noticed := mkHostTime(t, "2026-08-25 09:30").Add(40 * time.Second)
	sch := newTestScheduler(store, sessions, &fakeScheduleCreator{}, &seeded, noticed)
	sch.tick(context.Background())
	sch.wait()
	if len(store.stampedAt) != 1 {
		t.Fatalf("stampedAt = %v, want one stamp", store.stampedAt)
	}
	if want := mustStamp(t, "2026-08-25 09:30"); store.stampedAt[0] != want {
		t.Fatalf("stampedAt = %q, want %q (the cadence drifted by the delay)", store.stampedAt[0], want)
	}
}

// Une fenetre ratee est rattrapee sur sa propre heure, pas sur celle du
// demarrage : sans ca un daemon relance a 9h12 recalerait sur 9h12 un horaire
// qui doit rester cale sur 9h00.
func TestCatchUpStampsTheMissedWindowAndNotTheBoot(t *testing.T) {
	store := &fakeScheduleStore{rows: []schedule.Schedule{{
		Name: "digest", Session: "live", Every: "24h", Task: "t",
		LastRun: mustStamp(t, "2026-08-24 09:00"),
	}}}
	sessions := &fakeScheduleSessions{rows: []state.Session{{Name: "live"}}}
	var seeded []string
	sch := newTestScheduler(store, sessions, &fakeScheduleCreator{}, &seeded, mkHostTime(t, "2026-08-25 09:12"))
	sch.catchUp(context.Background())
	sch.wait()
	if len(store.stampedAt) != 1 {
		t.Fatalf("stampedAt = %v, want one stamp", store.stampedAt)
	}
	if want := mustStamp(t, "2026-08-25 09:00"); store.stampedAt[0] != want {
		t.Fatalf("stampedAt = %q, want %q", store.stampedAt[0], want)
	}
}

// state.json s'edite a la main. Une cadence qui ne se lit plus ne tirera jamais,
// et la boucle doit passer son chemin sans rien tirer ni rien casser.
func TestCatchUpSkipsAScheduleWhoseCadenceNoLongerReads(t *testing.T) {
	store := &fakeScheduleStore{rows: []schedule.Schedule{{
		Name: "broken", Session: "live", Cron: "0 9 * *", Task: "t",
		LastRun: mustStamp(t, "2026-08-24 09:00"),
	}}}
	sessions := &fakeScheduleSessions{rows: []state.Session{{Name: "live"}}}
	var seeded []string
	sch := newTestScheduler(store, sessions, &fakeScheduleCreator{}, &seeded, mkHostTime(t, "2026-08-25 09:12"))
	sch.catchUp(context.Background())
	sch.wait()
	if len(seeded) != 0 || len(store.stamped) != 0 {
		t.Fatalf("seeded = %v, stamped = %v, want nothing on an unreadable cadence", seeded, store.stamped)
	}
}

func TestRunStopsWithItsContext(t *testing.T) {
	store := &fakeScheduleStore{}
	var seeded []string
	sch := newTestScheduler(store, &fakeScheduleSessions{}, &fakeScheduleCreator{}, &seeded, mkHostTime(t, "2026-08-25 09:00"))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { sch.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return when its context was cancelled")
	}
}
