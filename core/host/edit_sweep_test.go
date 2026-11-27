package host

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/testsupport/stagingtest"
)

func TestEnvSetSweepsTheStagingFilesOfTheEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "herrscher.env")
	if err := os.WriteFile(path, []byte("A=1\n"), envFileMode); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, ".herrscher.env.4242600")
	fresh := filepath.Join(dir, ".herrscher.env.4242601")
	stagingtest.Plant(t, stale, time.Hour)
	stagingtest.Plant(t, fresh, time.Second)
	set := envSetVerb(clitext.NewMessages(), fixedLanguage(i18n.English), func() string { return path }, newEditLock())

	if _, err := set.Run(t.Context(), contracts.Input{Args: map[string]string{paramKey: "B", paramValue: "2"}}); err != nil {
		t.Fatalf("env set: %v", err)
	}

	stagingtest.Absent(t, stale)
	stagingtest.Present(t, fresh)
}

func TestConfigSetSweepsTheStagingFilesOfTheConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	stale := filepath.Join(dir, ".config.json.4242610")
	fresh := filepath.Join(dir, ".config.json.4242611")
	stagingtest.Plant(t, stale, time.Hour)
	stagingtest.Plant(t, fresh, time.Second)
	set := verbNamed(t, configVerbs(clitext.NewMessages(), fixedLanguage(i18n.English), func() string { return path }, newEditLock()), "set")

	if _, err := set.Run(t.Context(), contracts.Input{Args: map[string]string{paramKey: "language", paramValue: "fr"}}); err != nil {
		t.Fatalf("config set: %v", err)
	}

	stagingtest.Absent(t, stale)
	stagingtest.Present(t, fresh)
}

func TestConfigUnsetSweepsTheStagingFilesOfTheConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"language":"fr"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, ".config.json.4242620")
	fresh := filepath.Join(dir, ".config.json.4242621")
	stagingtest.Plant(t, stale, time.Hour)
	stagingtest.Plant(t, fresh, time.Second)
	unset := verbNamed(t, configVerbs(clitext.NewMessages(), fixedLanguage(i18n.English), func() string { return path }, newEditLock()), "unset")

	if _, err := unset.Run(t.Context(), contracts.Input{Args: map[string]string{paramKey: "language"}}); err != nil {
		t.Fatalf("config unset: %v", err)
	}

	stagingtest.Absent(t, stale)
	stagingtest.Present(t, fresh)
}

func verbNamed(t *testing.T, verbs []contracts.Cmd, name string) contracts.Cmd {
	t.Helper()
	for _, verb := range verbs {
		if verb.Path[len(verb.Path)-1] == name {
			return verb
		}
	}
	t.Fatalf("no verb named %s", name)
	return contracts.Cmd{}
}
