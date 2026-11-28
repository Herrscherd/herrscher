package devices

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/subtle"
	"crypto/x509"
	"fmt"
	"io"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/authz"
)

const serialRadix = 16

const (
	pairingWindowLength = 10 * time.Minute
	maxPairingAttempts  = 5
)

// InactivityLimit is how long a device may go without connecting before its record expires.
const InactivityLimit = 30 * 24 * time.Hour

const (
	retentionAfterRevocation = 30 * 24 * time.Hour
	retentionAfterExpiry     = 30 * 24 * time.Hour
	maxRequestBytes          = 4096
)

// Registry is the set of devices the daemon has paired, with the pairing window in progress, if any.
type Registry struct {
	Devices []Device `json:"devices,omitempty"`
	Pairing *Window  `json:"pairing,omitempty"`
}

// Window is an open pairing window.
type Window struct {
	Name      string     `json:"name,omitempty"`
	Role      authz.Role `json:"role"`
	Salt      []byte     `json:"salt"`
	Digest    []byte     `json:"digest"`
	ExpiresAt time.Time  `json:"expiresAt"`
	Failures  int        `json:"failures,omitempty"`
	Pending   *Request   `json:"pending,omitempty"`
}

// Request is a device that redeemed the window's code and waits for an operator.
type Request struct {
	CSR         []byte    `json:"csr"`
	Fingerprint string    `json:"fingerprint"`
	RedeemedAt  time.Time `json:"redeemedAt"`
}

// Device is one paired device as the daemon records it.
type Device struct {
	ID          DeviceID  `json:"id"`
	Name        string    `json:"name,omitempty"`
	Fingerprint string    `json:"fingerprint"`
	Serial      string    `json:"serial"`
	CreatedAt   time.Time `json:"createdAt"`
	LastSeen    time.Time `json:"lastSeen"`
	Revoked     bool      `json:"revoked,omitempty"`
	RevokedAt   time.Time `json:"revokedAt,omitempty"`
}

// Status is what a device's record says about whether it may connect.
type Status uint8

const (
	// StatusActive is a device that may connect.
	StatusActive Status = iota + 1
	// StatusExpired is a device that has not connected within InactivityLimit.
	StatusExpired
	// StatusRevoked is a device an operator revoked.
	StatusRevoked
)

// Enrolment is what an approval produced: the new device, the role it was granted and the certificate that the device keeps.
type Enrolment struct {
	Device      Device
	Role        authz.Role
	Fingerprint string
	Certificate *x509.Certificate
}

// Signer certifies the key of a request for the device id, valid from now on.
type Signer func(csr *x509.CertificateRequest, deviceID DeviceID, now time.Time) (*x509.Certificate, error)

// String is the machine name of the status, which the device listing prints as JSON.
func (s Status) String() string {
	switch s {
	case StatusActive:
		return "active"
	case StatusExpired:
		return "expired"
	case StatusRevoked:
		return "revoked"
	}
	return "unknown"
}

// StatusAt reports what the record says at now.
func (d Device) StatusAt(now time.Time) Status {
	switch {
	case d.Revoked:
		return StatusRevoked
	case now.Sub(d.LastSeen) > InactivityLimit:
		return StatusExpired
	}
	return StatusActive
}

// OpenWindow opens a pairing window that grants role to the device once an operator approves it, and returns the code the operator reads out.
func (r *Registry) OpenWindow(now time.Time, name string, role authz.Role, rnd io.Reader) (PairingCode, error) {
	if !authz.Grantable(string(role)) {
		return PairingCode{}, ErrUngrantableRole
	}
	if r.Pairing != nil && now.Before(r.Pairing.ExpiresAt) {
		return PairingCode{}, ErrPairingOpen
	}
	code, err := NewPairingCode(rnd)
	if err != nil {
		return PairingCode{}, err
	}
	salt, err := newSalt(rnd)
	if err != nil {
		return PairingCode{}, err
	}
	r.Pairing = &Window{
		Name:      name,
		Role:      role,
		Salt:      salt,
		Digest:    code.digest(salt),
		ExpiresAt: now.Add(pairingWindowLength),
	}
	return code, nil
}

// Redeem takes the window's code and the request of a device that asks to be paired.
func (r *Registry) Redeem(now time.Time, code PairingCode, der []byte) error {
	window, err := r.window(now, code)
	if err != nil {
		return err
	}
	if window.Pending != nil {
		return ErrPairingAlreadyRedeemed
	}
	if len(der) > maxRequestBytes {
		return ErrRequestTooLarge
	}
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBadRequest, err)
	}
	if checkErr := checkRequest(csr); checkErr != nil {
		return checkErr
	}
	window.Pending = &Request{
		CSR:         append([]byte(nil), der...),
		Fingerprint: ShortFingerprint(csr.RawSubjectPublicKeyInfo),
		RedeemedAt:  now,
	}
	return nil
}

// Preview is what an operator reads before approving: the name and role the window was opened with, and the fingerprint the device shows.
type Preview struct {
	Name        string
	Role        authz.Role
	Fingerprint string
}

// Preview returns the pending request of the window, and signs nothing.
func (r *Registry) Preview(now time.Time, code PairingCode) (Preview, error) {
	window, err := r.window(now, code)
	if err != nil {
		return Preview{}, err
	}
	if window.Pending == nil {
		return Preview{}, ErrNothingPending
	}
	return Preview{Name: window.Name, Role: window.Role, Fingerprint: window.Pending.Fingerprint}, nil
}

// Approve registers the pending device under a fresh identifier and closes the window.
func (r *Registry) Approve(now time.Time, code PairingCode, fingerprint Fingerprint, rnd io.Reader, sign Signer) (Enrolment, error) {
	window, err := r.window(now, code)
	if err != nil {
		return Enrolment{}, err
	}
	if window.Pending == nil {
		return Enrolment{}, ErrNothingPending
	}
	if !fingerprint.matches(window.Pending.Fingerprint) {
		return Enrolment{}, r.refuse(window, ErrFingerprintMismatch)
	}
	csr, err := x509.ParseCertificateRequest(window.Pending.CSR)
	if err != nil {
		return Enrolment{}, fmt.Errorf("%w: %w", ErrBadRequest, err)
	}
	if checkErr := checkRequest(csr); checkErr != nil {
		return Enrolment{}, checkErr
	}
	deviceID, err := NewDeviceID(rnd)
	if err != nil {
		return Enrolment{}, err
	}
	cert, err := sign(csr, deviceID, now)
	if err != nil {
		return Enrolment{}, err
	}
	device := Device{
		ID:          deviceID,
		Name:        window.Name,
		Fingerprint: KeyFingerprint(cert.RawSubjectPublicKeyInfo),
		Serial:      cert.SerialNumber.Text(serialRadix),
		CreatedAt:   now,
		LastSeen:    now,
	}
	enrolment := Enrolment{
		Device:      device,
		Role:        window.Role,
		Fingerprint: window.Pending.Fingerprint,
		Certificate: cert,
	}
	r.Devices = append(r.Devices, device)
	r.Pairing = nil
	return enrolment, nil
}

// Deny closes the window on a right code, so that nothing is granted.
func (r *Registry) Deny(now time.Time, code PairingCode) error {
	if _, err := r.window(now, code); err != nil {
		return err
	}
	r.Pairing = nil
	return nil
}

// Revoke marks a device as revoked, so that its certificate authenticates it no more.
func (r *Registry) Revoke(now time.Time, deviceID DeviceID) error {
	idx := r.indexOf(deviceID)
	if idx < 0 {
		return ErrUnknownDevice
	}
	if !r.Devices[idx].Revoked {
		r.Devices[idx].Revoked = true
		r.Devices[idx].RevokedAt = now
	}
	return nil
}

// Purge removes the devices whose retention is over and returns their identifiers, so their roles can go too.
func (r *Registry) Purge(now time.Time) []DeviceID {
	kept := make([]Device, 0, len(r.Devices))
	var purged []DeviceID
	for idx := range r.Devices {
		device := &r.Devices[idx]
		if device.retentionOverAt(now) {
			purged = append(purged, device.ID)
			continue
		}
		kept = append(kept, *device)
	}
	r.Devices = kept
	return purged
}

func (d Device) retentionOverAt(now time.Time) bool {
	if d.Revoked {
		return now.Sub(d.RevokedAt) > retentionAfterRevocation
	}
	return now.Sub(d.LastSeen) > InactivityLimit+retentionAfterExpiry
}

// Seen records that the device connected at now, which keeps it from expiring.
func (r *Registry) Seen(deviceID DeviceID, now time.Time) error {
	idx := r.indexOf(deviceID)
	if idx < 0 {
		return ErrUnknownDevice
	}
	r.Devices[idx].LastSeen = now
	return nil
}

// Find returns the device with the given id and whether it exists.
func (r *Registry) Find(deviceID DeviceID) (Device, bool) {
	idx := r.indexOf(deviceID)
	if idx < 0 {
		return Device{}, false
	}
	return r.Devices[idx], true
}

// Authenticate turns a client certificate the authority verified into the principal of its device.
func (r *Registry) Authenticate(client VerifiedClient, now time.Time) (authz.Principal, error) {
	cert := client.cert
	if cert == nil {
		return authz.Principal{}, ErrUntrustedCertificate
	}
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return authz.Principal{}, ErrCertificateExpired
	}
	deviceID, err := ParseDeviceID(cert.Subject.CommonName)
	if err != nil {
		return authz.Principal{}, fmt.Errorf("%w: %w", ErrUnknownDevice, err)
	}
	device, found := r.Find(deviceID)
	if !found {
		return authz.Principal{}, ErrUnknownDevice
	}
	switch device.StatusAt(now) {
	case StatusRevoked:
		return authz.Principal{}, ErrDeviceRevoked
	case StatusExpired:
		return authz.Principal{}, ErrDeviceExpired
	case StatusActive:
	}
	if cert.SerialNumber.Text(serialRadix) != device.Serial || KeyFingerprint(cert.RawSubjectPublicKeyInfo) != device.Fingerprint {
		return authz.Principal{}, ErrCertificateMismatch
	}
	return deviceID.Principal(), nil
}

func (r *Registry) window(now time.Time, code PairingCode) (*Window, error) {
	window := r.Pairing
	if window == nil {
		return nil, ErrNoPairingWindow
	}
	if !now.Before(window.ExpiresAt) {
		r.Pairing = nil
		return nil, ErrPairingExpired
	}
	if subtle.ConstantTimeCompare(window.Digest, code.digest(window.Salt)) == 1 {
		return window, nil
	}
	return nil, r.refuse(window, ErrWrongCode)
}

func (r *Registry) refuse(window *Window, reason error) error {
	window.Failures++
	if window.Failures >= maxPairingAttempts {
		r.Pairing = nil
		return ErrTooManyAttempts
	}
	return reason
}

func (r *Registry) indexOf(deviceID DeviceID) int {
	for idx := range r.Devices {
		if r.Devices[idx].ID == deviceID {
			return idx
		}
	}
	return -1
}

func acceptsSignature(algorithm x509.SignatureAlgorithm) bool {
	return algorithm == x509.ECDSAWithSHA256 || algorithm == x509.ECDSAWithSHA384
}

func checkRequest(csr *x509.CertificateRequest) error {
	if csr == nil {
		return ErrBadRequest
	}
	if !acceptsSignature(csr.SignatureAlgorithm) {
		return ErrUnsupportedSignature
	}
	if err := csr.CheckSignature(); err != nil {
		return fmt.Errorf("%w: %w", ErrBadRequest, err)
	}
	key, isECDSA := csr.PublicKey.(*ecdsa.PublicKey)
	if !isECDSA || key.Curve != elliptic.P256() {
		return ErrUnsupportedKey
	}
	return nil
}
