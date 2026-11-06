package manager

import (
	"sync"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

func TestOperatorLanguageIsSafeToChangeWhileReplying(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "category")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 100 {
			h.SetLanguage(i18n.French)
			h.SetLanguage(i18n.English)
		}
	}()
	go func() {
		defer wg.Done()
		for range 100 {
			_ = h.text(clitext.HostLangReset, clitext.Params{clitext.ParamLang: "en"})
			if err := h.localized(ErrUnsupportedLang, clitext.HostErrUnsupportedLang); err == nil {
				t.Error("localized returned no error")
			}
		}
	}()
	wg.Wait()
}

func TestWorktreesErrorIsSafeToLocalizeWhileTheLanguageChanges(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "category")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 100 {
			h.SetLanguage(i18n.French)
			h.SetLanguage(i18n.English)
		}
	}()
	go func() {
		defer wg.Done()
		for range 100 {
			if _, _, err := h.worktreesOn("ghost"); err == nil {
				t.Error("worktreesOn with an unknown host returned no error")
			}
		}
	}()
	wg.Wait()
}
