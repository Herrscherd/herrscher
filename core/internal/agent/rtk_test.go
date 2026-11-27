package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApprovalHandlerCarriesTheRTKFlagOnlyWhenOn(t *testing.T) {
	on, onIsString := approvalHandler("/far/bin/herrscher", true)[keyCommand].(string)
	off, offIsString := approvalHandler("/far/bin/herrscher", false)[keyCommand].(string)
	if !onIsString || !offIsString {
		t.Fatalf("the handler carries no command: on=%v off=%v", onIsString, offIsString)
	}
	if !strings.HasSuffix(on, " approve hook --rtk") {
		t.Fatalf("rtk on: command %q", on)
	}
	if strings.Contains(off, "--rtk") {
		t.Fatalf("rtk off: command %q carries the flag", off)
	}
}

func TestInjectApprovalHookWritesTheRTKFlagAndReplacesAnOlderHook(t *testing.T) {
	first, err := injectApprovalHook([]byte("{}"), "/bin/h", false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := injectApprovalHook(first, "/bin/h", true)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(second), "approve hook"); n != 1 {
		t.Fatalf("%d hooks after re-materialization, want 1: %s", n, second)
	}
	if !strings.Contains(string(second), "approve hook --rtk") {
		t.Fatalf("the flagged hook is missing: %s", second)
	}
	third, err := injectApprovalHook(second, "/bin/h", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(third), "--rtk") {
		t.Fatalf("switching rtk off left the flag: %s", third)
	}
}

func TestIsApprovalHookMatchesTheFlaggedCommand(t *testing.T) {
	entry := approvalGroup(approvalHandler("/bin/h", true))
	if !isApprovalHook(entry) {
		t.Fatalf("the flagged approval group is not recognised as ours: %v", entry)
	}
}

func TestReadRTKIsOffByDefaultAndOnWhenWritten(t *testing.T) {
	home := t.TempDir()
	if readRTK(home) {
		t.Fatal("rtk is on with no file")
	}
	for _, body := range []string{"off\n", "yes", ""} {
		if err := os.WriteFile(filepath.Join(home, rtkFile), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if readRTK(home) {
			t.Fatalf("rtk is on for %q", body)
		}
	}
	if err := os.WriteFile(filepath.Join(home, rtkFile), []byte(" ON\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !readRTK(home) {
		t.Fatal("rtk is off for on")
	}
}
