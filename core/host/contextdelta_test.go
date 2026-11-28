package host

import (
	"os"
	"path/filepath"
	"testing"
)

func writeAgentContextDelta(t *testing.T, root, name, setting string) {
	t.Helper()
	home := filepath.Join(root, name)
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if setting == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(home, "context-delta"), []byte(setting+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAgentContextDeltaOnOverridesAnOffDaemonDefault(t *testing.T) {
	root := t.TempDir()
	writeAgentContextDelta(t, root, "ada", "on")
	if !AgentContextDelta(root, "ada", false) {
		t.Fatal("an agent set to on must get the delta with the daemon default off")
	}
}

func TestAgentContextDeltaOffOverridesAnOnDaemonDefault(t *testing.T) {
	root := t.TempDir()
	writeAgentContextDelta(t, root, "bob", "off")
	if AgentContextDelta(root, "bob", true) {
		t.Fatal("an agent set to off must keep every block with the daemon default on")
	}
}

func TestAgentContextDeltaInheritsTheDaemonDefaultWhenAbsent(t *testing.T) {
	root := t.TempDir()
	writeAgentContextDelta(t, root, "cy", "")
	writeAgentContextDelta(t, root, "dee", "maybe")
	for _, name := range []string{"cy", "dee", "nobody"} {
		if !AgentContextDelta(root, name, true) {
			t.Fatalf("agent %q with no usable setting must inherit a daemon default of on", name)
		}
		if AgentContextDelta(root, name, false) {
			t.Fatalf("agent %q with no usable setting must inherit a daemon default of off", name)
		}
	}
}
