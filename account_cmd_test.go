package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/accounts"
)

func TestAccountArgsRefuseAnUnknownVendor(t *testing.T) {
	if _, _, err := accountArgs([]string{"remove", "victim", "perso"}); err == nil {
		t.Fatal("accountArgs accepted an unknown vendor")
	}
	vendor, name, err := accountArgs([]string{"remove", contracts.VendorClaude, "perso"})
	if err != nil || vendor != contracts.VendorClaude || name != "perso" {
		t.Fatalf("accountArgs(claude, perso) = %q %q %v", vendor, name, err)
	}
}

func TestRefuseProfileInUseNamesTheSessions(t *testing.T) {
	if err := refuseProfileInUse(contracts.VendorClaude, "perso", nil); err != nil {
		t.Fatalf("unused profile refused: %v", err)
	}
	err := refuseProfileInUse(contracts.VendorClaude, "perso", []string{"alpha", "beta"})
	if !errors.Is(err, accounts.ErrInUse) {
		t.Fatalf("err = %v, want ErrInUse", err)
	}
	if !strings.Contains(err.Error(), "alpha, beta") {
		t.Fatalf("refusal does not name the sessions: %v", err)
	}
}

func TestWarnResumingNamesTheSessionsAndTheWayOut(t *testing.T) {
	var quiet bytes.Buffer
	if err := warnResuming(contracts.VendorClaude, nil, &quiet); err != nil || quiet.Len() != 0 {
		t.Fatalf("no resuming session: output %q err %v, want silence", quiet.String(), err)
	}
	var loud bytes.Buffer
	if err := warnResuming(contracts.VendorClaude, []string{"alpha"}, &loud); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1 session", "alpha", "set-account"} {
		if !strings.Contains(loud.String(), want) {
			t.Fatalf("warning %q lacks %q", loud.String(), want)
		}
	}
}
