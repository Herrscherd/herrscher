package accounts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func newStore(t *testing.T) Store {
	t.Helper()
	return Store{Root: filepath.Join(t.TempDir(), "accounts")}
}

func mustName(t *testing.T, raw string) contracts.ProfileName {
	t.Helper()
	name, err := contracts.ParseProfileName(raw)
	if err != nil {
		t.Fatalf("ParseProfileName(%q): %v", raw, err)
	}
	return name
}

func TestCreateMakesAPrivateDirAndRefusesADuplicate(t *testing.T) {
	store := newStore(t)
	dir, err := store.Create(contracts.VendorClaude, mustName(t, "perso"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("profile dir mode %v, want 0700", info.Mode().Perm())
	}
	if _, err := store.Create(contracts.VendorClaude, mustName(t, "perso")); !errors.Is(err, ErrExists) {
		t.Fatalf("second Create = %v, want ErrExists", err)
	}
}

func TestCursorHasNoProfiles(t *testing.T) {
	store := newStore(t)
	if _, err := store.Create(contracts.VendorCursor, mustName(t, "perso")); !errors.Is(err, contracts.ErrNoProfileHome) {
		t.Fatalf("Create for cursor = %v, want ErrNoProfileHome", err)
	}
}

func TestResolvePrefersSessionOverDefaultAndFallsBackToNative(t *testing.T) {
	store := newStore(t)
	for _, name := range []string{"perso", "boulot"} {
		if _, err := store.Create(contracts.VendorClaude, mustName(t, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetDefault(contracts.VendorClaude, mustName(t, "perso")); err != nil {
		t.Fatal(err)
	}

	name, dir, err := store.Resolve("boulot", contracts.VendorClaude)
	if err != nil || name != "boulot" || dir != store.Dir(contracts.VendorClaude, mustName(t, "boulot")) {
		t.Fatalf("session choice: got %q %q %v, want boulot", name, dir, err)
	}
	name, _, err = store.Resolve("", contracts.VendorClaude)
	if err != nil || name != "perso" {
		t.Fatalf("default choice: got %q %v, want perso", name, err)
	}
	name, dir, err = store.Resolve(contracts.NativeProfile, contracts.VendorClaude)
	if err != nil || name != "" || dir != "" {
		t.Fatalf("native choice: got %q %q %v, want no profile", name, dir, err)
	}
}

func TestResolveRefusesAMissingProfile(t *testing.T) {
	store := newStore(t)
	if _, _, err := store.Resolve("fantome", contracts.VendorCodex); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Resolve of a missing profile = %v, want ErrNotFound", err)
	}
}

func TestRemoveRefusesAProfileInUse(t *testing.T) {
	store := newStore(t)
	name := mustName(t, "perso")
	if _, err := store.Create(contracts.VendorClaude, name); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove(contracts.VendorClaude, name, true); !errors.Is(err, ErrInUse) {
		t.Fatalf("Remove while in use = %v, want ErrInUse", err)
	}
	if _, err := os.Stat(store.Dir(contracts.VendorClaude, name)); err != nil {
		t.Fatalf("profile dir should survive a refused remove: %v", err)
	}
}

func TestRemoveClearsTheDefaultPointingAtIt(t *testing.T) {
	store := newStore(t)
	name := mustName(t, "perso")
	if _, err := store.Create(contracts.VendorClaude, name); err != nil {
		t.Fatal(err)
	}
	if err := store.SetDefault(contracts.VendorClaude, name); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove(contracts.VendorClaude, name, false); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if def, err := store.Default(contracts.VendorClaude); err != nil || def != "" {
		t.Fatalf("default after remove = %q %v, want empty", def, err)
	}
}

func TestListMarksTheDefault(t *testing.T) {
	store := newStore(t)
	for _, name := range []string{"perso", "boulot"} {
		if _, err := store.Create(contracts.VendorCodex, mustName(t, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetDefault(contracts.VendorCodex, mustName(t, "boulot")); err != nil {
		t.Fatal(err)
	}
	profiles, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(profiles) != 2 || profiles[0].Name != "boulot" || !profiles[0].Default || profiles[1].Default {
		t.Fatalf("List = %+v, want boulot as the only default", profiles)
	}
}

func TestStatusReadsEachVendorsAnswer(t *testing.T) {
	cases := []struct {
		vendor string
		out    string
		want   bool
	}{
		{contracts.VendorClaude, `{"loggedIn": true, "authMethod": "claude.ai"}`, true},
		{contracts.VendorClaude, `{"loggedIn": false}`, false},
		{contracts.VendorCodex, "Logged in using ChatGPT\n", true},
		{contracts.VendorCodex, "Not logged in\n", false},
	}
	for _, tc := range cases {
		run := func(_ context.Context, env map[string]string, _ []string) ([]byte, error) {
			if len(env) != 1 {
				t.Fatalf("%s status env = %v, want only the home variable", tc.vendor, env)
			}
			return []byte(tc.out), nil
		}
		env, err := contracts.ProfileEnv(tc.vendor, "/tmp/home")
		if err != nil {
			t.Fatal(err)
		}
		got, err := Status(context.Background(), run, tc.vendor, env)
		if err != nil || got != tc.want {
			t.Fatalf("Status(%s, %q) = %v %v, want %v", tc.vendor, tc.out, got, err, tc.want)
		}
	}
}
