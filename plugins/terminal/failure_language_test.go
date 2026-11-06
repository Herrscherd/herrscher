package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTerminalFailuresFollowTheClientLanguage(t *testing.T) {
	cases := []struct {
		lang     string
		resume   string
		verb     string
		dispatch string
	}{
		{
			lang:     "fr",
			resume:   "aucun contrôle de session n'est lié à ce terminal",
			dispatch: `la commande "bogus" n'est pas disponible depuis le terminal`,
		},
		{
			lang:     "en",
			resume:   "no session control is bound to this terminal",
			dispatch: `command "bogus" is not available from the terminal`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"language":"`+tc.lang+`"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HERRSCHER_STATE_DIR", dir)
			term := &Terminal{}

			_, err := term.Resume("alpha")
			if err == nil || err.Error() != tc.resume {
				t.Fatalf("Resume error = %v, want %q", err, tc.resume)
			}
			if _, err := term.Dispatch([]string{"bogus"}); err == nil || !strings.HasPrefix(err.Error(), tc.dispatch) {
				t.Fatalf("Dispatch error = %v, want a prefix %q", err, tc.dispatch)
			}
		})
	}
}
