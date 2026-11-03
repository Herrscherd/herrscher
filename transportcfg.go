package main

import (
	"errors"
	"log/slog"
	"os"
	"strings"

	"google.golang.org/grpc/credentials"

	contracts "github.com/Herrscherd/herrscher-contracts"
	transport "github.com/Herrscherd/herrscher-transport"
	"github.com/Herrscherd/herrscher/core/host"
)

var errTLSUnset = errors.New("no TLS configured: plaintext loopback")

func remoteCategories() map[contracts.Category]bool {
	out := make(map[contracts.Category]bool)
	for _, category := range strings.Split(os.Getenv("HERRSCHER_REMOTE"), ",") {
		category = strings.TrimSpace(category)
		if category != "" {
			out[contracts.Category(category)] = true
		}
	}
	return out
}

func tlsConfigFromEnv() transport.TLSConfig {
	return transport.TLSConfig{
		CAFile:   os.Getenv("HERRSCHER_TLS_CA"),
		CertFile: os.Getenv("HERRSCHER_TLS_CERT"),
		KeyFile:  os.Getenv("HERRSCHER_TLS_KEY"),
	}
}

func bindAddr() string {
	if a := strings.TrimSpace(os.Getenv("HERRSCHER_BIND")); a != "" {
		return a
	}
	return "127.0.0.1:0"
}

func remoteClientCredentials() (credentials.TransportCredentials, error) {
	cfg := tlsConfigFromEnv()
	if !cfg.Enabled() {
		return nil, errTLSUnset
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg.ClientCredentials()
}

func newResolver(log *slog.Logger) (*host.Resolver, error) {
	resolver := host.NewResolver(remoteCategories(), os.Getenv("HERRSCHER_NATS"))
	resolver.SetLogger(log)
	creds, err := remoteClientCredentials()
	if err != nil && !errors.Is(err, errTLSUnset) {
		return nil, err
	}
	resolver.SetCredentials(creds)
	return resolver, nil
}
