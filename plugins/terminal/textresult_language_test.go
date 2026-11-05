package terminal

import (
	"os"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/language"
)

func TestTUIFollowsTheClientLocaleWithoutPublishingIt(t *testing.T) {
	t.Setenv("HERRSCHER_STATE_DIR", t.TempDir())
	t.Setenv(language.EnvVar, "")
	t.Setenv("LANG", "fr_FR.UTF-8")
	notice := contracts.NoticeResult(contracts.Notice{Code: contracts.CodeSessionClosed, Args: []contracts.Arg{contracts.Text("Name", "demo")}})
	got, err := textResult(notice, nil)
	if err != nil {
		t.Fatal(err)
	}
	french, err := cli.Render(notice, nil, i18n.French)
	if err != nil {
		t.Fatal(err)
	}
	english, err := cli.Render(notice, nil, i18n.English)
	if err != nil {
		t.Fatal(err)
	}
	if got != french || got == english {
		t.Fatalf("TUI text = %q, want the French rendering %q, not English %q", got, french, english)
	}
	if published := os.Getenv(language.EnvVar); published != "" {
		t.Fatalf("%s = %q, the TUI must not publish the client language to the daemon", language.EnvVar, published)
	}
}
