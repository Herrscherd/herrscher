// Package health tracks the host liveness and renders its health snapshot.
package health

import (
	"sync"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/metrics"
)

// Health is the daemon's in-memory liveness state. Thread-safe. Driven only by
// bot/transport facts (gateway heartbeat + REST self-ping), never by a bridged
// Claude session.
type Health struct {
	mu            sync.Mutex
	startedAt     time.Time
	lastHeartbeat time.Time
	lastPing      time.Time
	pingLatencyMS int64
	sessions      int
	metrics       *metrics.Registry
}

// Snapshot is an immutable view rendered for /health and the status embed.
type Snapshot struct {
	Online        bool             `json:"online"`
	UptimeS       int64            `json:"uptime_s"`
	PingMS        int64            `json:"ping_ms"`
	Sessions      int              `json:"sessions"`
	LastHeartbeat string           `json:"last_heartbeat"`
	LastPing      string           `json:"last_ping"`
	Metrics       metrics.Snapshot `json:"metrics"`
}

// NewHealth starts a Health clock at startedAt with a fresh metrics registry.
func NewHealth(startedAt time.Time) *Health {
	return &Health{startedAt: startedAt, metrics: metrics.NewRegistry()}
}

// Metrics returns the runtime metrics registry surfaced on every snapshot, so
// the supervisor and turn loop can record into the same registry health reports.
func (h *Health) Metrics() *metrics.Registry { return h.metrics }

// HeartbeatAck records a gateway heartbeat ACK at t.
func (h *Health) HeartbeatAck(when time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastHeartbeat = when
}

// Ping records a successful REST self-ping at t with round-trip latency.
func (h *Health) Ping(when time.Time, latencyMS int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastPing, h.pingLatencyMS = when, latencyMS
}

// SetSessions records the active supervised-bridge count.
func (h *Health) SetSessions(count int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions = count
}

// Snapshot renders health as of now; Online is true iff the last heartbeat ACK
// is within window.
func (h *Health) Snapshot(now time.Time, window time.Duration) Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	online := !h.lastHeartbeat.IsZero() && now.Sub(h.lastHeartbeat) <= window
	return Snapshot{
		Online:        online,
		UptimeS:       int64(now.Sub(h.startedAt).Seconds()),
		PingMS:        h.pingLatencyMS,
		Sessions:      h.sessions,
		LastHeartbeat: stamp(h.lastHeartbeat),
		LastPing:      stamp(h.lastPing),
		Metrics:       h.metrics.Snapshot(),
	}
}

func stamp(when time.Time) string {
	if when.IsZero() {
		return ""
	}
	return when.UTC().Format(time.RFC3339)
}
