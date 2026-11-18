package host

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestServiceUpdateBuildErrorComesBackInTheCallerLanguage(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module example.com/other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.SetSource(src); err != nil {
		t.Fatal(err)
	}
	messages := clitext.NewMessages()
	updater := serviceUpdater{st: st, messages: messages, lang: fixedLanguageSource(i18n.English)}
	wantFrench, err := messages.Text(i18n.French, clitext.ServiceNotCheckoutWrongModule, clitext.Params{clitext.ParamPath: src})
	if err != nil {
		t.Fatal(err)
	}
	wantEnglish, err := messages.Text(i18n.English, clitext.ServiceNotCheckoutWrongModule, clitext.Params{clitext.ParamPath: src})
	if err != nil {
		t.Fatal(err)
	}

	_, err = updater.Build(clitext.WithCallerLanguage(context.Background(), "fr"), false)
	if err == nil || err.Error() != wantFrench {
		t.Fatalf("fr client build error = %v, want %q", err, wantFrench)
	}
	_, err = updater.Build(context.Background(), false)
	if err == nil || err.Error() != wantEnglish {
		t.Fatalf("build error without caller language = %v, want the operator language %q", err, wantEnglish)
	}
}
