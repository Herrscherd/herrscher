package clitext

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func TestForgeWordsTranslateEveryTokenSource(t *testing.T) {
	french := NewMessages().For(i18n.French)
	sources := []contracts.TokenSource{contracts.TokenSourceSaved, contracts.TokenSourceApp, contracts.TokenSourceEnv, contracts.TokenSourceCLI}
	for _, source := range sources {
		key := ForgeSourceKey(source)
		if key == forgeSourceUnknown {
			t.Errorf("source %v maps to the unknown key", source)
		}
		if text := french.Text(key, nil); text == "" || text == source.String() {
			t.Errorf("source %v renders %q in French, want a French word", source, text)
		}
	}
	var unset contracts.TokenSource
	if key := ForgeSourceKey(unset); key != forgeSourceUnknown {
		t.Errorf("unset source maps to %q, want the unknown key", key)
	}
}

func TestForgeWordsTranslateEveryRepoPermission(t *testing.T) {
	french := NewMessages().For(i18n.French)
	permissions := []contracts.RepoPermission{contracts.PermissionNone, contracts.PermissionRead, contracts.PermissionTriage, contracts.PermissionWrite, contracts.PermissionMaintain, contracts.PermissionAdmin}
	for _, permission := range permissions {
		key := ForgePermissionKey(permission)
		if key == forgePermissionUnkn {
			t.Errorf("permission %v maps to the unknown key", permission)
		}
		if text := french.Text(key, nil); text == "" || text == permission.String() {
			t.Errorf("permission %v renders %q in French, want a French word", permission, text)
		}
	}
}
