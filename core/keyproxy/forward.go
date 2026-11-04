package keyproxy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"path"
	"strconv"
	"time"
)

const (
	relayChunkBytes = 32 << 10
	drainLimitBytes = 64 << 10
)

var (
	errBodyTooLarge = errors.New("request body exceeds the proxy cap")
	errBodyUnread   = errors.New("request body could not be read")
)

func (p *Proxy) forward(writer http.ResponseWriter, request *http.Request, route Route) {
	body, err := bufferBody(request, route.Pool.options.MaxBody)
	switch {
	case errors.Is(err, errBodyTooLarge):
		http.Error(writer, errBodyTooLarge.Error(), http.StatusRequestEntityTooLarge)
		return
	case err != nil:
		http.Error(writer, errBodyUnread.Error(), http.StatusBadRequest)
		return
	}
	tried := make(map[int]bool, len(route.Pool.keys))
	for {
		now := time.Now()
		index, ok := route.Pool.pick(now, tried)
		if !ok {
			writeUnavailable(writer, route.Pool.soonestAvailable(now))
			return
		}
		tried[index] = true
		response, err := p.send(request, route, index, body)
		if err != nil {
			p.log.Warn("key proxy could not reach the upstream", "provider", route.Pool.name, "err", err)
			http.Error(writer, "upstream unreachable", http.StatusBadGateway)
			return
		}
		if !route.Pool.fails(response.StatusCode) || !route.Pool.hasRoom(now, tried) {
			relay(writer, response, p.log)
			return
		}
		route.Pool.cool(index, now, retryAfter(response.Header, now))
		p.log.Info("key proxy moved to another key", "provider", route.Pool.name, "key", route.Pool.key(index).Name(), "status", response.StatusCode)
		discard(response, p.log)
	}
}

func bufferBody(request *http.Request, limit int64) ([]byte, error) {
	if request.Body == nil || request.Body == http.NoBody {
		return nil, nil
	}
	buffered, err := io.ReadAll(io.LimitReader(request.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(buffered)) > limit {
		return nil, errBodyTooLarge
	}
	return buffered, nil
}

func (p *Proxy) send(request *http.Request, route Route, index int, body []byte) (*http.Response, error) {
	outbound, err := http.NewRequestWithContext(request.Context(), request.Method, route.Upstream.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	outbound.URL.Path = path.Join("/", route.Upstream.Path, request.URL.Path)
	outbound.URL.RawQuery = request.URL.RawQuery
	copyRequestHeaders(outbound.Header, request.Header)
	setAuth(outbound.Header, route.Auth, route.Pool.key(index))
	return p.client.Do(outbound)
}

func copyRequestHeaders(dst, src http.Header) {
	for name, values := range src {
		canonical := http.CanonicalHeaderKey(name)
		if isHopByHop(canonical) || canonical == "Authorization" || canonical == "X-Api-Key" {
			continue
		}
		dst[canonical] = append([]string(nil), values...)
	}
}

func isHopByHop(canonical string) bool {
	switch canonical {
	case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Proxy-Connection", "Te", "Trailer", "Transfer-Encoding", "Upgrade":
		return true
	}
	return false
}

func setAuth(header http.Header, style AuthStyle, key Secret) {
	if style == AuthBearer {
		header.Set("Authorization", "Bearer "+key.Value())
		return
	}
	header.Set("X-Api-Key", key.Value())
}

func relay(writer http.ResponseWriter, response *http.Response, log *slog.Logger) {
	defer closeBody(response.Body, log)
	copyResponseHeaders(writer.Header(), response.Header)
	writer.WriteHeader(response.StatusCode)
	flusher, canFlush := writer.(http.Flusher)
	buffer := make([]byte, relayChunkBytes)
	for {
		count, readErr := response.Body.Read(buffer)
		if count > 0 {
			if _, writeErr := writer.Write(buffer[:count]); writeErr != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if errors.Is(readErr, io.EOF) {
			return
		}
		if readErr != nil {
			log.Warn("key proxy stopped relaying", "err", readErr)
			return
		}
	}
}

func copyResponseHeaders(dst, src http.Header) {
	for name, values := range src {
		if !isHopByHop(http.CanonicalHeaderKey(name)) {
			dst[name] = append([]string(nil), values...)
		}
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
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		return max(at.Sub(now), 0)
	}
	return 0
}

func writeUnavailable(writer http.ResponseWriter, wait time.Duration) {
	seconds := max(int(math.Ceil(wait.Seconds())), 1)
	writer.Header().Set("Retry-After", strconv.Itoa(seconds))
	http.Error(writer, "every key is cooling down", http.StatusServiceUnavailable)
}
