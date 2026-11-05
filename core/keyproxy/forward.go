package keyproxy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"time"
)

const (
	drainLimitBytes          = 64 << 10
	flushEveryWrite          = -1
	maxRetryAfter            = time.Hour
	unavailableMessage       = "every key is cooling down\n"
	unreachableMessage       = "upstream unreachable"
	unavailableStatusMessage = "503 Service Unavailable"
)

var (
	errBodyTooLarge = errors.New("request body exceeds the proxy cap")
	errBodyUnread   = errors.New("request body could not be read")
)

type rotatingTransport struct {
	proxy *Proxy
	route Route
	body  []byte
}

func (p *Proxy) forward(writer http.ResponseWriter, request *http.Request, route Route) {
	body, held, err := p.bufferBody(request, route.Pool.options.MaxBody)
	defer p.budget.give(held)
	switch {
	case errors.Is(err, errBodyTooLarge):
		http.Error(writer, errBodyTooLarge.Error(), http.StatusRequestEntityTooLarge)
		return
	case errors.Is(err, errBodyBudget):
		writer.Header().Set("Retry-After", "1")
		http.Error(writer, errBodyBudget.Error(), http.StatusServiceUnavailable)
		return
	case err != nil:
		http.Error(writer, errBodyUnread.Error(), http.StatusBadRequest)
		return
	}
	reverse := &httputil.ReverseProxy{
		Rewrite:       rewriteUpstream(route),
		Transport:     &rotatingTransport{proxy: p, route: route, body: body},
		FlushInterval: flushEveryWrite,
		ErrorLog:      slog.NewLogLogger(p.log.Handler(), slog.LevelWarn),
		ErrorHandler: func(writer http.ResponseWriter, _ *http.Request, cause error) {
			p.log.Warn("key proxy could not reach the upstream", "provider", route.Pool.name, "err", cause)
			http.Error(writer, unreachableMessage, http.StatusBadGateway)
		},
	}
	reverse.ServeHTTP(writer, request)
}

func rewriteUpstream(route Route) func(*httputil.ProxyRequest) {
	return func(outbound *httputil.ProxyRequest) {
		outbound.SetURL(route.Upstream)
		outbound.Out.Header.Del("Authorization")
		outbound.Out.Header.Del("X-Api-Key")
	}
}

func (t *rotatingTransport) RoundTrip(outbound *http.Request) (*http.Response, error) {
	pool := t.route.Pool
	tried := make(map[int]bool, len(pool.keys))
	for {
		now := time.Now()
		index, ok := pool.pick(now, tried)
		if !ok {
			return unavailableResponse(outbound, pool.soonestAvailable(now)), nil
		}
		tried[index] = true
		response, err := t.attempt(outbound, index)
		if err != nil {
			return nil, err
		}
		if !pool.fails(response.StatusCode) || !pool.hasRoom(now, tried) {
			return response, nil
		}
		pool.cool(index, now, retryAfter(response.Header, now))
		t.proxy.log.Info("key proxy moved to another key", "provider", pool.name, "key", pool.key(index).Name(), "status", response.StatusCode)
		discard(response, t.proxy.log)
	}
}

func (t *rotatingTransport) attempt(outbound *http.Request, index int) (*http.Response, error) {
	replay := outbound.Clone(outbound.Context())
	replay.Body = nil
	replay.GetBody = nil
	replay.ContentLength = int64(len(t.body))
	if len(t.body) > 0 {
		replay.Body = io.NopCloser(bytes.NewReader(t.body))
		replay.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(t.body)), nil
		}
	}
	setAuth(replay.Header, t.route.Auth, t.route.Pool.key(index))
	response, err := t.proxy.transport.RoundTrip(replay)
	if err != nil {
		return nil, fmt.Errorf("upstream round trip: %w", err)
	}
	return response, nil
}

func setAuth(header http.Header, style AuthStyle, key Secret) {
	if style == AuthBearer {
		header.Set("Authorization", "Bearer "+key.Value())
		return
	}
	header.Set("X-Api-Key", key.Value())
}

func unavailableResponse(outbound *http.Request, wait time.Duration) *http.Response {
	seconds := max(int(math.Ceil(wait.Seconds())), 1)
	header := make(http.Header)
	header.Set("Retry-After", strconv.Itoa(seconds))
	header.Set("Content-Type", "text/plain; charset=utf-8")
	return &http.Response{
		Status:        unavailableStatusMessage,
		StatusCode:    http.StatusServiceUnavailable,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		Body:          io.NopCloser(strings.NewReader(unavailableMessage)),
		ContentLength: int64(len(unavailableMessage)),
		Request:       outbound,
	}
}

func discard(response *http.Response, log *slog.Logger) {
	_, copyErr := io.CopyN(io.Discard, response.Body, drainLimitBytes)
	if copyErr != nil && !errors.Is(copyErr, io.EOF) {
		log.Debug("key proxy could not drain a refused answer", "err", copyErr)
	}
	closeBody(response.Body, log)
}

func closeBody(body io.Closer, log *slog.Logger) {
	if err := body.Close(); err != nil {
		log.Debug("key proxy could not close an upstream body", "err", err)
	}
}

func retryAfter(header http.Header, now time.Time) time.Duration {
	value := header.Get("Retry-After")
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return boundedSeconds(seconds)
	}
	if at, err := http.ParseTime(value); err == nil {
		return min(max(at.Sub(now), 0), maxRetryAfter)
	}
	return 0
}

func boundedSeconds(seconds int) time.Duration {
	if seconds >= int(maxRetryAfter/time.Second) {
		return maxRetryAfter
	}
	return time.Duration(seconds) * time.Second
}
