package host

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestSessionFailureNoticeIsWrittenInTheSessionLanguage(t *testing.T) {
	h := &hub{warnings: io.Discard, lang: i18n.English}
	var published []contracts.Event
	h.eventPublisher = func(_ string, event contracts.Event) {
		published = append(published, event)
	}
	cause := errors.New("boom")
	sess := state.Session{Name: "revue", Language: "fr"}
	h.reportSessionFailure(sess, supervisor.StagePrepare, cause)

	params := map[string]any{nameParam: "revue", detailParam: cause.Error()}
	inFrench := LocalizeError(i18n.French, wrapHost(cause, sessionFailureKey(supervisor.StagePrepare), params)).Error()
	inEnglish := LocalizeError(i18n.English, wrapHost(cause, sessionFailureKey(supervisor.StagePrepare), params)).Error()
	if inFrench == inEnglish {
		t.Fatal("the two languages render the same notice, the test proves nothing")
	}
	if len(published) != 1 || published[0].Text != inFrench {
		t.Fatalf("notice = %+v, want the French text %q", published, inFrench)
	}
}

func TestSessionFailureWithoutAnEventSinkIsLogged(t *testing.T) {
	var logged bytes.Buffer
	saved := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(saved) })

	h := &hub{warnings: io.Discard, lang: i18n.English}
	h.reportSessionFailure(state.Session{Name: "revue"}, supervisor.StageLaunch, errors.New("boom"))

	if !strings.Contains(logged.String(), "session failure not published") || !strings.Contains(logged.String(), "revue") {
		t.Fatalf("a failure with no sink must be logged, got %q", logged.String())
	}
}
