package host

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/accounts"
	"github.com/Herrscherd/herrscher/core/internal/manager"
)

func TestWiredManagerRefusesAnUnknownAccount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	name, err := contracts.ParseProfileName("perso")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (accounts.Store{Root: accounts.DefaultRoot(home)}).Create("claude", name); err != nil {
		t.Fatal(err)
	}
	hdl := manager.NewHandler(nil, nil, nil, nil, nil, nil, nil, "claude", t.TempDir(), nil)
	wireManagerHandler(t.Context(), hdl, nil, "inst", nil, nil)
	if err := hdl.CheckAccount("claude", "ghost"); err == nil {
		t.Fatal("wired handler accepted an unknown account")
	}
	if err := hdl.CheckAccount("claude", "perso"); err != nil {
		t.Fatalf("wired handler refused a known account: %v", err)
	}
}
