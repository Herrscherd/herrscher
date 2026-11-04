// Package keyproxy is the host reverse proxy that spawned CLIs reach through a per-spawn token, rotating a provider key pool behind it.
package keyproxy

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Herrscherd/herrscher/core/config"
)

// AuthStyle is the header a provider reads its key from.
type AuthStyle string

const (
	// AuthXAPIKey sends the key in the x-api-key header.
	AuthXAPIKey AuthStyle = "x-api-key"
	// AuthBearer sends the key as a bearer token.
	AuthBearer AuthStyle = "bearer"
)

const (
	tokenBytes        = 32
	tokenPrefix       = "hsk_"
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 5 * time.Second
	dialTimeout       = 30 * time.Second
	responseTimeout   = 10 * time.Minute
	idleConnTimeout   = 90 * time.Second
	tlsHandshakeLimit = 10 * time.Second
	maxIdleConns      = 8
)

const (
	defaultAnthropicBaseURL = "https://api.anthropic.com"
	defaultOpenAIBaseURL    = "https://api.openai.com"
)

var (
	// ErrDisabled reports that the config switches the key proxy off.
	ErrDisabled = errors.New("key proxy is disabled")
	// ErrProxyClosed reports a grant asked of a closed proxy.
	ErrProxyClosed = errors.New("key proxy is closed")
	// ErrNoListener reports a proxy that could not bind a loopback port.
	ErrNoListener = errors.New("key proxy could not bind a loopback port")
)

// Route is what a grant forwards to: upstream, key header and key pool.
type Route struct {
	Upstream *url.URL
	Auth     AuthStyle
	Pool     *Pool
}

// Grant is the base URL and token a spawned CLI receives in place of its key.
type Grant struct {
	BaseURL string
	Token   string
}

type grant struct {
	route Route
	token string
}

// Proxy is the running reverse proxy, created by Open and closed by Close.
type Proxy struct {
	log       *slog.Logger
	providers map[string]Route
	client    *http.Client
	listener  net.Listener
	server    *http.Server
	served    chan struct{}

	mu     sync.Mutex
	grants []*grant
	closed bool
}

// Open loads every configured key and starts serving; it returns ErrDisabled when the proxy is off.
func Open(ctx context.Context, keys *config.KeysConfig, loader Loader, log *slog.Logger) (*Proxy, error) {
	if !keys.ProxyEnabled() {
		return nil, ErrDisabled
	}
	providers, err := loadProviders(ctx, keys, loader)
	if err != nil {
		return nil, err
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, errors.Join(ErrNoListener, err)
	}
	proxy := &Proxy{
		log:       log,
		providers: providers,
		client:    newClient(),
		listener:  listener,
		served:    make(chan struct{}),
	}
	proxy.server = &http.Server{Handler: proxy, ReadHeaderTimeout: readHeaderTimeout}
	go proxy.serve()
	return proxy, nil
}

func newClient() *http.Client {
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: dialTimeout}).DialContext,
		TLSHandshakeTimeout:   tlsHandshakeLimit,
		ResponseHeaderTimeout: responseTimeout,
		IdleConnTimeout:       idleConnTimeout,
		MaxIdleConns:          maxIdleConns,
		ForceAttemptHTTP2:     true,
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (p *Proxy) serve() {
	defer close(p.served)
	if err := p.server.Serve(p.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		p.log.Error("key proxy stopped", "err", err)
	}
}

func loadProviders(ctx context.Context, keys *config.KeysConfig, loader Loader) (map[string]Route, error) {
	providers := make(map[string]Route, len(keys.Providers))
	for name, provider := range keys.Providers {
		route, err := loadProvider(ctx, name, provider, loader)
		if err != nil {
			return nil, fmt.Errorf("keys.providers.%s: %w", name, err)
		}
		providers[name] = route
	}
	return providers, nil
}

func loadProvider(ctx context.Context, name string, provider config.ProviderKeys, loader Loader) (Route, error) {
	upstream, auth, err := providerDefaults(name, provider.BaseURL)
	if err != nil {
		return Route{}, err
	}
	secrets := make([]Secret, 0, len(provider.Sources))
	for _, source := range provider.Sources {
		secret, err := loader.Load(ctx, source)
		if err != nil {
			return Route{}, err
		}
		secrets = append(secrets, secret)
	}
	return Route{Upstream: upstream, Auth: auth, Pool: PoolFromConfig(name, secrets, provider)}, nil
}

func providerDefaults(name, baseURL string) (*url.URL, AuthStyle, error) {
	switch name {
	case config.ProviderAnthropic:
		upstream, err := parseBase(baseURL, defaultAnthropicBaseURL)
		return upstream, AuthXAPIKey, err
	case config.ProviderOpenAI:
		upstream, err := parseBase(baseURL, defaultOpenAIBaseURL)
		return upstream, AuthBearer, err
	}
	return nil, "", fmt.Errorf("%q: %w", name, config.ErrKeysProvider)
}

func parseBase(configured, fallback string) (*url.URL, error) {
	if configured == "" {
		configured = fallback
	}
	parsed, err := url.Parse(configured)
	if err != nil {
		return nil, fmt.Errorf("base URL %q: %w", configured, err)
	}
	return parsed, nil
}

// Provider returns the route of a configured provider.
func (p *Proxy) Provider(name string) (Route, bool) {
	route, ok := p.providers[name]
	return route, ok
}

// GatewayRoute builds a route to a single-key upstream such as the gateway.
func GatewayRoute(upstream *url.URL, auth AuthStyle, key Secret) Route {
	return Route{Upstream: upstream, Auth: auth, Pool: SingleKeyPool(key.Name(), key)}
}

// Grant mints a token for one spawn, valid until Revoke or Close.
func (p *Proxy) Grant(route Route) (Grant, error) {
	token, err := newToken()
	if err != nil {
		return Grant{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return Grant{}, ErrProxyClosed
	}
	p.grants = append(p.grants, &grant{route: route, token: token})
	return Grant{BaseURL: "http://" + p.listener.Addr().String(), Token: token}, nil
}

func newToken() (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("mint key proxy token: %w", err)
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

// Revoke ends the grant that owns token.
func (p *Proxy) Revoke(token string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	kept := p.grants[:0]
	for _, entry := range p.grants {
		if entry.token != token {
			kept = append(kept, entry)
		}
	}
	p.grants = kept
}

// Close revokes every grant and stops the listener.
func (p *Proxy) Close(ctx context.Context) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.grants = nil
	p.mu.Unlock()
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	err := p.server.Shutdown(shutdownCtx)
	<-p.served
	return err
}

func (p *Proxy) lookup(presented string) (*grant, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var found *grant
	for _, entry := range p.grants {
		if subtle.ConstantTimeCompare([]byte(presented), []byte(entry.token)) == 1 {
			found = entry
		}
	}
	return found, found != nil
}

func presentedToken(header http.Header) string {
	if key := header.Get("X-Api-Key"); key != "" {
		return key
	}
	scheme, token, found := strings.Cut(header.Get("Authorization"), " ")
	if found && strings.EqualFold(scheme, "Bearer") {
		return token
	}
	return ""
}

// ServeHTTP authenticates the spawn that made the request and forwards it.
func (p *Proxy) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	entry, ok := p.lookup(presentedToken(request.Header))
	if !ok {
		p.log.Warn("key proxy refused a request without a valid token", "path", request.URL.Path)
		http.Error(writer, "unknown or revoked proxy token", http.StatusUnauthorized)
		return
	}
	p.forward(writer, request, entry.route)
}
