package tui

import (
	"slices"
	"testing"
)

func TestAccountCommandPassesFreshThrough(t *testing.T) {
	plain := setAccountArgv("alpha", []string{"perso"})
	if !slices.Equal(plain, []string{"session", "set-account", "alpha", "perso"}) {
		t.Fatalf("argv without fresh = %v", plain)
	}
	fresh := setAccountArgv("alpha", []string{"perso", "fresh"})
	if !slices.Equal(fresh, []string{"session", "set-account", "alpha", "perso", "fresh"}) {
		t.Fatalf("argv with fresh = %v, want the fresh word forwarded", fresh)
	}
}
