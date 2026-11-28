package bridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/eventwire"
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

type captureBackend struct {
	reply     string
	contexts  []string
	texts     []string
	languages []i18n.Language
}

func (b *captureBackend) Respond(_ context.Context, p contracts.Prompt, _ func(contracts.BackendEvent)) (string, error) {
	b.contexts = append(b.contexts, p.Context)
	b.texts = append(b.texts, p.Content)
	b.languages = append(b.languages, p.Language)
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

func TestNewSkillEngineNilForNativeBackend(t *testing.T) {
	t.Setenv("HERRSCHER_STATE_DIR", t.TempDir())
	messages := newBridgeMessages()
	if eng, err := newSkillEngine(nativeBackend{}, skills.Budget{}, messages); eng != nil || err != nil {
		t.Fatalf("native backend must get no engine and no error, got %v, %v", eng, err)
	}
	if eng, err := newSkillEngine(plainBackend{}, skills.Budget{}, messages); eng == nil || err != nil {
		t.Fatalf("non-native backend must get an engine, got %v, %v", eng, err)
	}
}

func TestHubInjectsSkillMenuAndExpandsOnMarker(t *testing.T) {
	root := t.TempDir()
	writeBridgeSkill(t, root, "demo", "name: demo\ndescription: a demo\n", "DEMO BODY\n")
	eng := skills.NewEngine([]string{filepath.Join(root, ".claude", "skills")})

	resp := &captureBackend{reply: "ok <use-skill>demo</use-skill>"}

	sink := &recordSink{}
	if err := runOneTurn(context.Background(), sink, resp, nil, contracts.Event{T: eventwire.EventInput, Text: "hi"}, nil, eng, affordances{}, nil, 0); err != nil {
		t.Fatalf("turn failed: %v", err)
	}
	last := sink.events[len(sink.events)-1]
	if last.T != eventwire.EventReply || last.Text != "ok" {
		t.Fatalf("marker must be stripped from the delivered reply, got %+v", last)
	}
	if !strings.Contains(resp.contexts[0], "demo: a demo") {
		t.Fatalf("turn 1 context missing menu:\n%s", resp.contexts[0])
	}
	if strings.Contains(resp.contexts[0], "DEMO BODY") {
		t.Fatalf("turn 1 must not carry body yet:\n%s", resp.contexts[0])
	}

	if err := runOneTurn(context.Background(), &recordSink{}, resp, nil, contracts.Event{T: eventwire.EventInput, Text: "again"}, nil, eng, affordances{}, nil, 0); err != nil {
		t.Fatalf("turn 2 failed: %v", err)
	}
	if !strings.Contains(resp.contexts[1], "DEMO BODY") {
		t.Fatalf("turn 2 context missing expanded body:\n%s", resp.contexts[1])
	}
}

func TestHubSkipsInjectionWhenEngineNil(t *testing.T) {
	resp := &captureBackend{reply: "ok"}
	if err := runOneTurn(context.Background(), &recordSink{}, resp, nil, contracts.Event{T: eventwire.EventInput, Text: "hi"}, nil, nil, affordances{}, nil, 0); err != nil {
		t.Fatalf("turn failed: %v", err)
	}
	if resp.contexts[0] != "" {
		t.Fatalf("nil engine must leave context empty, got %q", resp.contexts[0])
	}
}
