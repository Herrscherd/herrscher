package manager

import (
	"context"
	"errors"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func TestSetHomeCategory(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	if _, err := h.setHomeRun(context.Background(), args("channel", "cat1")); err != nil {
		t.Fatal(err)
	}
	if st.Home.ID != "cat1" || st.Home.Type != "category" {
		t.Fatalf("home not persisted: %+v", st.Home)
	}
}

func TestSetHomeRejectsPlainChannel(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "text")
	if _, err := h.setHomeRun(context.Background(), args("channel", "c9")); err == nil {
		t.Fatal("expected a plain text channel to be rejected as home")
	}
	if st.Home.ID != "" {
		t.Fatalf("rejected home must not persist: %+v", st.Home)
	}
}

func TestSetHomeMissingChannel(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "category")
	if _, err := h.setHomeRun(context.Background(), args()); err == nil {
		t.Fatal("expected missing channel to error")
	}
}

func TestSetSource(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	if _, err := h.setSourceRun(context.Background(), args("path", "/src/herrscher")); err != nil {
		t.Fatal(err)
	}
	if st.SourceDir() != "/src/herrscher" {
		t.Fatalf("source not persisted: %q", st.SourceDir())
	}
}

func TestSetHomeRejectionIsTheHomeChannelKindNotice(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "text")
	_, err := h.setHomeRun(context.Background(), args("channel", "c9"))
	notice, ok := errors.AsType[contracts.NoticeError](err)
	if !ok {
		t.Fatalf("%v carries no notice", err)
	}
	if got := notice.Notice().Code; got != contracts.CodeChannelKindHome {
		t.Fatalf("code = %s, want %s", got.Key(), contracts.CodeChannelKindHome.Key())
	}
}

func TestSetLangPersistsAndRepliesInTheNewLanguage(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	h.SetDefaultLanguage(i18n.English)
	res, err := h.setLangRun(context.Background(), args("lang", "fr"))
	if err != nil {
		t.Fatal(err)
	}
	if st.OperatorLang != "fr" {
		t.Fatalf("operator language not persisted: %q", st.OperatorLang)
	}
	if h.lang != i18n.French {
		t.Fatalf("handler language = %v, want French", h.lang)
	}
	reply, isText := res.Data().(string)
	if !isText || !strings.Contains(reply, "langue de l'opérateur réglée sur fr") {
		t.Fatalf("reply not in French: %q", reply)
	}
}

func TestSetLangNotifiesTheLanguageHook(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "category")
	var notified []i18n.Language
	h.OnLanguageChange(func(lang i18n.Language) { notified = append(notified, lang) })
	h.SetDefaultLanguage(i18n.English)
	if _, err := h.setLangRun(context.Background(), args("lang", "fr")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.setLangRun(context.Background(), args("lang", "auto")); err != nil {
		t.Fatal(err)
	}
	if len(notified) != 2 || notified[0] != i18n.French || notified[1] != i18n.English {
		t.Fatalf("hook saw %v, want French then English", notified)
	}
}

func TestSetLangAutoAndEmptyRestoreTheDefault(t *testing.T) {
	for _, value := range []string{"auto", ""} {
		t.Run("value "+value, func(t *testing.T) {
			h, _, _, _, _, st := newTestHandler(t, "category")
			h.SetDefaultLanguage(i18n.English)
			if _, err := h.setLangRun(context.Background(), args("lang", "fr")); err != nil {
				t.Fatal(err)
			}
			if _, err := h.setLangRun(context.Background(), args("lang", value)); err != nil {
				t.Fatal(err)
			}
			if st.OperatorLang != "" {
				t.Fatalf("reset must clear the stored language: %q", st.OperatorLang)
			}
			if h.lang != i18n.English {
				t.Fatalf("handler language = %v, want the default English", h.lang)
			}
		})
	}
}

func TestSetLangRejectsAnythingOutsideTheClosedSet(t *testing.T) {
	for _, value := range []string{"en_US", "FR", "fr_FR.UTF-8", "klingon"} {
		t.Run(value, func(t *testing.T) {
			h, _, _, _, _, st := newTestHandler(t, "category")
			if _, err := h.setLangRun(context.Background(), args("lang", "fr")); err != nil {
				t.Fatal(err)
			}
			_, err := h.setLangRun(context.Background(), args("lang", value))
			if !errors.Is(err, ErrUnsupportedLang) {
				t.Fatalf("err = %v, want ErrUnsupportedLang", err)
			}
			if st.OperatorLang != "fr" {
				t.Fatalf("rejected value must not replace the stored language: %q", st.OperatorLang)
			}
		})
	}
}
