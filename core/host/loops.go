package host

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/health"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const healthWindow = 90 * time.Second

func serveHealth(ctx context.Context, addr string, monitor *health.Health) {
	srv := newHealthServer(addr, monitor)
	go func() { <-ctx.Done(); closeRead(srv) }()
	if err := srv.ListenAndServe(); err != nil && ctx.Err() == nil {
		warnStderr("health server: %v\n", err)
	}
}

func newHealthServer(addr string, monitor *health.Health) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(writer http.ResponseWriter, _ *http.Request) {
		snap := monitor.Snapshot(time.Now(), healthWindow)
		writer.Header().Set("Content-Type", "application/json")
		if !snap.Online {
			writer.WriteHeader(http.StatusServiceUnavailable)
		}
		logCleanup(json.NewEncoder(writer).Encode(snap))
	})
	return &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: healthReadHeaderTimeout}
}

func pingLoop(ctx context.Context, prober contracts.Prober, monitor *health.Health) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ms, err := prober.Probe(ctx); err == nil {
				monitor.Ping(time.Now(), ms)
			}
		}
	}
}

func statusContent(instanceID string, snap health.Snapshot) string {
	dot, word := "🟢", "online"
	if !snap.Online {
		dot, word = "🔴", "offline"
	}
	uptime := (time.Duration(snap.UptimeS) * time.Second).String()
	prefix := ""
	if instanceID != "" {
		prefix = "[" + instanceID + "] "
	}
	metrics := snap.Metrics
	return fmt.Sprintf("%s%s **herrscher %s** · uptime %s · ping %dms · %d sessions · turns %d done/%d abandoned · %d restarts",
		prefix, dot, word, uptime, snap.PingMS, snap.Sessions, metrics.TurnsCompleted, metrics.TurnsAbandoned, metrics.BridgeRestarts)
}

func statusLoop(ctx context.Context, channels contracts.ChannelReader, channelID string, hostState *state.State, monitor *health.Health, instanceID string) {
	ticker := time.NewTicker(statusInterval)
	defer ticker.Stop()
	render := func() {
		snap := monitor.Snapshot(time.Now(), healthWindow)
		content := statusContent(instanceID, snap)
		messageID, err := channels.UpsertStatusMessage(ctx, channelID, hostState.StatusMessageID, content)
		if err == nil && messageID != hostState.StatusMessageID {
			logCleanup(hostState.SetStatusMessageID(messageID))
		}
	}
	render()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			render()
		}
	}
}
