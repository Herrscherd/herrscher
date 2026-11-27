package devices_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/authz"
	"github.com/Herrscherd/herrscher/core/internal/devices"
)

func loadAuthority(stateDir string, now time.Time, rnd io.Reader, enrolled bool) (*devices.Authority, error) {
	gate := &devices.AuthorityGate{}
	return gate.LoadOrCreate(context.Background(), stateDir, now, rnd, enrolled)
}

func epochInstant() time.Time {
	return time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
}

func requestFor(t *testing.T, curve elliptic.Curve) (*x509.CertificateRequest, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "phone"}}, key)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil {
		t.Fatal(err)
	}
	return csr, key
}

func openWindow(t *testing.T, registry *devices.Registry, now time.Time) devices.PairingCode {
	t.Helper()
	code, err := registry.OpenWindow(now, "phone", authz.RoleApprover, rand.Reader)
	if err != nil {
		t.Fatalf("open window: %v", err)
	}
	return code
}

func reparse(t *testing.T, code devices.PairingCode) devices.PairingCode {
	t.Helper()
	again, err := devices.ParsePairingCode(code.String())
	if err != nil {
		t.Fatal(err)
	}
	return again
}

func newAuthority(t *testing.T, now time.Time) *devices.Authority {
	t.Helper()
	authority, err := loadAuthority(t.TempDir(), now, rand.Reader, false)
	if err != nil {
		t.Fatal(err)
	}
	return authority
}

func signerFor(authority *devices.Authority) devices.Signer {
	return func(csr *x509.CertificateRequest, id devices.DeviceID, now time.Time) (*x509.Certificate, error) {
		return authority.SignClient(csr, id, now, rand.Reader)
	}
}

func ecdsaKey(t *testing.T) (*ecdsa.PrivateKey, error) {
	t.Helper()
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}
