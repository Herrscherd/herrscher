package bridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/skills"
)

type nativeBackend struct{}

func (nativeBackend) Respond(context.Context, contracts.Prompt, func(contracts.BackendEvent)) (string, error) {
	return "", nil
}
func (nativeBackend) Close() error       { return nil }
func (nativeBackend) NativeSkills() bool { return true }

type plainBackend struct{}

func (plainBackend) Respond(context.Context, contracts.Prompt, func(contracts.BackendEvent)) (string, error) {
	return "", nil
}
func (plainBackend) Close() error { return nil }

// captureBackend records the Context of each prompt it answers and returns a
// fixed reply, so a test can assert what the hub injected.
type captureBackend struct {
	reply    string
	contexts []string
}

func (b *captureBackend) Respond(_ context.Context, p contracts.Prompt, _ func(contracts.BackendEvent)) (string, error) {
	b.contexts = append(b.contexts, p.Context)
	return b.reply, nil
}
func (b *captureBackend) Close() error { return nil }

func writeBridgeSkill(t *testing.T, workspace, name, front, body string) {
	t.Helper()
	dir := filepath.Join(workspace, ".claude", "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\n"+front+"---\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSkillRootsOrder(t *testing.T) {
	roots := skillRoots("/work/repo", []string{"/opt/x"})
	if roots[0] != filepath.Join("/work/repo", ".claude", "skills") {
		t.Fatalf("workspace root must come first, got %v", roots)
	}
	if roots[len(roots)-1] != "/opt/x" {
		t.Fatalf("extra roots must come last, got %v", roots)
	}
}

func TestNewSkillEngineNilForNativeBackend(t *testing.T) {
	if eng := newSkillEngine(nativeBackend{}); eng != nil {
		t.Fatalf("native backend must get no engine")
	}
	if eng := newSkillEngine(plainBackend{}); eng == nil {
		t.Fatalf("non-native backend must get an engine")
	}
}

func TestHubInjectsSkillMenuAndExpandsOnMarker(t *testing.T) {
	root := t.TempDir()
	writeBridgeSkill(t, root, "demo", "name: demo\ndescription: a demo\n", "DEMO BODY\n")
	eng := skills.NewEngine([]string{filepath.Join(root, ".claude", "skills")})

	resp := &captureBackend{reply: "ok <use-skill>demo</use-skill>"}

	sink := &recordSink{}
	runOneTurn(context.Background(), sink, resp, nil, contracts.Event{T: "input", Text: "hi"}, nil, eng)
	last := sink.events[len(sink.events)-1]
	if last.T != "reply" || last.Text != "ok" {
		t.Fatalf("marker must be stripped from the delivered reply, got %+v", last)
	}
	if !strings.Contains(resp.contexts[0], "demo: a demo") {
		t.Fatalf("turn 1 context missing menu:\n%s", resp.contexts[0])
	}
	if strings.Contains(resp.contexts[0], "DEMO BODY") {
		t.Fatalf("turn 1 must not carry body yet:\n%s", resp.contexts[0])
	}

	runOneTurn(context.Background(), &recordSink{}, resp, nil, contracts.Event{T: "input", Text: "again"}, nil, eng)
	if !strings.Contains(resp.contexts[1], "DEMO BODY") {
		t.Fatalf("turn 2 context missing expanded body:\n%s", resp.contexts[1])
	}
}

func TestHubSkipsInjectionWhenEngineNil(t *testing.T) {
	resp := &captureBackend{reply: "ok"}
	runOneTurn(context.Background(), &recordSink{}, resp, nil, contracts.Event{T: "input", Text: "hi"}, nil, nil)
	if resp.contexts[0] != "" {
		t.Fatalf("nil engine must leave context empty, got %q", resp.contexts[0])
	}
}
