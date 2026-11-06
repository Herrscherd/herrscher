package manager

import (
	"errors"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
)

func TestLocalizedErrorsKeepAnEnglishErrorAndRenderTheCatalogueOnce(t *testing.T) {
	restartErr := errors.New("agent exited")
	rollbackErr := errors.New("state locked")
	const restartEnglish = "backend restart: agent exited; rollback failed, session s1 out of service: state locked"
	cases := []struct {
		name         string
		err          error
		wantError    string
		wantRendered string
		causes       []error
	}{
		{
			name:         "restart lost in english",
			err:          restartLostError(i18n.English, "s1", restartErr, rollbackErr),
			wantError:    restartEnglish,
			wantRendered: "unexpected error: backend restart: agent exited; rollback failed, session s1 out of service: state locked",
			causes:       []error{restartErr, rollbackErr},
		},
		{
			name:         "restart lost in french",
			err:          restartLostError(i18n.French, "s1", restartErr, rollbackErr),
			wantError:    restartEnglish,
			wantRendered: "erreur inattendue : red\u00e9marrage backend : agent exited ; le retour arrière a échoué, session s1 hors service : state locked",
			causes:       []error{restartErr, rollbackErr},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.wantError {
				t.Fatalf("Error() = %q, want the English %q", got, tc.wantError)
			}
			for _, cause := range tc.causes {
				if !errors.Is(tc.err, cause) {
					t.Fatalf("errors.Is(%v) = false, the cause is lost", cause)
				}
			}
		})
	}
}

func TestLocalizedErrorsRenderTheirCatalogueTextOnce(t *testing.T) {
	restartErr := errors.New("agent exited")
	rollbackErr := errors.New("state locked")
	cases := []struct {
		name string
		lang i18n.Language
		err  error
		want string
	}{
		{"restart lost en", i18n.English, restartLostError(i18n.English, "s1", restartErr, rollbackErr), "unexpected error: backend restart: agent exited; rollback failed, session s1 out of service: state locked"},
		{"restart lost fr", i18n.French, restartLostError(i18n.French, "s1", restartErr, rollbackErr), "erreur inattendue : red\u00e9marrage backend : agent exited ; le retour arrière a échoué, session s1 hors service : state locked"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cli.Localize(tc.err, tc.lang).Error(); got != tc.want {
				t.Fatalf("rendered %q, want %q", got, tc.want)
			}
		})
	}
}
