package host

import (
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestAccountChangeNoticeSaysWhenItTakesEffect(t *testing.T) {
	plain := accountChangeNotice("alpha", "perso", false)
	if !strings.Contains(plain, "effectif au prochain lancement") || !strings.Contains(plain, "garde l'ancien compte") {
		t.Fatalf("plain notice does not say the live bridge keeps the old account: %q", plain)
	}
	fresh := accountChangeNotice("alpha", "perso", true)
	if !strings.Contains(fresh, "n'est plus reprise") || !strings.Contains(fresh, "nouvelle s'ouvre") {
		t.Fatalf("fresh notice does not say the conversation is dropped: %q", fresh)
	}
	if defaulted := accountChangeNotice("alpha", "", false); !strings.Contains(defaulted, "le défaut du vendeur") {
		t.Fatalf("default choice notice = %q, want it named as the vendor default", defaulted)
	}
	if native := accountChangeNotice("alpha", contracts.NativeProfile, false); !strings.Contains(native, "le compte natif") {
		t.Fatalf("native notice = %q, want it named as the native login", native)
	}
}
