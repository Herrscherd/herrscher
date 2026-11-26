//go:build linux

package mcpweight

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/agent"
)

const fakeTagEnv = "HERRSCHER_MCPWEIGHT_TAG"

func TestConcurrentReportsShareTheConcurrencyLimit(t *testing.T) {
	tag := t.Name() + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	servers := make([]agent.MCPServer, 0, 2*Concurrency)
	for range 2 * Concurrency {
		servers = append(servers, fakeServerWithEnv("silent", map[string]string{fakeTagEnv: tag}))
	}
	measurer := NewMeasurer()

	done := make(chan struct{})
	sampled := make(chan int)
	go func() {
		peak := 0
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			live, err := liveTagged(tag)
			if err != nil {
				t.Error(err)
			}
			peak = max(peak, live)
			select {
			case <-done:
				sampled <- peak
				return
			case <-ticker.C:
			}
		}
	}()

	var reports sync.WaitGroup
	for range 2 {
		reports.Go(func() {
			measurer.MeasureAll(context.Background(), servers, 2*time.Second)
		})
	}
	reports.Wait()
	close(done)
	peak := <-sampled

	if peak > Concurrency {
		t.Fatalf("peak of live servers = %d, want at most %d across both reports", peak, Concurrency)
	}
	if peak == 0 {
		t.Fatal("the probe never saw a live server, so the limit was not tested")
	}
}

func liveTagged(tag string) (int, error) {
	entry := []byte(fakeTagEnv + "=" + tag + "\x00")
	paths, err := filepath.Glob("/proc/[0-9]*/environ")
	if err != nil {
		return 0, err
	}
	live := 0
	for _, path := range paths {
		environ, err := os.ReadFile(path)
		if err == nil && bytes.Contains(environ, entry) {
			live++
		}
	}
	return live, nil
}
