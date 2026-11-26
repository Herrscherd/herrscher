package state

import (
	"errors"
	"path/filepath"

	"github.com/Herrscherd/herrscher/core/internal/devices"
)

// UpdateDevices runs update on the device registry under the state's lock and persists the result.
func (s *State) UpdateDevices(update func(*devices.Registry) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Devices == nil {
		s.Devices = &devices.Registry{}
	}
	updateErr := update(s.Devices)
	if saveErr := s.saveLocked(); saveErr != nil {
		return errors.Join(updateErr, saveErr)
	}
	return updateErr
}

// DeviceRegistry returns a copy of the registry, for reading it outside the lock.
func (s *State) DeviceRegistry() devices.Registry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Devices == nil {
		return devices.Registry{}
	}
	out := devices.Registry{Devices: append([]devices.Device(nil), s.Devices.Devices...)}
	if s.Devices.Pairing != nil {
		pairing := *s.Devices.Pairing
		out.Pairing = &pairing
	}
	return out
}

// PrivateDir is the directory the state file lives in, where the device authority keeps its keys.
func (s *State) PrivateDir() string {
	return filepath.Dir(s.path)
}

// EnrollDevice runs enroll on the registry and grants the role of the device it approved, under one lock and in one save.
func (s *State) EnrollDevice(enroll func(*devices.Registry) (devices.Enrolment, error)) (devices.Enrolment, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Devices == nil {
		s.Devices = &devices.Registry{}
	}
	enrolment, enrollErr := enroll(s.Devices)
	if enrollErr != nil {
		return devices.Enrolment{}, false, errors.Join(enrollErr, s.saveLocked())
	}
	first := len(s.Roles) == 0
	if s.Roles == nil {
		s.Roles = make(map[string]RoleAssignment)
	}
	label := enrolment.Device.Name
	if label == "" {
		label = enrolment.Device.ID.String()
	}
	s.Roles[enrolment.Device.ID.Principal().ID] = RoleAssignment{Role: string(enrolment.Role), Label: label}
	if err := s.saveLocked(); err != nil {
		return devices.Enrolment{}, false, err
	}
	return enrolment, first, nil
}
