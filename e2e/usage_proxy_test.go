//go:build e2e && linux

package e2e_test

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rogpeppe/go-internal/testscript"
)

const (
	usageHost       = "api.anthropic.com"
	usagePath       = "/api/oauth/usage"
	exhaustedToken  = "exhausted-token"
	spentPercent    = 100
	freshPercent    = 10
	usageCAFileName = "usage-ca.pem"
	usageCertYears  = 1
)

type usageProxy struct {
	address string
	caFile  string
	tls     *tls.Config
}

func startUsageProxy(dir string) (*usageProxy, net.Listener, error) {
	authority, err := newUsageAuthority()
	if err != nil {
		return nil, nil, err
	}
	caFile := filepath.Join(dir, usageCAFileName)
	if err := os.WriteFile(caFile, authority.caPEM, 0o600); err != nil {
		return nil, nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}
	proxy := &usageProxy{address: listener.Addr().String(), caFile: caFile, tls: authority.serverConfig()}
	go proxy.accept(listener)
	return proxy, listener, nil
}

func (p *usageProxy) accept(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go p.serveConnect(conn)
	}
}

func (p *usageProxy) serveConnect(conn net.Conn) {
	request, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil || request.Method != http.MethodConnect || request.Host != usageHost+":443" {
		_, _ = fmt.Fprint(conn, "HTTP/1.1 405 Method Not Allowed\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		_ = conn.Close()
		return
	}
	if _, err := fmt.Fprint(conn, "HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
		_ = conn.Close()
		return
	}
	closed := make(chan struct{})
	secured := tls.Server(&notifyClose{Conn: conn, closed: closed}, p.tls)
	if err := secured.Handshake(); err != nil {
		_ = secured.Close()
		return
	}
	_ = http.Serve(&singleConn{conn: secured, closed: closed}, http.HandlerFunc(serveUsage))
}

func serveUsage(out http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet || request.URL.Path != usagePath {
		http.NotFound(out, request)
		return
	}
	percent := freshPercent
	if strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ") == exhaustedToken {
		percent = spentPercent
	}
	out.Header().Set("Content-Type", "application/json")
	report := map[string]map[string]any{
		"five_hour": {
			"utilization": percent,
			"resets_at":   time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		},
	}
	if err := json.NewEncoder(out).Encode(report); err != nil {
		http.Error(out, err.Error(), http.StatusInternalServerError)
	}
}

type notifyClose struct {
	net.Conn
	closed chan struct{}
	once   sync.Once
}

func (n *notifyClose) Close() error {
	n.once.Do(func() { close(n.closed) })
	return n.Conn.Close()
}

type singleConn struct {
	conn   net.Conn
	closed <-chan struct{}
	used   bool
}

func (s *singleConn) Accept() (net.Conn, error) {
	if !s.used {
		s.used = true
		return s.conn, nil
	}
	<-s.closed
	return nil, net.ErrClosed
}

func (s *singleConn) Close() error {
	return nil
}

func (s *singleConn) Addr() net.Addr {
	return &net.TCPAddr{}
}

type usageAuthority struct {
	caPEM []byte
	leaf  tls.Certificate
}

func newUsageAuthority() (usageAuthority, error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return usageAuthority{}, err
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "herrscher e2e usage CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(usageCertYears, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return usageAuthority{}, err
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return usageAuthority{}, err
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return usageAuthority{}, err
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: usageHost},
		DNSNames:     []string{usageHost},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(usageCertYears, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		return usageAuthority{}, err
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	return usageAuthority{
		caPEM: caPEM,
		leaf:  tls.Certificate{Certificate: [][]byte{leafDER}, PrivateKey: leafKey},
	}, nil
}

func (a usageAuthority) serverConfig() *tls.Config {
	return &tls.Config{Certificates: []tls.Certificate{a.leaf}, MinVersion: tls.VersionTLS12}
}

func usageProxyEnv(proxy *usageProxy) []string {
	return []string{
		"HTTPS_PROXY=http://" + proxy.address,
		"NO_PROXY=",
		"SSL_CERT_FILE=" + proxy.caFile,
	}
}

func stopUsageProxy(env *testscript.Env, listener net.Listener) {
	env.Defer(func() {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			env.T().Fatal(err)
		}
	})
}
