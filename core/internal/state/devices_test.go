package state

import (
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/authz"
	"github.com/Herrscherd/herrscher/core/internal/devices"
)

func TestAFailedCodeIsSavedAndStillCountedAfterAReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	var code devices.PairingCode
	if err = st.UpdateDevices(func(registry *devices.Registry) error {
		opened, openErr := registry.OpenWindow(now, "phone", authz.RoleApprover, rand.Reader)
		code = opened
		return openErr
	}); err != nil {
		t.Fatal(err)
	}
	wrong, err := devices.NewPairingCode(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for wrong.String() == code.String() {
		if wrong, err = devices.NewPairingCode(rand.Reader); err != nil {
			t.Fatal(err)
		}
	}
	err = st.UpdateDevices(func(registry *devices.Registry) error {
		return registry.Deny(now, wrong)
	})
	if !errors.Is(err, devices.ErrWrongCode) {
		t.Fatalf("wrong code = %v, want ErrWrongCode", err)
	}
	reloaded, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.DeviceRegistry().Pairing.Failures; got != 1 {
		t.Fatalf("failures after reload = %d, want 1: a refused code must survive a restart", got)
	}
}

func TestTheDeviceRegistryIsReadBackFromTheStateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	if err = st.UpdateDevices(func(registry *devices.Registry) error {
		_, openErr := registry.OpenWindow(now, "phone", authz.RoleObserver, rand.Reader)
		return openErr
	}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	pairing := reloaded.DeviceRegistry().Pairing
	if pairing == nil || pairing.Name != "phone" || pairing.Role != authz.RoleObserver {
		t.Fatalf("pairing window after reload = %+v", pairing)
	}
	if reloaded.PrivateDir() != filepath.Dir(path) {
		t.Fatalf("PrivateDir = %q, want the state directory", reloaded.PrivateDir())
	}
}
