package state

import (
	"errors"
	"path/filepath"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/devices"
)

// UpdateDevices runs update on the device registry under the state lock and saves it, restoring memory if the save fails.
func (s *State) UpdateDevices(update func(*devices.Registry) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Devices == nil {
		s.Devices = &devices.Registry{}
	}
	before, rolesBefore := cloneRegistry(*s.Devices), cloneRoles(s.Roles)
	updateErr := update(s.Devices)
	s.purgeDevicesLocked(time.Now())
	if saveErr := s.saveLocked(); saveErr != nil {
		*s.Devices, s.Roles = before, rolesBefore
		return errors.Join(updateErr, saveErr)
	}
	return updateErr
}

// DeviceRegistry returns a copy of the device registry, for reading it outside the lock.
func (s *State) DeviceRegistry() devices.Registry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Devices == nil {
		return devices.Registry{}
	}
	return cloneRegistry(*s.Devices)
}

// PrivateDir is the directory of the state file, which holds the device authority in a devices subdirectory.
func (s *State) PrivateDir() string {
	return filepath.Dir(s.path)
}

// EnrollDevice approves a device and grants its role in one save, restoring memory if the save fails.
func (s *State) EnrollDevice(enroll func(*devices.Registry) (devices.Enrolment, error)) (devices.Enrolment, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Devices == nil {
		s.Devices = &devices.Registry{}
	}
	before, rolesBefore := cloneRegistry(*s.Devices), cloneRoles(s.Roles)
	enrolment, enrollErr := enroll(s.Devices)
	if enrollErr != nil {
		if saveErr := s.saveLocked(); saveErr != nil {
			*s.Devices, s.Roles = before, rolesBefore
			return devices.Enrolment{}, false, errors.Join(enrollErr, saveErr)
		}
		return devices.Enrolment{}, false, enrollErr
	}
	s.purgeDevicesLocked(time.Now())
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
		*s.Devices, s.Roles = before, rolesBefore
		return devices.Enrolment{}, false, err
	}
	return enrolment, first, nil
}

func (s *State) purgeDevicesLocked(now time.Time) {
	for _, id := range s.Devices.Purge(now) {
		delete(s.Roles, id.Principal().ID)
	}
}

func cloneRegistry(registry devices.Registry) devices.Registry {
	out := devices.Registry{Devices: append([]devices.Device(nil), registry.Devices...)}
	if registry.Pairing != nil {
		pairing := *registry.Pairing
		out.Pairing = &pairing
	}
	return out
}

func cloneRoles(roles map[string]RoleAssignment) map[string]RoleAssignment {
	if roles == nil {
		return nil
	}
	out := make(map[string]RoleAssignment, len(roles))
	for principal, assignment := range roles {
		out[principal] = assignment
	}
	return out
}
