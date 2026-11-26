package devices_test

import (
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/authz"
	"github.com/Herrscherd/herrscher/core/internal/devices"
)

func TestDeviceIDRoundTripsAndRefusesOtherForms(t *testing.T) {
	id, err := devices.NewDeviceID(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := devices.ParseDeviceID(id.String())
	if err != nil || parsed != id {
		t.Fatalf("round trip gave %v, %v", parsed, err)
	}
	for _, text := range []string{"", "abc", strings.ToUpper(id.String()), id.String() + "0", "zz" + id.String()[2:]} {
		if _, err := devices.ParseDeviceID(text); !errors.Is(err, devices.ErrMalformedDeviceID) {
			t.Fatalf("ParseDeviceID(%q) = %v, want malformed", text, err)
		}
	}
}

func TestPairingCodesUseNoAmbiguousSymbols(t *testing.T) {
	seen := make(map[string]bool)
	for range 200 {
		code, err := devices.NewPairingCode(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(code.String(), "-", "")
		if len(text) != 10 {
			t.Fatalf("code %q has %d symbols, want 10", text, len(text))
		}
		if strings.ContainsAny(text, "01IO") {
			t.Fatalf("code %q holds an ambiguous symbol", text)
		}
		seen[text] = true
	}
	if len(seen) < 199 {
		t.Fatalf("200 codes gave only %d distinct values", len(seen))
	}
}

func TestParsePairingCodeAcceptsCaseAndDashButNotOtherText(t *testing.T) {
	code, err := devices.NewPairingCode(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{code.String(), strings.ToLower(code.String()), strings.ReplaceAll(code.String(), "-", "")} {
		if _, err := devices.ParsePairingCode(text); err != nil {
			t.Fatalf("ParsePairingCode(%q): %v", text, err)
		}
	}
	for _, text := range []string{"", "ABCDE", "ABCDEFGHJK0", "ABCDEFGHJKLMN"} {
		if _, err := devices.ParsePairingCode(text); !errors.Is(err, devices.ErrMalformedCode) {
			t.Fatalf("ParsePairingCode(%q) = %v, want malformed", text, err)
		}
	}
}

func TestOnlyOneWindowIsOpenAtATime(t *testing.T) {
	var registry devices.Registry
	openWindow(t, &registry, epochInstant())
	if _, err := registry.OpenWindow(epochInstant().Add(time.Minute), "other", authz.RoleApprover, rand.Reader); !errors.Is(err, devices.ErrPairingOpen) {
		t.Fatalf("second window = %v, want ErrPairingOpen", err)
	}
}

func TestAWindowOpensAgainOnceTheFirstHasExpired(t *testing.T) {
	var registry devices.Registry
	openWindow(t, &registry, epochInstant())
	if _, err := registry.OpenWindow(epochInstant().Add(11*time.Minute), "other", authz.RoleApprover, rand.Reader); err != nil {
		t.Fatalf("a window after the first expired was refused: %v", err)
	}
}

func TestAWindowRefusesARoleThatIsNotGrantable(t *testing.T) {
	var registry devices.Registry
	if _, err := registry.OpenWindow(epochInstant(), "phone", authz.RoleAgent, rand.Reader); !errors.Is(err, devices.ErrUngrantableRole) {
		t.Fatalf("agent window = %v, want ErrUngrantableRole", err)
	}
}

func TestAnExpiredCodeIsRefusedAndClosesTheWindow(t *testing.T) {
	var registry devices.Registry
	code := openWindow(t, &registry, epochInstant())
	csr, _ := requestFor(t, elliptic.P256())
	if err := registry.Redeem(epochInstant().Add(10*time.Minute), code, csr); !errors.Is(err, devices.ErrPairingExpired) {
		t.Fatalf("redeem after ten minutes = %v, want ErrPairingExpired", err)
	}
	if err := registry.Redeem(epochInstant().Add(time.Minute), code, csr); !errors.Is(err, devices.ErrNoPairingWindow) {
		t.Fatalf("the window must be closed after an expired code: %v", err)
	}
}

func TestACodeRedeemedAgainIsRefused(t *testing.T) {
	var registry devices.Registry
	code := openWindow(t, &registry, epochInstant())
	csr, _ := requestFor(t, elliptic.P256())
	if err := registry.Redeem(epochInstant(), code, csr); err != nil {
		t.Fatalf("first redeem: %v", err)
	}
	other, _ := requestFor(t, elliptic.P256())
	if err := registry.Redeem(epochInstant().Add(time.Minute), code, other); !errors.Is(err, devices.ErrPairingAlreadyRedeemed) {
		t.Fatalf("replayed code = %v, want ErrPairingAlreadyRedeemed", err)
	}
}

func TestFourWrongCodesLeaveTheWindowOpenForTheRightOne(t *testing.T) {
	var registry devices.Registry
	code := openWindow(t, &registry, epochInstant())
	csr, _ := requestFor(t, elliptic.P256())
	wrong := wrongCodeFor(t, code)
	for range 4 {
		if err := registry.Redeem(epochInstant(), wrong, csr); !errors.Is(err, devices.ErrWrongCode) {
			t.Fatalf("wrong code = %v, want ErrWrongCode", err)
		}
	}
	if err := registry.Redeem(epochInstant(), code, csr); err != nil {
		t.Fatalf("right code after four wrong ones was refused: %v", err)
	}
}

func TestTheFifthWrongCodeClosesTheWindow(t *testing.T) {
	var registry devices.Registry
	code := openWindow(t, &registry, epochInstant())
	csr, _ := requestFor(t, elliptic.P256())
	wrong := wrongCodeFor(t, code)
	for range 4 {
		if err := registry.Redeem(epochInstant(), wrong, csr); !errors.Is(err, devices.ErrWrongCode) {
			t.Fatalf("wrong code = %v, want ErrWrongCode", err)
		}
	}
	if err := registry.Redeem(epochInstant(), wrong, csr); !errors.Is(err, devices.ErrTooManyAttempts) {
		t.Fatalf("fifth wrong code = %v, want ErrTooManyAttempts", err)
	}
	if err := registry.Redeem(epochInstant(), code, csr); !errors.Is(err, devices.ErrNoPairingWindow) {
		t.Fatalf("the right code after the lock = %v, want ErrNoPairingWindow", err)
	}
}

func TestRedeemWaitsForApprovalAndSignsNothing(t *testing.T) {
	var registry devices.Registry
	code := openWindow(t, &registry, epochInstant())
	csr, _ := requestFor(t, elliptic.P256())
	if err := registry.Redeem(epochInstant(), code, csr); err != nil {
		t.Fatal(err)
	}
	if len(registry.Devices) != 0 {
		t.Fatalf("redeem registered %d devices, want none before approval", len(registry.Devices))
	}
	if registry.Pairing == nil || registry.Pairing.Pending == nil {
		t.Fatal("redeem must leave the request pending for an operator")
	}
	if registry.Pairing.Pending.Fingerprint != devices.ShortFingerprint(csr.RawSubjectPublicKeyInfo) {
		t.Fatal("the pending request must show the short fingerprint of the device key")
	}
}

func TestRedeemRefusesARequestWhoseSignatureDoesNotCheck(t *testing.T) {
	var registry devices.Registry
	code := openWindow(t, &registry, epochInstant())
	csr, _ := requestFor(t, elliptic.P256())
	tampered, err := x509.ParseCertificateRequest(tamperLastByte(csr.Raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Redeem(epochInstant(), code, tampered); !errors.Is(err, devices.ErrBadRequest) {
		t.Fatalf("tampered request = %v, want ErrBadRequest", err)
	}
}

func TestRedeemRefusesAKeyThatIsNotP256(t *testing.T) {
	var registry devices.Registry
	code := openWindow(t, &registry, epochInstant())
	csr, _ := requestFor(t, elliptic.P384())
	if err := registry.Redeem(epochInstant(), code, csr); !errors.Is(err, devices.ErrUnsupportedKey) {
		t.Fatalf("P-384 request = %v, want ErrUnsupportedKey", err)
	}
}

func TestApproveWithoutARedeemedCodeIsRefused(t *testing.T) {
	var registry devices.Registry
	code := openWindow(t, &registry, epochInstant())
	authority := newAuthority(t, epochInstant())
	if _, err := registry.Approve(epochInstant(), code, rand.Reader, signerFor(authority)); !errors.Is(err, devices.ErrNothingPending) {
		t.Fatalf("approve with nothing pending = %v, want ErrNothingPending", err)
	}
}

func TestApproveRegistersTheDeviceWithItsCertificate(t *testing.T) {
	var registry devices.Registry
	code := openWindow(t, &registry, epochInstant())
	csr, _ := requestFor(t, elliptic.P256())
	if err := registry.Redeem(epochInstant(), code, csr); err != nil {
		t.Fatal(err)
	}
	authority := newAuthority(t, epochInstant())
	enrolment, err := registry.Approve(epochInstant().Add(time.Minute), reparse(t, code), rand.Reader, signerFor(authority))
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if enrolment.Role != authz.RoleApprover {
		t.Fatalf("approved role = %q, want approver", enrolment.Role)
	}
	device, found := registry.Find(enrolment.Device.ID)
	if !found {
		t.Fatal("approved device is not in the registry")
	}
	if device.Serial != enrolment.Certificate.SerialNumber.Text(16) {
		t.Fatal("the record must carry the serial of the certificate it issued")
	}
	if registry.Pairing != nil {
		t.Fatal("approval must close the window")
	}
	verified, err := authority.VerifyClient(enrolment.Certificate, epochInstant().Add(time.Minute))
	if err != nil {
		t.Fatalf("the issued certificate does not verify: %v", err)
	}
	principal, err := registry.Authenticate(verified, epochInstant().Add(time.Minute))
	if err != nil {
		t.Fatalf("the device cannot authenticate: %v", err)
	}
	if principal.ID != "device:"+enrolment.Device.ID.String() {
		t.Fatalf("principal = %q, want the device principal", principal.ID)
	}
}

func TestAFailedSigningKeepsTheRequestForAnotherTry(t *testing.T) {
	var registry devices.Registry
	code := openWindow(t, &registry, epochInstant())
	csr, _ := requestFor(t, elliptic.P256())
	if err := registry.Redeem(epochInstant(), code, csr); err != nil {
		t.Fatal(err)
	}
	failing := func(*x509.CertificateRequest, devices.DeviceID, time.Time) (*x509.Certificate, error) {
		return nil, errors.New("disk full")
	}
	if _, err := registry.Approve(epochInstant(), code, rand.Reader, failing); err == nil {
		t.Fatal("a failed signature must fail the approval")
	}
	if registry.Pairing == nil || registry.Pairing.Pending == nil {
		t.Fatal("a failed signature must keep the request pending")
	}
	if _, err := registry.Approve(epochInstant(), code, rand.Reader, signerFor(newAuthority(t, epochInstant()))); err != nil {
		t.Fatalf("approval after the failure: %v", err)
	}
}

func TestDenyClosesTheWindowAndGrantsNothing(t *testing.T) {
	var registry devices.Registry
	code := openWindow(t, &registry, epochInstant())
	csr, _ := requestFor(t, elliptic.P256())
	if err := registry.Redeem(epochInstant(), code, csr); err != nil {
		t.Fatal(err)
	}
	if err := registry.Deny(epochInstant(), code); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if len(registry.Devices) != 0 {
		t.Fatal("deny must not register a device")
	}
	if err := registry.Redeem(epochInstant(), code, csr); !errors.Is(err, devices.ErrNoPairingWindow) {
		t.Fatalf("redeem after deny = %v, want ErrNoPairingWindow", err)
	}
}

func TestARevokedDeviceCannotAuthenticate(t *testing.T) {
	registry, authority, enrolment := pairedRegistry(t)
	if err := registry.Revoke(enrolment.Device.ID); err != nil {
		t.Fatal(err)
	}
	verified, err := authority.VerifyClient(enrolment.Certificate, epochInstant())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Authenticate(verified, epochInstant()); !errors.Is(err, devices.ErrDeviceRevoked) {
		t.Fatalf("revoked device = %v, want ErrDeviceRevoked", err)
	}
}

func TestADeviceUnseenForThirtyDaysExpires(t *testing.T) {
	registry, authority, enrolment := pairedRegistry(t)
	registry.Devices[0].LastSeen = epochInstant().Add(-31 * 24 * time.Hour)
	verified, err := authority.VerifyClient(enrolment.Certificate, epochInstant())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Authenticate(verified, epochInstant()); !errors.Is(err, devices.ErrDeviceExpired) {
		t.Fatalf("device unseen for 31 days = %v, want ErrDeviceExpired", err)
	}
}

func TestSeenKeepsADeviceFromExpiring(t *testing.T) {
	registry, authority, enrolment := pairedRegistry(t)
	if err := registry.Seen(enrolment.Device.ID, epochInstant().Add(20*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	at := epochInstant().Add(25 * 24 * time.Hour)
	verified, err := authority.VerifyClient(enrolment.Certificate, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Authenticate(verified, at); err != nil {
		t.Fatalf("a device seen five days ago was refused: %v", err)
	}
}

func TestAnExpiredClientCertificateIsRefused(t *testing.T) {
	registry, authority, enrolment := pairedRegistry(t)
	at := enrolment.Certificate.NotAfter.Add(time.Second)
	if err := registry.Seen(enrolment.Device.ID, at); err != nil {
		t.Fatal(err)
	}
	verified, err := authority.VerifyClient(enrolment.Certificate, enrolment.Certificate.NotAfter)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Authenticate(verified, at); !errors.Is(err, devices.ErrCertificateExpired) {
		t.Fatalf("expired certificate = %v, want ErrCertificateExpired", err)
	}
}

func TestACertificateWhoseSerialNoLongerMatchesIsRefused(t *testing.T) {
	registry, authority, enrolment := pairedRegistry(t)
	device := registry.Devices[0]
	device.Serial = "1"
	registry.Devices[0] = device
	verified, err := authority.VerifyClient(enrolment.Certificate, epochInstant())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Authenticate(verified, epochInstant()); !errors.Is(err, devices.ErrCertificateMismatch) {
		t.Fatalf("serial mismatch = %v, want ErrCertificateMismatch", err)
	}
}

func TestAnUnknownDeviceIsRefused(t *testing.T) {
	_, authority, enrolment := pairedRegistry(t)
	var empty devices.Registry
	verified, err := authority.VerifyClient(enrolment.Certificate, epochInstant())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := empty.Authenticate(verified, epochInstant()); !errors.Is(err, devices.ErrUnknownDevice) {
		t.Fatalf("unknown device = %v, want ErrUnknownDevice", err)
	}
}

func TestAClientNobodyVerifiedNeverBecomesADevicePrincipal(t *testing.T) {
	registry, _, _ := pairedRegistry(t)
	if _, err := registry.Authenticate(devices.VerifiedClient{}, epochInstant()); !errors.Is(err, devices.ErrUntrustedCertificate) {
		t.Fatalf("unverified client = %v, want ErrUntrustedCertificate", err)
	}
}

func TestRevokeOfAnUnknownDeviceIsRefused(t *testing.T) {
	var registry devices.Registry
	id, err := devices.NewDeviceID(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Revoke(id); !errors.Is(err, devices.ErrUnknownDevice) {
		t.Fatalf("revoke unknown = %v, want ErrUnknownDevice", err)
	}
}

func TestDeviceStatusNamesItsState(t *testing.T) {
	device := devices.Device{LastSeen: epochInstant()}
	if got := device.StatusAt(epochInstant()).String(); got != "active" {
		t.Fatalf("fresh device = %q, want active", got)
	}
	if got := device.StatusAt(epochInstant().Add(31 * 24 * time.Hour)).String(); got != "expired" {
		t.Fatalf("stale device = %q, want expired", got)
	}
	device.Revoked = true
	if got := device.StatusAt(epochInstant()).String(); got != "revoked" {
		t.Fatalf("revoked device = %q, want revoked", got)
	}
}

func pairedRegistry(t *testing.T) (*devices.Registry, *devices.Authority, devices.Enrolment) {
	t.Helper()
	var registry devices.Registry
	code := openWindow(t, &registry, epochInstant())
	csr, _ := requestFor(t, elliptic.P256())
	if err := registry.Redeem(epochInstant(), code, csr); err != nil {
		t.Fatal(err)
	}
	authority := newAuthority(t, epochInstant())
	enrolment, err := registry.Approve(epochInstant(), code, rand.Reader, signerFor(authority))
	if err != nil {
		t.Fatal(err)
	}
	return &registry, authority, enrolment
}

func wrongCodeFor(t *testing.T, code devices.PairingCode) devices.PairingCode {
	t.Helper()
	for range 64 {
		other, err := devices.NewPairingCode(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if other.String() != code.String() {
			return other
		}
	}
	t.Fatal("could not draw a code different from the one in use")
	return devices.PairingCode{}
}

func tamperLastByte(der []byte) []byte {
	out := append([]byte(nil), der...)
	out[len(out)-1] ^= 0x01
	return out
}
