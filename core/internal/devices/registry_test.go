package devices_test

import (
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
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
	if _, err := registry.Approve(epochInstant(), code, devices.Fingerprint{}, rand.Reader, signerFor(authority)); !errors.Is(err, devices.ErrNothingPending) {
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
	enrolment, err := registry.Approve(epochInstant().Add(time.Minute), reparse(t, code), fingerprintFor(t, csr), rand.Reader, signerFor(authority))
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
	if _, err := registry.Approve(epochInstant(), code, fingerprintFor(t, csr), rand.Reader, failing); err == nil {
		t.Fatal("a failed signature must fail the approval")
	}
	if registry.Pairing == nil || registry.Pairing.Pending == nil {
		t.Fatal("a failed signature must keep the request pending")
	}
	if _, err := registry.Approve(epochInstant(), code, fingerprintFor(t, csr), rand.Reader, signerFor(newAuthority(t, epochInstant()))); err != nil {
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
	if err := registry.Revoke(epochInstant(), enrolment.Device.ID); err != nil {
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
	if err := registry.Revoke(epochInstant(), id); !errors.Is(err, devices.ErrUnknownDevice) {
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
	enrolment, err := registry.Approve(epochInstant(), code, fingerprintFor(t, csr), rand.Reader, signerFor(authority))
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

func fingerprintFor(t *testing.T, csr *x509.CertificateRequest) devices.Fingerprint {
	t.Helper()
	fingerprint, err := devices.ParseFingerprint(devices.ShortFingerprint(csr.RawSubjectPublicKeyInfo))
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint
}

func TestPreviewShowsTheRequestAndSignsNothing(t *testing.T) {
	var registry devices.Registry
	code := openWindow(t, &registry, epochInstant())
	csr, _ := requestFor(t, elliptic.P256())
	if err := registry.Redeem(epochInstant(), code, csr); err != nil {
		t.Fatal(err)
	}
	preview, err := registry.Preview(epochInstant(), code)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Name != "phone" || preview.Role != authz.RoleApprover || preview.Fingerprint != devices.ShortFingerprint(csr.RawSubjectPublicKeyInfo) {
		t.Fatalf("preview = %+v", preview)
	}
	if len(registry.Devices) != 0 || registry.Pairing == nil || registry.Pairing.Pending == nil {
		t.Fatal("a preview must leave the request pending and register nothing")
	}
}

func TestAWrongFingerprintSignsNothingAndCountsAsAnAttempt(t *testing.T) {
	var registry devices.Registry
	code := openWindow(t, &registry, epochInstant())
	csr, _ := requestFor(t, elliptic.P256())
	if err := registry.Redeem(epochInstant(), code, csr); err != nil {
		t.Fatal(err)
	}
	other, _ := requestFor(t, elliptic.P256())
	wrong := fingerprintFor(t, other)
	signed := false
	sign := func(*x509.CertificateRequest, devices.DeviceID, time.Time) (*x509.Certificate, error) {
		signed = true
		return nil, errors.New("must not sign")
	}
	if _, err := registry.Approve(epochInstant(), code, wrong, rand.Reader, sign); !errors.Is(err, devices.ErrFingerprintMismatch) {
		t.Fatalf("wrong fingerprint = %v, want ErrFingerprintMismatch", err)
	}
	if signed {
		t.Fatal("a wrong fingerprint must never reach the signer")
	}
	if registry.Pairing == nil || registry.Pairing.Failures != 1 {
		t.Fatal("a wrong fingerprint must count as one attempt and keep the window")
	}
}

func TestFiveWrongFingerprintsCloseTheWindow(t *testing.T) {
	var registry devices.Registry
	code := openWindow(t, &registry, epochInstant())
	csr, _ := requestFor(t, elliptic.P256())
	if err := registry.Redeem(epochInstant(), code, csr); err != nil {
		t.Fatal(err)
	}
	other, _ := requestFor(t, elliptic.P256())
	wrong := fingerprintFor(t, other)
	noSign := func(*x509.CertificateRequest, devices.DeviceID, time.Time) (*x509.Certificate, error) {
		return nil, errors.New("must not sign")
	}
	for range 4 {
		if _, err := registry.Approve(epochInstant(), code, wrong, rand.Reader, noSign); !errors.Is(err, devices.ErrFingerprintMismatch) {
			t.Fatalf("wrong fingerprint = %v, want ErrFingerprintMismatch", err)
		}
	}
	if _, err := registry.Approve(epochInstant(), code, wrong, rand.Reader, noSign); !errors.Is(err, devices.ErrTooManyAttempts) {
		t.Fatalf("fifth wrong fingerprint = %v, want ErrTooManyAttempts", err)
	}
	if _, err := registry.Preview(epochInstant(), code); !errors.Is(err, devices.ErrNoPairingWindow) {
		t.Fatalf("the window after five wrong fingerprints = %v, want ErrNoPairingWindow", err)
	}
}

func TestTheRightFingerprintSignsAndTheTypedFormIsNormalised(t *testing.T) {
	var registry devices.Registry
	code := openWindow(t, &registry, epochInstant())
	csr, _ := requestFor(t, elliptic.P256())
	if err := registry.Redeem(epochInstant(), code, csr); err != nil {
		t.Fatal(err)
	}
	typed := strings.ToLower(strings.ReplaceAll(devices.ShortFingerprint(csr.RawSubjectPublicKeyInfo), "-", " "))
	fingerprint, err := devices.ParseFingerprint(typed)
	if err != nil {
		t.Fatalf("ParseFingerprint(%q): %v", typed, err)
	}
	if _, err := registry.Approve(epochInstant(), code, fingerprint, rand.Reader, signerFor(newAuthority(t, epochInstant()))); err != nil {
		t.Fatalf("the right fingerprint in another case and separator was refused: %v", err)
	}
	if len(registry.Devices) != 1 {
		t.Fatal("the right fingerprint must register the device")
	}
}

func TestAFingerprintIsSixteenSymbolsOfThePairingAlphabet(t *testing.T) {
	if got := len(strings.ReplaceAll(devices.ShortFingerprint(make([]byte, 32)), "-", "")); got != 16 {
		t.Fatalf("short fingerprint has %d symbols, want 16 (80 bits)", got)
	}
	for _, text := range []string{"", "ABCD", "ABCDEFGHIJKLMNO0", "ABCDEFGHIJ"} {
		if _, err := devices.ParseFingerprint(text); !errors.Is(err, devices.ErrMalformedFingerprint) {
			t.Fatalf("ParseFingerprint(%q) = %v, want ErrMalformedFingerprint", text, err)
		}
	}
}

func TestACertificateRequestSignedWithANonSHA2AlgorithmIsRefusedAtRedeem(t *testing.T) {
	var registry devices.Registry
	code := openWindow(t, &registry, epochInstant())
	key, err := ecdsaKey(t)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:            pkix.Name{CommonName: "phone"},
		SignatureAlgorithm: x509.ECDSAWithSHA1,
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Redeem(epochInstant(), code, csr); !errors.Is(err, devices.ErrUnsupportedSignature) {
		t.Fatalf("SHA-1 request at redeem = %v, want ErrUnsupportedSignature", err)
	}
}

func TestADeviceWhoseRetentionIsOverIsPurgedAndOthersStay(t *testing.T) {
	now := epochInstant()
	active, expiring, expired, revoked, lingering := mustID(t), mustID(t), mustID(t), mustID(t), mustID(t)
	registry := devices.Registry{Devices: []devices.Device{
		{ID: active, LastSeen: now},
		{ID: lingering, LastSeen: now.Add(-59 * 24 * time.Hour)},
		{ID: expired, LastSeen: now.Add(-61 * 24 * time.Hour)},
		{ID: revoked, Revoked: true, RevokedAt: now.Add(-31 * 24 * time.Hour), LastSeen: now},
		{ID: expiring, Revoked: true, RevokedAt: now.Add(-29 * 24 * time.Hour), LastSeen: now},
	}}
	purged := registry.Purge(now)
	if len(purged) != 2 || !containsID(purged, expired) || !containsID(purged, revoked) {
		t.Fatalf("purged %v, want the expired and the long-revoked devices only", purged)
	}
	if _, found := registry.Find(active); !found {
		t.Fatal("an active device must stay")
	}
	if _, found := registry.Find(lingering); !found {
		t.Fatal("a device expired less than thirty days ago must stay")
	}
	if _, found := registry.Find(expiring); !found {
		t.Fatal("a device revoked less than thirty days ago must stay")
	}
}

func TestRevokingKeepsTheFirstRevocationTime(t *testing.T) {
	var registry devices.Registry
	id := mustID(t)
	registry.Devices = []devices.Device{{ID: id, LastSeen: epochInstant()}}
	first := epochInstant()
	if err := registry.Revoke(first, id); err != nil {
		t.Fatal(err)
	}
	if err := registry.Revoke(first.Add(time.Hour), id); err != nil {
		t.Fatal(err)
	}
	device, _ := registry.Find(id)
	if !device.RevokedAt.Equal(first) {
		t.Fatalf("second revocation moved the time to %v", device.RevokedAt)
	}
}

func TestACertificateRequestAboveTheBoundIsRefused(t *testing.T) {
	var registry devices.Registry
	code := openWindow(t, &registry, epochInstant())
	key, err := ecdsaKey(t)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: strings.Repeat("x", 8192)},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Redeem(epochInstant(), code, csr); !errors.Is(err, devices.ErrRequestTooLarge) {
		t.Fatalf("oversized request = %v, want ErrRequestTooLarge", err)
	}
}

func mustID(t *testing.T) devices.DeviceID {
	t.Helper()
	id, err := devices.NewDeviceID(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func containsID(ids []devices.DeviceID, want devices.DeviceID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
