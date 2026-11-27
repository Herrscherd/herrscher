package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestADocumentEditIsReadBackByLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	doc, err := ReadDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"compactAt": "70", "telemetry.enabled": "false", "cmd": "claude --verbose"} {
		if setErr := doc.Set(key, value); setErr != nil {
			t.Fatalf("Set(%s): %v", key, setErr)
		}
	}
	if _, err = doc.Write(path); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CompactAt == nil || *cfg.CompactAt != 70 || !cfg.TelemetryOff() || cfg.Cmd != "claude --verbose" {
		t.Fatalf("Load = %+v, want the three edits", cfg)
	}

	reread, err := ReadDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = reread.Unset("telemetry.enabled"); err != nil {
		t.Fatal(err)
	}
	if _, err = reread.Write(path); err != nil {
		t.Fatal(err)
	}
	if cfg, err = Load(path); err != nil || cfg.TelemetryOff() {
		t.Fatalf("after unset Load = %+v, %v, want telemetry back to its default", cfg, err)
	}
}

func TestAWriteTheDaemonWouldRefuseLeavesTheFileAsItWas(t *testing.T) {
	cases := map[string][2]string{
		"an unknown key":          {"compactAtt", "70"},
		"a value of another type": {"compactAt", `"seventy"`},
		"a refused value":         {"compactAt", "250"},
	}
	for name, edit := range cases {
		path := filepath.Join(t.TempDir(), "config.json")
		const before = "{\n  \"cmd\": \"claude\"\n}\n"
		if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
			t.Fatal(err)
		}
		doc, err := ReadDocument(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := doc.Set(edit[0], edit[1]); err != nil {
			t.Fatalf("%s: Set: %v", name, err)
		}
		if _, err := doc.Write(path); err == nil {
			t.Errorf("%s: Write accepted what Load refuses", name)
		}
		if after, readErr := os.ReadFile(path); readErr != nil || string(after) != before {
			t.Errorf("%s: file = %q, %v, want it untouched", name, after, readErr)
		}
	}
}

func TestAKeyTheFileAlreadyHeldDoesNotBlockAnEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"$schema": "https://example.invalid/config.json", "cmd": "claude"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := ReadDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = doc.Set("compactAt", "60"); err != nil {
		t.Fatal(err)
	}
	if _, err = doc.Write(path); err != nil {
		t.Fatalf("Write refused an edit because of a key Load ignores: %v", err)
	}
}

func TestARewriteKeepsTheCommentedFileBesideIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(Template("", ":8787")), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := ReadDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = doc.Set("language", "fr"); err != nil {
		t.Fatal(err)
	}
	backup, err := doc.Write(path)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := os.ReadFile(backup)
	if err != nil || !strings.Contains(string(kept), "//") {
		t.Fatalf("backup %q = %q, %v, want the commented original", backup, kept, err)
	}
}

func TestAKeyThatCrossesAValueOrMissesIsNamed(t *testing.T) {
	doc := Document{root: map[string]any{"cmd": "claude"}}
	if err := doc.Set("cmd.inner", "x"); !errors.Is(err, ErrNotAnObject) {
		t.Errorf("Set across a string: %v, want ErrNotAnObject", err)
	}
	if err := doc.Unset("telemetry.enabled"); !errors.Is(err, ErrNoSuchKey) {
		t.Errorf("Unset of an absent key: %v, want ErrNoSuchKey", err)
	}
	if _, err := doc.Get("a..b"); !errors.Is(err, ErrEmptyKey) {
		t.Errorf("Get of an empty segment: %v, want ErrEmptyKey", err)
	}
}

func TestWriteReplacesTheFilePrivatelyPastAStrayStagingName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	doc, err := ReadDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = doc.Set("cmd", "claude --verbose"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err = doc.Write(path); err != nil {
		t.Fatalf("Write blocked by a stray staging name: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != documentMode {
		t.Fatalf("config mode = %v; want %v", info.Mode().Perm(), documentMode)
	}
	if cfg, err := Load(path); err != nil || cfg.Cmd != "claude --verbose" {
		t.Fatalf("Load = %+v, %v; want the edit", cfg, err)
	}
}
