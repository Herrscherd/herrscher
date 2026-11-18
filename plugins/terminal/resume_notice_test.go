package terminal

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/plugins/terminal/tui"
)

func TestResumeLostNoticeReachesTheTabInEachLanguage(t *testing.T) {
	tm := New()
	got := make(chan tui.RoutedEvent, 2)
	go func() {
		got <- <-tm.Frontend()
		got <- <-tm.Frontend()
	}()
	conv := contracts.Conversation{Gateway: "terminal", ID: "terminal"}
	notice := contracts.Notice{Code: contracts.CodeResumeLost}
	for _, lang := range []i18n.Language{i18n.English, i18n.French} {
		if _, err := tm.Notify(context.Background(), conv, lang, notice); err != nil {
			t.Fatalf("Notify(%v): %v", lang, err)
		}
		want, err := contracts.NewRenderer().Render(lang, notice)
		if err != nil {
			t.Fatalf("Render(%v): %v", lang, err)
		}
		re := <-got
		if re.Event.T != eventwire.EventNotice || re.Event.Text != want || want == "" {
			t.Fatalf("Notify(%v) emitted %+v, want a notice event with %q", lang, re.Event, want)
		}
	}
}
