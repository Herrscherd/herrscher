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

func TestEnrollingADeviceGrantsItsRoleInTheSameSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	id, err := devices.NewDeviceID(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	enrolled, first, err := st.EnrollDevice(func(registry *devices.Registry) (devices.Enrolment, error) {
		device := devices.Device{ID: id, Name: "phone"}
		registry.Devices = append(registry.Devices, device)
		return devices.Enrolment{Device: device, Role: authz.RoleApprover}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !first || enrolled.Device.ID != id {
		t.Fatalf("enrol reported first=%v, device %v", first, enrolled.Device.ID)
	}
	reloaded, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.RoleNames()[id.Principal().ID]; got != "approver" {
		t.Fatalf("the device holds %q after reload, want approver", got)
	}
	if len(reloaded.DeviceRegistry().Devices) != 1 {
		t.Fatal("the device must be persisted with its role")
	}
}

func TestAFailedEnrollmentPersistsNeitherTheDeviceNorItsRole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = st.EnrollDevice(func(*devices.Registry) (devices.Enrolment, error) {
		return devices.Enrolment{}, devices.ErrFingerprintMismatch
	})
	if !errors.Is(err, devices.ErrFingerprintMismatch) {
		t.Fatalf("enrol = %v, want ErrFingerprintMismatch", err)
	}
	reloaded, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.RoleNames()) != 0 || len(reloaded.DeviceRegistry().Devices) != 0 {
		t.Fatal("a failed enrolment must leave neither a device nor a role behind")
	}
}
