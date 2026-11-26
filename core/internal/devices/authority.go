package devices

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	clientCertValidity = 30 * 24 * time.Hour
	authorityValidity  = 5 * 365 * 24 * time.Hour
	serverCertValidity = 365 * 24 * time.Hour
	privateDirMode     = 0o700
	privateFileMode    = 0o600
	authorityCertFile  = "authority-cert.pem"
	authorityKeyFile   = "authority-key.pem"
	serverCertFile     = "server-cert.pem"
	serverKeyFile      = "server-key.pem"
	serialBytes        = 16
)

// Authority is the daemon's own certificate authority. It signs the client certificates of paired
// devices, and it holds the server certificate the daemon presents to them.
type Authority struct {
	caCert    *x509.Certificate
	caKey     *ecdsa.PrivateKey
	server    *x509.Certificate
	serverKey *ecdsa.PrivateKey
}

// VerifiedClient is a client certificate the authority has checked. Only VerifyClient makes one, so a
// device principal cannot come from a certificate nobody checked.
type VerifiedClient struct {
	cert *x509.Certificate
}

// LoadOrCreateAuthority reads the authority kept in dir, and creates it and its server certificate the
// first time. A key file without its certificate, or the reverse, is refused rather than replaced, since
// replacing the authority would silently unpair every device.
func LoadOrCreateAuthority(dir string, now time.Time, rnd io.Reader) (*Authority, error) {
	if err := os.MkdirAll(dir, privateDirMode); err != nil {
		return nil, fmt.Errorf("cannot create the device directory: %w", err)
	}
	if err := os.Chmod(dir, privateDirMode); err != nil {
		return nil, fmt.Errorf("cannot restrict the device directory: %w", err)
	}
	caCert, caKey, err := loadOrCreateAuthorityCertificate(dir, now, rnd)
	if err != nil {
		return nil, err
	}
	if now.After(caCert.NotAfter) {
		return nil, ErrAuthorityExpired
	}
	server, serverKey, err := loadOrIssueServerCertificate(dir, caCert, caKey, now, rnd)
	if err != nil {
		return nil, err
	}
	return &Authority{caCert: caCert, caKey: caKey, server: server, serverKey: serverKey}, nil
}

// SignClient certifies the key of a device's request for the identifier id, for thirty days from now. The
// key stays on the device: the request carries only its public half.
func (a *Authority) SignClient(csr *x509.CertificateRequest, deviceID DeviceID, now time.Time, rnd io.Reader) (*x509.Certificate, error) {
	if err := checkRequest(csr); err != nil {
		return nil, err
	}
	serial, err := randomSerial(rnd)
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: deviceID.String()},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(clientCertValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rnd, template, a.caCert, csr.PublicKey, a.caKey)
	if err != nil {
		return nil, fmt.Errorf("cannot sign the device certificate: %w", err)
	}
	return x509.ParseCertificate(der)
}

// VerifyClient checks that a client certificate chains to this authority for client authentication at now.
func (a *Authority) VerifyClient(cert *x509.Certificate, now time.Time) (VerifiedClient, error) {
	if cert == nil {
		return VerifiedClient{}, ErrUntrustedCertificate
	}
	roots := x509.NewCertPool()
	roots.AddCert(a.caCert)
	_, err := cert.Verify(x509.VerifyOptions{
		Roots:       roots,
		CurrentTime: now,
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	if err != nil {
		return VerifiedClient{}, fmt.Errorf("%w: %w", ErrUntrustedCertificate, err)
	}
	return VerifiedClient{cert: cert}, nil
}

// ServerCertificate is the certificate the daemon presents on a network listener. Clients pin it, and
// the authority signed it.
func (a *Authority) ServerCertificate() tls.Certificate {
	return tls.Certificate{
		Certificate: [][]byte{a.server.Raw},
		PrivateKey:  a.serverKey,
		Leaf:        a.server,
	}
}

func loadOrCreateAuthorityCertificate(dir string, now time.Time, rnd io.Reader) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPath := filepath.Join(dir, authorityCertFile)
	keyPath := filepath.Join(dir, authorityKeyFile)
	present, err := pairPresence(certPath, keyPath)
	if err != nil {
		return nil, nil, err
	}
	if present {
		return readPair(certPath, keyPath)
	}
	return createAuthorityCertificate(now, rnd, certPath, keyPath)
}

func loadOrIssueServerCertificate(dir string, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, now time.Time, rnd io.Reader) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPath := filepath.Join(dir, serverCertFile)
	keyPath := filepath.Join(dir, serverKeyFile)
	present, err := pairPresence(certPath, keyPath)
	if err != nil {
		return nil, nil, err
	}
	if present {
		server, key, readErr := readPair(certPath, keyPath)
		if readErr == nil && now.Before(server.NotAfter) {
			return server, key, nil
		}
	}
	return issueServerCertificate(caCert, caKey, now, rnd, certPath, keyPath)
}

func pairPresence(certPath, keyPath string) (bool, error) {
	_, certErr := os.Stat(certPath)
	_, keyErr := os.Stat(keyPath)
	certMissing := errors.Is(certErr, fs.ErrNotExist)
	keyMissing := errors.Is(keyErr, fs.ErrNotExist)
	switch {
	case certMissing && keyMissing:
		return false, nil
	case certErr == nil && keyErr == nil:
		return true, nil
	}
	return false, ErrAuthorityFiles
}

func readPair(certPath, keyPath string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPEM, err := os.ReadFile(filepath.Clean(certPath))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrAuthorityFiles, err)
	}
	keyPEM, err := os.ReadFile(filepath.Clean(keyPath))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrAuthorityFiles, err)
	}
	cert, err := parseCertificatePEM(certPEM)
	if err != nil {
		return nil, nil, err
	}
	key, err := parseKeyPEM(keyPEM)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func createAuthorityCertificate(now time.Time, rnd io.Reader, certPath, keyPath string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rnd)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot generate the authority key: %w", err)
	}
	serial, err := randomSerial(rnd)
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "herrscher device authority"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(authorityValidity),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rnd, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot self-sign the authority: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	if err := writePair(certPath, keyPath, der, key); err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func issueServerCertificate(caCert *x509.Certificate, caKey *ecdsa.PrivateKey, now time.Time, rnd io.Reader, certPath, keyPath string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rnd)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot generate the server key: %w", err)
	}
	serial, err := randomSerial(rnd)
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "herrscher daemon"},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(serverCertValidity),
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rnd, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot sign the server certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	if err := writePair(certPath, keyPath, der, key); err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func writePair(certPath, keyPath string, der []byte, key *ecdsa.PrivateKey) error {
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("cannot encode a key: %w", err)
	}
	if err := writePrivate(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})); err != nil {
		return err
	}
	return writePrivate(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func writePrivate(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, privateFileMode); err != nil {
		return fmt.Errorf("cannot write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("cannot move %s into place: %w", filepath.Base(path), err)
	}
	return nil
}

func parseCertificatePEM(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, ErrAuthorityFiles
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAuthorityFiles, err)
	}
	return cert, nil
}

func parseKeyPEM(data []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "EC PRIVATE KEY" {
		return nil, ErrAuthorityFiles
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAuthorityFiles, err)
	}
	return key, nil
}

func randomSerial(rnd io.Reader) (*big.Int, error) {
	raw := make([]byte, serialBytes)
	if _, err := io.ReadFull(rnd, raw); err != nil {
		return nil, fmt.Errorf("cannot draw a serial number: %w", err)
	}
	raw[0] &= 0x7f
	serial := new(big.Int).SetBytes(raw)
	if serial.Sign() == 0 {
		serial.SetInt64(1)
	}
	return serial, nil
}
