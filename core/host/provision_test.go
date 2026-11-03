package host

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

type scriptedRunner struct {
	replies []string
	seen    [][]string
	copies  [][2]string
}

func (s *scriptedRunner) Command(ctx context.Context, _ string, argv ...string) *exec.Cmd {
	s.seen = append(s.seen, argv)
	body := "true"
	if len(s.seen) <= len(s.replies) {
		body = s.replies[len(s.seen)-1]
	}
	return exec.CommandContext(ctx, "sh", "-c", body)
}

func (s *scriptedRunner) Copy(ctx context.Context, src, dst string) *exec.Cmd {
	s.copies = append(s.copies, [2]string{src, dst})
	return exec.CommandContext(ctx, "true")
}

func (s *scriptedRunner) Describe() string { return "scripted" }

func TestPlatformFromUname(t *testing.T) {
	cases := []struct{ in, goos, goarch string }{
		{"Linux x86_64\n", "linux", "amd64"},
		{"Linux aarch64", "linux", "arm64"},
		{"Darwin arm64", "darwin", "arm64"},
	}
	for _, c := range cases {
		goos, goarch, err := platformFromUname(c.in)
		if err != nil || goos != c.goos || goarch != c.goarch {
			t.Errorf("platformFromUname(%q) = (%q,%q,%v)", c.in, goos, goarch, err)
		}
	}
	for _, bad := range []string{"", "Linux", "Plan9 x86_64", "Linux vax"} {
		if _, _, err := platformFromUname(bad); err == nil {
			t.Errorf("platformFromUname(%q) accepted the unsupported", bad)
		}
	}
}

func TestProvisionRefusesWithoutASourceCheckout(t *testing.T) {
	_, err := provisionHost(context.Background(), &scriptedRunner{}, state.Host{Name: "build1", SSH: "me@b1"}, "")
	if err == nil {
		t.Fatal("want a refusal")
	}
	if !strings.Contains(err.Error(), "set source") {
		t.Fatalf("the refusal does not point at the fix: %v", err)
	}
}

func TestProvisionStopsOnAnUnsupportedPlatform(t *testing.T) {
	run := &scriptedRunner{replies: []string{"printf 'Plan9 vax'"}}
	_, err := provisionHost(context.Background(), run, state.Host{Name: "build1", SSH: "me@b1"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("want an unsupported-platform refusal, got %v", err)
	}
	if len(run.copies) != 0 {
		t.Fatalf("it copied something anyway: %v", run.copies)
	}
}

func TestCheckReportsTheFourPoints(t *testing.T) {
	run := &scriptedRunner{replies: []string{
		"true",
		"true",
		"true",
		"true",
	}}
	rep := checkHost(context.Background(), run, state.Host{Name: "build1", SSH: "me@b1", Workspace: "/srv/work", Bin: "/hs", Version: "abc1234"}, "abc1234")
	if !rep.Reachable || !rep.Workspace || !rep.Git || rep.Herrscher == "" {
		t.Fatalf("report = %+v", rep)
	}
	if rep.Drift {
		t.Fatalf("no drift expected: %+v", rep)
	}
}

func TestCheckReportsVersionDrift(t *testing.T) {
	run := &scriptedRunner{replies: []string{"true", "true", "true", "true"}}
	rep := checkHost(context.Background(), run, state.Host{Name: "build1", SSH: "me@b1", Workspace: "/srv/work", Bin: "/hs", Version: "old1234"}, "new5678")
	if !rep.Drift {
		t.Fatal("want drift")
	}
	if !strings.Contains(strings.Join(rep.Notes, " "), "host provision") {
		t.Fatalf("the note does not point at the fix: %v", rep.Notes)
	}
}

func TestCheckOnAnUnreachableHostSaysSoAndStops(t *testing.T) {
	run := &scriptedRunner{replies: []string{"exit 255"}}
	rep := checkHost(context.Background(), run, state.Host{Name: "build1", SSH: "me@b1", Workspace: "/srv/work", Bin: "/hs"}, "v")
	if rep.Reachable {
		t.Fatal("want unreachable")
	}
	if len(run.seen) != 1 {
		t.Fatalf("an unreachable host was asked %d questions, want 1", len(run.seen))
	}
}

func TestCheckSaysWhenNothingIsProvisioned(t *testing.T) {
	run := &scriptedRunner{replies: []string{"true", "true", "true"}}
	rep := checkHost(context.Background(), run, state.Host{Name: "build1", SSH: "me@b1", Workspace: "/srv/work"}, "v")
	if rep.Herrscher != "" {
		t.Fatalf("Herrscher = %q, want empty", rep.Herrscher)
	}
	if !strings.Contains(strings.Join(rep.Notes, " "), "host provision") {
		t.Fatalf("notes = %v", rep.Notes)
	}
}
