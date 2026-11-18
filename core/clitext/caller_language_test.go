package clitext

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func TestCallerLanguageComesFromTheContextOrTheFallback(t *testing.T) {
	fallback := func() i18n.Language { return i18n.English }
	if got := CallerLanguage(WithCallerLanguage(context.Background(), "fr"), fallback); got != i18n.French {
		t.Fatalf("CallerLanguage with fr = %v, want French", got)
	}
	if got := CallerLanguage(context.Background(), fallback); got != i18n.English {
		t.Fatalf("CallerLanguage without a tag = %v, want the fallback", got)
	}
	if got := CallerLanguage(WithCallerLanguage(context.Background(), "xx"), func() i18n.Language { return i18n.French }); got != i18n.French {
		t.Fatalf("CallerLanguage with an unsupported tag = %v, want the fallback", got)
	}
}
