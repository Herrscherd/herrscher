package host

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/schedule"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

type fakeScheduleRunner struct {
	fired []string
	err   error
}

func (f *fakeScheduleRunner) fireNow(_ context.Context, name string) error {
	if f.err != nil {
		return f.err
	}
	f.fired = append(f.fired, name)
	return nil
}

func testScheduleRegistry(t *testing.T, st *state.State, slot *schedulerSlot) *cli.Registry {
	t.Helper()
	reg := &cli.Registry{}
	if slot == nil {
		slot = &schedulerSlot{}
	}
	if err := addScheduleCommands(reg, st, fakeAgents{known: map[string]bool{"scout": true}}, slot); err != nil {
		t.Fatalf("addScheduleCommands: %v", err)
	}
	return reg
}

func TestScheduleAddRefusesWhatCannotRun(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	reg := testScheduleRegistry(t, st, nil)
	cases := []struct {
		name string
		args []string
	}{
		{"no target", []string{"schedule", "add", "--name", "d", "--every", "30m", "--task", "t"}},
		{"two targets", []string{"schedule", "add", "--name", "d", "--agent", "scout", "--session", "s", "--every", "30m", "--task", "t"}},
		{"two cadences", []string{"schedule", "add", "--name", "d", "--agent", "scout", "--every", "30m", "--cron", "0 9 * * *", "--task", "t"}},
		{"unknown agent", []string{"schedule", "add", "--name", "d", "--agent", "ghost", "--every", "30m", "--task", "t"}},
		{"bad cron", []string{"schedule", "add", "--name", "d", "--agent", "scout", "--cron", "0 9 * *", "--task", "t"}},
		{"bad every", []string{"schedule", "add", "--name", "d", "--agent", "scout", "--every", "soon", "--task", "t"}},
		{"bad grace", []string{"schedule", "add", "--name", "d", "--agent", "scout", "--every", "30m", "--task", "t", "--grace", "later"}},
	}
	for _, tc := range cases {
		if _, err := reg.Dispatch(context.Background(), tc.args); err == nil {
			t.Errorf("schedule add (%s) accepted, want an error", tc.name)
		}
	}
	if got := st.SnapshotSchedules(); len(got) != 0 {
		t.Fatalf("SnapshotSchedules = %+v, want nothing written by a refused add", got)
	}
}

func TestScheduleAddStampsCreatedAtSoTheFirstWindowIsCounted(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	reg := testScheduleRegistry(t, st, nil)
	if _, err := reg.Dispatch(context.Background(), []string{"schedule", "add", "--name", "digest",
		"--agent", "scout", "--every", "30m", "--task", "t"}); err != nil {
		t.Fatalf("schedule add: %v", err)
	}
	got := st.SnapshotSchedules()
	if len(got) != 1 || got[0].CreatedAt == "" {
		t.Fatalf("SnapshotSchedules = %+v, want a row carrying its creation stamp", got)
	}
	if got[0].LastRun != "" {
		t.Errorf("LastRun = %q, want empty on a fresh schedule", got[0].LastRun)
	}
}

func TestScheduleAddThenListThenRemove(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	reg := testScheduleRegistry(t, st, nil)
	ctx := context.Background()
	if _, err := reg.Dispatch(ctx, []string{"schedule", "add", "--name", "digest",
		"--agent", "scout", "--cron", "0 9 * * 1-5", "--task", "read the PRs"}); err != nil {
		t.Fatalf("schedule add: %v", err)
	}
	// Un nom deja pris est refuse : deux horaires du meme nom se disputeraient la
	// meme session possedee.
	if _, err := reg.Dispatch(ctx, []string{"schedule", "add", "--name", "digest",
		"--agent", "scout", "--every", "30m", "--task", "other"}); err == nil {
		t.Error("a duplicate name was accepted, want an error")
	}
	out, err := reg.Dispatch(ctx, []string{"schedule", "list"})
	if err != nil {
		t.Fatalf("schedule list: %v", err)
	}
	for _, want := range []string{"digest", "agent:scout", "cron 0 9 * * 1-5", "live"} {
		if !strings.Contains(out, want) {
			t.Errorf("schedule list = %q, want it to carry %q", out, want)
		}
	}
	if _, err := reg.Dispatch(ctx, []string{"schedule", "pause", "--name", "digest"}); err != nil {
		t.Fatalf("schedule pause: %v", err)
	}
	if got := st.SnapshotSchedules(); len(got) != 1 || !got[0].Paused {
		t.Fatalf("SnapshotSchedules = %+v, want the row paused", got)
	}
	if _, err := reg.Dispatch(ctx, []string{"schedule", "resume", "--name", "digest"}); err != nil {
		t.Fatalf("schedule resume: %v", err)
	}
	if got := st.SnapshotSchedules(); len(got) != 1 || got[0].Paused {
		t.Fatalf("SnapshotSchedules = %+v, want the row live again", got)
	}
	if _, err := reg.Dispatch(ctx, []string{"schedule", "rm", "--name", "digest"}); err != nil {
		t.Fatalf("schedule rm: %v", err)
	}
	if got := st.SnapshotSchedules(); len(got) != 0 {
		t.Fatalf("SnapshotSchedules = %+v, want nothing left", got)
	}
	// Les mutateurs disent quand le nom n'existe pas, plutot que de reussir en
	// silence sur rien.
	for _, verb := range []string{"rm", "pause", "resume"} {
		if _, err := reg.Dispatch(ctx, []string{"schedule", verb, "--name", "digest"}); err == nil {
			t.Errorf("schedule %s on an unknown name accepted, want an error", verb)
		}
	}
}

func TestScheduleResumeRestartsTheCadenceFromNow(t *testing.T) {
	// Sans ca, un horaire mis en pause une semaine trouverait sa fenetre
	// largement passee et tirerait a la seconde ou il revient.
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	reg := testScheduleRegistry(t, st, nil)
	ctx := context.Background()
	if _, err := reg.Dispatch(ctx, []string{"schedule", "add", "--name", "digest",
		"--agent", "scout", "--every", "30m", "--task", "t"}); err != nil {
		t.Fatalf("schedule add: %v", err)
	}
	for _, verb := range []string{"pause", "resume"} {
		if _, err := reg.Dispatch(ctx, []string{"schedule", verb, "--name", "digest"}); err != nil {
			t.Fatalf("schedule %s: %v", verb, err)
		}
	}
	got := st.SnapshotSchedules()
	if len(got) != 1 || got[0].LastRun == "" {
		t.Fatalf("SnapshotSchedules = %+v, want the anchor moved to the resume", got)
	}
	if schedule.Due(got[0], time.Now()) {
		t.Error("the resumed schedule is already due, want its next window half an hour out")
	}
}

func TestScheduleAddSaysWhenTheNamedSessionIsNotThere(t *testing.T) {
	// La session peut etre ouverte plus tard, donc c'est accepte. Mais une faute
	// de frappe acheterait sinon un horaire qui saute chaque fenetre en silence.
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	reg := testScheduleRegistry(t, st, nil)
	out, err := reg.Dispatch(context.Background(), []string{"schedule", "add", "--name", "digest",
		"--session", "typo", "--every", "30m", "--task", "t"})
	if err != nil {
		t.Fatalf("schedule add: %v", err)
	}
	if !strings.Contains(out, "typo") || !strings.Contains(out, "no session named") {
		t.Fatalf("schedule add = %q, want it to name the session it cannot see", out)
	}
	if got := st.SnapshotSchedules(); len(got) != 1 {
		t.Fatalf("SnapshotSchedules = %+v, want the schedule written anyway", got)
	}
}

func TestScheduleListSaysWhenThereIsNothing(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	reg := testScheduleRegistry(t, st, nil)
	out, err := reg.Dispatch(context.Background(), []string{"schedule", "list"})
	if err != nil {
		t.Fatalf("schedule list: %v", err)
	}
	if !strings.Contains(out, "no schedules") {
		t.Fatalf("schedule list = %q, want it to say there are none", out)
	}
}

func TestScheduleRunNeedsALiveDaemon(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	reg := testScheduleRegistry(t, st, nil)
	// Un CLI operateur n'a pas de boucle : le verbe doit le dire plutot que de
	// paniquer sur un slot vide.
	_, err := reg.Dispatch(context.Background(), []string{"schedule", "run", "--name", "digest"})
	if err == nil || !strings.Contains(err.Error(), "serve") {
		t.Fatalf("schedule run without a daemon = %v, want an error pointing at `herrscher serve`", err)
	}
}

// The verbs only reach an operator if buildRegistry registers them, and a
// registration wired one place and tested another is a registration that drifts.
// So dispatch through the real registry, once, for the whole namespace.
func TestScheduleVerbsAreInTheRealRegistry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	st := state.NewState(filepath.Join(dir, "s.json"))
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher")
	reg, _, err := buildRegistry(ctx, Deps{}, Options{StatePath: filepath.Join(dir, "s.json"), DefaultCmd: "claude"}, st, sup, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Dispatch(ctx, []string{"schedule", "add", "--name", "digest",
		"--session", "live", "--every", "30m", "--task", "read the PRs"}); err != nil {
		t.Fatalf("schedule add: %v", err)
	}
	out, err := reg.Dispatch(ctx, []string{"schedule", "list"})
	if err != nil {
		t.Fatalf("schedule list: %v", err)
	}
	if !strings.Contains(out, "digest") {
		t.Fatalf("schedule list = %q, want the row just added", out)
	}
	for _, verb := range []string{"pause", "resume", "rm"} {
		if _, err := reg.Dispatch(ctx, []string{"schedule", verb, "--name", "digest"}); err != nil {
			t.Fatalf("schedule %s: %v", verb, err)
		}
	}
	// The operator CLI shares this registry and has no loop, so `schedule run`
	// must be registered and refuse rather than be missing or panic.
	if _, err := reg.Dispatch(ctx, []string{"schedule", "run", "--name", "digest"}); err == nil {
		t.Error("schedule run without a daemon accepted, want an error")
	}
}

func TestScheduleRunReachesTheLoop(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	runner := &fakeScheduleRunner{}
	reg := testScheduleRegistry(t, st, &schedulerSlot{sched: runner})
	if _, err := reg.Dispatch(context.Background(), []string{"schedule", "run", "--name", "digest"}); err != nil {
		t.Fatalf("schedule run: %v", err)
	}
	if len(runner.fired) != 1 || runner.fired[0] != "digest" {
		t.Fatalf("fired = %v, want digest", runner.fired)
	}
	runner.err = errors.New("no schedule named \"ghost\"")
	if _, err := reg.Dispatch(context.Background(), []string{"schedule", "run", "--name", "ghost"}); err == nil {
		t.Error("schedule run on an unknown name accepted, want the loop's error surfaced")
	}
}
