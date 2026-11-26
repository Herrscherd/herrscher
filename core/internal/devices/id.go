// Package devices holds the paired devices of the daemon: their identifiers, the pairing window that admits them, their registry and the authority that certifies them.
package devices

import (
	"encoding/hex"
	"fmt"
	"io"

	"github.com/Herrscherd/herrscher/core/internal/authz"
)

const deviceIDBytes = 16

// DeviceID names a paired device.
type DeviceID struct {
	value string
}

// NewDeviceID draws a fresh identifier of 128 bits from rnd.
func NewDeviceID(rnd io.Reader) (DeviceID, error) {
	raw := make([]byte, deviceIDBytes)
	if _, err := io.ReadFull(rnd, raw); err != nil {
		return DeviceID{}, fmt.Errorf("%w: %w", ErrRandomSource, err)
	}
	return DeviceID{value: hex.EncodeToString(raw)}, nil
}

// ParseDeviceID accepts only the lowercase hexadecimal form NewDeviceID produces.
func ParseDeviceID(text string) (DeviceID, error) {
	raw, err := hex.DecodeString(text)
	if err != nil || len(raw) != deviceIDBytes || hex.EncodeToString(raw) != text {
		return DeviceID{}, ErrMalformedDeviceID
	}
	return DeviceID{value: text}, nil
}

// String returns the form the registry, the certificate and the operator all share.
func (id DeviceID) String() string {
	return id.value
}

// Principal is the principal a verified client certificate of this device mints.
func (id DeviceID) Principal() authz.Principal {
	return authz.DevicePrincipal(id.value)
}

// MarshalText writes the identifier as its hexadecimal form.
func (id DeviceID) MarshalText() ([]byte, error) {
	return []byte(id.value), nil
}

// UnmarshalText reads an identifier back, refusing anything NewDeviceID could not have made.
func (id *DeviceID) UnmarshalText(text []byte) error {
	parsed, err := ParseDeviceID(string(text))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}
