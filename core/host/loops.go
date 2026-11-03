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

func serveHealth(ctx context.Context, addr string, h *health.Health) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		snap := h.Snapshot(time.Now(), healthWindow)
		w.Header().Set("Content-Type", "application/json")
		if !snap.Online {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		logCleanup(json.NewEncoder(w).Encode(snap))
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: healthReadHeaderTimeout}
	go func() { <-ctx.Done(); closeRead(srv) }()
	if err := srv.ListenAndServe(); err != nil && ctx.Err() == nil {
		warnStderr("health server: %v\n", err)
	}
}

func pingLoop(ctx context.Context, pr contracts.Prober, h *health.Health) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if ms, err := pr.Probe(ctx); err == nil {
				h.Ping(time.Now(), ms)
			}
		}
	}
}

func statusContent(instanceID string, snap health.HealthSnapshot) string {
	dot, word := "🟢", "online"
	if !snap.Online {
		dot, word = "🔴", "offline"
	}
	uptime := (time.Duration(snap.UptimeS) * time.Second).String()
	prefix := ""
	if instanceID != "" {
		prefix = "[" + instanceID + "] "
	}
	m := snap.Metrics
	return fmt.Sprintf("%s%s **herrscher %s** · uptime %s · ping %dms · %d sessions · turns %d done/%d abandoned · %d restarts",
		prefix, dot, word, uptime, snap.PingMS, snap.Sessions, m.TurnsCompleted, m.TurnsAbandoned, m.BridgeRestarts)
}

func statusLoop(ctx context.Context, cr contracts.ChannelReader, channelID string, st *state.State, h *health.Health, instanceID string) {
	t := time.NewTicker(statusInterval)
	defer t.Stop()
	render := func() {
		snap := h.Snapshot(time.Now(), healthWindow)
		content := statusContent(instanceID, snap)
		id, err := cr.UpsertStatusMessage(ctx, channelID, st.StatusMessageID, content)
		if err == nil && id != st.StatusMessageID {
			logCleanup(st.SetStatusMessageID(id))
		}
	}
	render()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			render()
		}
	}
}
