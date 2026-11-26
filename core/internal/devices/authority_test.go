package devices_test

import (
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/devices"
)

func TestTheAuthorityIsKeptAcrossLoadsAndItsFilesStayPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "devices")
	first, err := devices.LoadOrCreateAuthority(dir, epochInstant(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csr, _ := requestFor(t, elliptic.P256())
	cert, err := first.SignClient(csr, mustDeviceID(t), epochInstant(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	second, err := devices.LoadOrCreateAuthority(dir, epochInstant().Add(time.Hour), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.VerifyClient(cert, epochInstant().Add(time.Hour)); err != nil {
		t.Fatalf("a reload must keep the authority that signed the certificate: %v", err)
	}
	assertMode(t, dir, 0o700)
	for _, name := range []string{"authority-key.pem", "authority-cert.pem", "server-key.pem", "server-cert.pem"} {
		assertMode(t, filepath.Join(dir, name), 0o600)
	}
}

func TestAnAuthorityKeyWithoutItsCertificateIsRefusedNotReplaced(t *testing.T) {
	dir := t.TempDir()
	if _, err := devices.LoadOrCreateAuthority(dir, epochInstant(), rand.Reader); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "authority-cert.pem")); err != nil {
		t.Fatal(err)
	}
	if _, err := devices.LoadOrCreateAuthority(dir, epochInstant(), rand.Reader); !errors.Is(err, devices.ErrAuthorityFiles) {
		t.Fatalf("partial authority = %v, want ErrAuthorityFiles", err)
	}
}

func TestTheServerCertificateIsSignedByTheAuthority(t *testing.T) {
	dir := t.TempDir()
	authority, err := devices.LoadOrCreateAuthority(dir, epochInstant(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caPEM, err := os.ReadFile(filepath.Join(dir, "authority-cert.pem"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(caPEM)
	ca, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	server := authority.ServerCertificate().Leaf
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	if _, err := server.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: epochInstant(), DNSName: "localhost", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Fatalf("the server certificate does not chain to the authority: %v", err)
	}
}

func TestASignedClientCertificateCarriesTheDeviceAndThirtyDays(t *testing.T) {
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
	dir := t.TempDir()
	if _, err := devices.LoadOrCreateAuthority(dir, epochInstant(), rand.Reader); err != nil {
		t.Fatal(err)
	}
	if _, err := devices.LoadOrCreateAuthority(dir, epochInstant().Add(6*365*24*time.Hour), rand.Reader); !errors.Is(err, devices.ErrAuthorityExpired) {
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
