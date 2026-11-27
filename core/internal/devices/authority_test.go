package devices_test

import (
	"context"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/devices"
)

func TestTheAuthorityIsKeptAcrossLoadsAndItsSecretsStayPrivate(t *testing.T) {
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o750); err != nil {
		t.Fatal(err)
	}
	first, err := loadAuthority(stateDir, epochInstant(), rand.Reader, false)
	if err != nil {
		t.Fatal(err)
	}
	csr, _ := requestFor(t, elliptic.P256())
	cert, err := first.SignClient(csr, mustDeviceID(t), epochInstant(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadAuthority(stateDir, epochInstant().Add(time.Hour), rand.Reader, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.VerifyClient(cert, epochInstant().Add(time.Hour)); err != nil {
		t.Fatalf("a reload must keep the authority that signed the certificate: %v", err)
	}
	assertMode(t, filepath.Join(stateDir, "devices"), 0o700)
	assertMode(t, stateDir, 0o750)
	for _, name := range []string{"authority-key.pem", "authority-cert.pem", "server-key.pem", "server-cert.pem"} {
		assertMode(t, filepath.Join(stateDir, "devices", name), 0o600)
	}
}

func TestAPartialAuthorityIsRebuiltWhenNoDeviceWasEnrolled(t *testing.T) {
	stateDir := t.TempDir()
	if _, err := loadAuthority(stateDir, epochInstant(), rand.Reader, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(authorityCertPath(stateDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAuthority(stateDir, epochInstant(), rand.Reader, false); err != nil {
		t.Fatalf("a partial pair with no device enrolled must be rebuilt: %v", err)
	}
	if _, err := os.Stat(authorityCertPath(stateDir)); err != nil {
		t.Fatal("the rebuilt pair must be complete")
	}
}

func TestAPartialAuthorityIsRefusedOnceADeviceWasEnrolled(t *testing.T) {
	stateDir := t.TempDir()
	if _, err := loadAuthority(stateDir, epochInstant(), rand.Reader, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(authorityCertPath(stateDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAuthority(stateDir, epochInstant(), rand.Reader, true); !errors.Is(err, devices.ErrAuthorityFiles) {
		t.Fatalf("partial pair with devices enrolled = %v, want ErrAuthorityFiles: replacing it would unpair them", err)
	}
}

func TestAnAuthorityKeyNeverFollowsASymlinkPlantedAtAGuessableName(t *testing.T) {
	stateDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "devices"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(stateDir, "devices", "authority-key.pem.tmp")); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAuthority(stateDir, epochInstant(), rand.Reader, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "untouched" {
		t.Fatal("a planted symlink was followed and its target overwritten")
	}
}

func TestTheServerCertificateIsSignedByTheAuthority(t *testing.T) {
	stateDir := t.TempDir()
	authority, err := loadAuthority(stateDir, epochInstant(), rand.Reader, false)
	if err != nil {
		t.Fatal(err)
	}
	caPEM, err := os.ReadFile(authorityCertPath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(caPEM)
	ca, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	server := authority.ServerCertificate().Leaf
	if _, err := server.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: epochInstant(), DNSName: "localhost", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Fatalf("the server certificate does not chain to the authority: %v", err)
	}
}

func TestASignedClientCertificateCarriesTheDeviceThirtyDaysAndIsNoAuthority(t *testing.T) {
	authority := newAuthority(t, epochInstant())
	csr, _ := requestFor(t, elliptic.P256())
	id := mustDeviceID(t)
	cert, err := authority.SignClient(csr, id, epochInstant(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != id.String() {
		t.Fatalf("common name = %q, want the device id", cert.Subject.CommonName)
	}
	if span := cert.NotAfter.Sub(epochInstant()); span < 30*24*time.Hour || span > 30*24*time.Hour+time.Hour {
		t.Fatalf("validity = %v, want about thirty days", span)
	}
	if len(cert.ExtKeyUsage) != 1 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Fatalf("ext key usage = %v, want client authentication only", cert.ExtKeyUsage)
	}
	if !cert.BasicConstraintsValid || cert.IsCA {
		t.Fatal("a client certificate must state explicitly that it is not a CA")
	}
}

func TestSignClientRefusesARequestWhoseSignatureDoesNotCheck(t *testing.T) {
	authority := newAuthority(t, epochInstant())
	csr, _ := requestFor(t, elliptic.P256())
	tampered, err := x509.ParseCertificateRequest(tamperLastByte(csr.Raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.SignClient(tampered, mustDeviceID(t), epochInstant(), rand.Reader); !errors.Is(err, devices.ErrBadRequest) {
		t.Fatalf("tampered request = %v, want ErrBadRequest", err)
	}
}

func TestSignClientRefusesARequestSignedWithSHA1(t *testing.T) {
	authority := newAuthority(t, epochInstant())
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
	if _, err := authority.SignClient(csr, mustDeviceID(t), epochInstant(), rand.Reader); !errors.Is(err, devices.ErrUnsupportedSignature) {
		t.Fatalf("SHA-1 request = %v, want ErrUnsupportedSignature", err)
	}
}

func TestVerifyClientRefusesACertificateFromAnotherAuthority(t *testing.T) {
	mine := newAuthority(t, epochInstant())
	other := newAuthority(t, epochInstant())
	csr, _ := requestFor(t, elliptic.P256())
	foreign, err := other.SignClient(csr, mustDeviceID(t), epochInstant(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mine.VerifyClient(foreign, epochInstant()); !errors.Is(err, devices.ErrUntrustedCertificate) {
		t.Fatalf("foreign certificate = %v, want ErrUntrustedCertificate", err)
	}
}

func TestVerifyClientRefusesAnExpiredCertificate(t *testing.T) {
	authority := newAuthority(t, epochInstant())
	csr, _ := requestFor(t, elliptic.P256())
	cert, err := authority.SignClient(csr, mustDeviceID(t), epochInstant(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.VerifyClient(cert, cert.NotAfter.Add(time.Hour)); !errors.Is(err, devices.ErrUntrustedCertificate) {
		t.Fatalf("expired certificate = %v, want ErrUntrustedCertificate", err)
	}
	if _, err := authority.VerifyClient(nil, epochInstant()); !errors.Is(err, devices.ErrUntrustedCertificate) {
		t.Fatalf("nil certificate = %v, want ErrUntrustedCertificate", err)
	}
}

func TestAnAuthorityPastItsValidityIsRefused(t *testing.T) {
	stateDir := t.TempDir()
	if _, err := loadAuthority(stateDir, epochInstant(), rand.Reader, false); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAuthority(stateDir, epochInstant().Add(6*365*24*time.Hour), rand.Reader, false); !errors.Is(err, devices.ErrAuthorityExpired) {
		t.Fatalf("authority five years and more old = %v, want ErrAuthorityExpired", err)
	}
}

func mustDeviceID(t *testing.T) devices.DeviceID {
	t.Helper()
	id, err := devices.NewDeviceID(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func authorityCertPath(stateDir string) string {
	return filepath.Join(stateDir, "devices", "authority-cert.pem")
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %v, want %v", filepath.Base(path), got, want)
	}
}

func TestConcurrentLoadsOfAnAbsentAuthorityAgreeOnOneAuthority(t *testing.T) {
	stateDir := t.TempDir()
	var gate devices.AuthorityGate
	const callers = 8
	results := make(chan *devices.Authority, callers)
	errs := make(chan error, callers)
	for range callers {
		go func() {
			authority, err := gate.LoadOrCreate(context.Background(), stateDir, epochInstant(), rand.Reader, false)
			results <- authority
			errs <- err
		}()
	}
	var authorities []*devices.Authority
	for range callers {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		authorities = append(authorities, <-results)
	}
	csr, _ := requestFor(t, elliptic.P256())
	cert, err := authorities[0].SignClient(csr, mustDeviceID(t), epochInstant(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for idx, authority := range authorities {
		if _, err := authority.VerifyClient(cert, epochInstant()); err != nil {
			t.Fatalf("caller %d holds a different authority: %v", idx, err)
		}
	}
}

func TestAnAbsentAuthorityIsNotRebuiltOnceDevicesArePaired(t *testing.T) {
	stateDir := t.TempDir()
	if _, err := loadAuthority(stateDir, epochInstant(), rand.Reader, true); !errors.Is(err, devices.ErrAuthorityMissing) {
		t.Fatalf("absent authority with devices paired = %v, want ErrAuthorityMissing", err)
	}
	if _, err := os.Stat(authorityCertPath(stateDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("nothing may be rebuilt when devices are paired")
	}
}

func TestTheServerCertificateDeclaresItsCAStatusExplicitly(t *testing.T) {
	authority := newAuthority(t, epochInstant())
	server := authority.ServerCertificate().Leaf
	if !server.BasicConstraintsValid || server.IsCA {
		t.Fatal("the server certificate must state explicitly that it is not a CA")
	}
}

func TestAServerCertificateNotSignedByTheCurrentAuthorityIsReissued(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	if _, err := loadAuthority(first, epochInstant(), rand.Reader, false); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAuthority(second, epochInstant(), rand.Reader, false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"server-cert.pem", "server-key.pem"} {
		data, err := os.ReadFile(filepath.Join(first, "devices", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(second, "devices", name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	authority, err := loadAuthority(second, epochInstant(), rand.Reader, false)
	if err != nil {
		t.Fatal(err)
	}
	caPEM, err := os.ReadFile(authorityCertPath(second))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(caPEM)
	ca, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	if _, err := authority.ServerCertificate().Leaf.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: epochInstant(), DNSName: "localhost", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Fatalf("a server certificate from another authority was kept: %v", err)
	}
}

func TestTwoGatesInOneProcessSerialiseThroughTheLockFile(t *testing.T) {
	stateDir := t.TempDir()
	const callers = 6
	results := make(chan *devices.Authority, callers)
	errs := make(chan error, callers)
	for range callers {
		go func() {
			gate := &devices.AuthorityGate{}
			authority, err := gate.LoadOrCreate(context.Background(), stateDir, epochInstant(), rand.Reader, false)
			results <- authority
			errs <- err
		}()
	}
	var authorities []*devices.Authority
	for range callers {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		authorities = append(authorities, <-results)
	}
	csr, _ := requestFor(t, elliptic.P256())
	cert, err := authorities[0].SignClient(csr, mustDeviceID(t), epochInstant(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for idx, authority := range authorities {
		if _, err := authority.VerifyClient(cert, epochInstant()); err != nil {
			t.Fatalf("gate %d created a different authority: %v", idx, err)
		}
	}
}
