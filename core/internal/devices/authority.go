package devices

import (
	"context"
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
	"sync"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/filelock"
)

const (
	clientCertValidity = 30 * 24 * time.Hour
	authorityValidity  = 5 * 365 * 24 * time.Hour
	serverCertValidity = 365 * 24 * time.Hour
	secretDirName      = "devices"
	privateDirMode     = 0o700
	privateFileMode    = 0o600
	authorityCertFile  = "authority-cert.pem"
	authorityKeyFile   = "authority-key.pem"
	serverCertFile     = "server-cert.pem"
	serverKeyFile      = "server-key.pem"
	serialBytes        = 16
	authorityLockFile  = "authority.lock"
	serialTopBitMask   = 0x7f
	clockSkew          = time.Minute
	pemECKey           = "EC PRIVATE KEY"
	pemCertificate     = "CERTIFICATE"
)

// Authority is the daemon's own certificate authority.
type Authority struct {
	caCert    *x509.Certificate
	caKey     *ecdsa.PrivateKey
	server    *x509.Certificate
	serverKey *ecdsa.PrivateKey
}

// AuthorityGate serialises the creation and loading of the authority across the goroutines of one process, so
// two approvals that race on a missing authority cannot each write a key and a certificate that do not match.
// It is separate from the state lock, so the disk work does not hold the registry.
type AuthorityGate struct {
	mu sync.Mutex
}

// LoadOrCreate loads the authority kept under stateDir, or creates it. It holds the gate's mutex for the
// goroutines of this process and an exclusive lock file for the other processes, a one-off command included.
func (g *AuthorityGate) LoadOrCreate(ctx context.Context, stateDir string, now time.Time, rnd io.Reader, enrolled bool) (*Authority, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	dir := filepath.Join(stateDir, secretDirName)
	if err := os.MkdirAll(dir, privateDirMode); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAuthorityFiles, err)
	}
	release, err := filelock.Acquire(ctx, filepath.Join(dir, authorityLockFile))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAuthorityFiles, err)
	}
	defer release()
	return loadOrCreateAuthority(stateDir, now, rnd, enrolled)
}

// VerifiedClient is a client certificate the authority has checked.
type VerifiedClient struct {
	cert *x509.Certificate
}

type pairState uint8

const (
	pairAbsent pairState = iota + 1
	pairComplete
	pairPartial
)

func loadOrCreateAuthority(stateDir string, now time.Time, rnd io.Reader, enrolled bool) (*Authority, error) {
	dir := filepath.Join(stateDir, secretDirName)
	if err := os.MkdirAll(dir, privateDirMode); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAuthorityFiles, err)
	}
	if err := os.Chmod(dir, privateDirMode); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAuthorityFiles, err)
	}
	caCert, caKey, err := loadOrCreateAuthorityCertificate(dir, now, rnd, enrolled)
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

// SignClient certifies the key of a device's request for the identifier deviceID, for thirty days from now.
func (a *Authority) SignClient(csr *x509.CertificateRequest, deviceID DeviceID, now time.Time, rnd io.Reader) (*x509.Certificate, error) {
	if err := checkRequest(csr); err != nil {
		return nil, err
	}
	serial, err := randomSerial(rnd)
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: deviceID.String()},
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              now.Add(clientCertValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
	}
	der, err := x509.CreateCertificate(rnd, template, a.caCert, csr.PublicKey, a.caKey)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSigning, err)
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

// ServerCertificate is the certificate the daemon presents on a network listener.
func (a *Authority) ServerCertificate() tls.Certificate {
	return tls.Certificate{
		Certificate: [][]byte{a.server.Raw},
		PrivateKey:  a.serverKey,
		Leaf:        a.server,
	}
}

func loadOrCreateAuthorityCertificate(dir string, now time.Time, rnd io.Reader, enrolled bool) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPath := filepath.Join(dir, authorityCertFile)
	keyPath := filepath.Join(dir, authorityKeyFile)
	state, err := pairStateOf(certPath, keyPath)
	if err != nil {
		return nil, nil, err
	}
	switch {
	case state == pairComplete:
		return readPair(certPath, keyPath)
	case state == pairAbsent && enrolled:
		return nil, nil, ErrAuthorityMissing
	case state == pairPartial && enrolled:
		return nil, nil, ErrAuthorityFiles
	}
	return createAuthorityCertificate(now, rnd, certPath, keyPath)
}

func loadOrIssueServerCertificate(dir string, caCert *x509.Certificate, caKey *ecdsa.PrivateKey, now time.Time, rnd io.Reader) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPath := filepath.Join(dir, serverCertFile)
	keyPath := filepath.Join(dir, serverKeyFile)
	state, err := pairStateOf(certPath, keyPath)
	if err != nil {
		return nil, nil, err
	}
	if state == pairComplete {
		server, key, readErr := readPair(certPath, keyPath)
		if readErr == nil && now.Before(server.NotAfter) && server.CheckSignatureFrom(caCert) == nil {
			return server, key, nil
		}
	}
	return issueServerCertificate(caCert, caKey, now, rnd, certPath, keyPath)
}

func pairStateOf(certPath, keyPath string) (pairState, error) {
	_, certErr := os.Stat(certPath)
	_, keyErr := os.Stat(keyPath)
	certMissing := errors.Is(certErr, fs.ErrNotExist)
	keyMissing := errors.Is(keyErr, fs.ErrNotExist)
	switch {
	case certMissing && keyMissing:
		return pairAbsent, nil
	case certErr == nil && keyErr == nil:
		return pairComplete, nil
	case certErr != nil && !certMissing:
		return 0, fmt.Errorf("%w: %w", ErrAuthorityFiles, certErr)
	case keyErr != nil && !keyMissing:
		return 0, fmt.Errorf("%w: %w", ErrAuthorityFiles, keyErr)
	}
	return pairPartial, nil
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
		return nil, nil, fmt.Errorf("%w: %w", ErrRandomSource, err)
	}
	serial, err := randomSerial(rnd)
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "herrscher device authority"},
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              now.Add(authorityValidity),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rnd, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrSigning, err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrAuthorityFiles, err)
	}
	if err := writePair(certPath, keyPath, der, key); err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func issueServerCertificate(caCert *x509.Certificate, caKey *ecdsa.PrivateKey, now time.Time, rnd io.Reader, certPath, keyPath string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rnd)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrRandomSource, err)
	}
	serial, err := randomSerial(rnd)
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "herrscher daemon"},
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              now.Add(serverCertValidity),
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback},
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
	}
	der, err := x509.CreateCertificate(rnd, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrSigning, err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrAuthorityFiles, err)
	}
	if err := writePair(certPath, keyPath, der, key); err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func writePair(certPath, keyPath string, der []byte, key *ecdsa.PrivateKey) error {
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAuthorityFiles, err)
	}
	if err := writePrivate(keyPath, pem.EncodeToMemory(&pem.Block{Type: pemECKey, Bytes: keyDER})); err != nil {
		return err
	}
	return writePrivate(certPath, pem.EncodeToMemory(&pem.Block{Type: pemCertificate, Bytes: der}))
}

func writePrivate(path string, data []byte) (err error) {
	file, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAuthorityFiles, err)
	}
	temporary := file.Name()
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(temporary))
		}
	}()
	if _, err = file.Write(data); err != nil {
		return fmt.Errorf("%w: %w", ErrAuthorityFiles, errors.Join(err, file.Close()))
	}
	if err = file.Sync(); err != nil {
		return fmt.Errorf("%w: %w", ErrAuthorityFiles, errors.Join(err, file.Close()))
	}
	if err = file.Close(); err != nil {
		return fmt.Errorf("%w: %w", ErrAuthorityFiles, err)
	}
	if err = os.Rename(temporary, path); err != nil {
		return fmt.Errorf("%w: %w", ErrAuthorityFiles, err)
	}
	if err = syncDirectory(filepath.Dir(path)); err != nil {
		return fmt.Errorf("%w: %w", ErrAuthorityFiles, err)
	}
	return nil
}

func syncDirectory(dir string) error {
	handle, err := os.Open(filepath.Clean(dir))
	if err != nil {
		return err
	}
	return errors.Join(handle.Sync(), handle.Close())
}

func parseCertificatePEM(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != pemCertificate {
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
	if block == nil || block.Type != pemECKey {
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
		return nil, fmt.Errorf("%w: %w", ErrRandomSource, err)
	}
	raw[0] &= serialTopBitMask
	serial := new(big.Int).SetBytes(raw)
	if serial.Sign() == 0 {
		serial.SetInt64(1)
	}
	return serial, nil
}
