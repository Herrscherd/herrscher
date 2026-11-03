package host

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestRendererPostsOnlyFinalReply(t *testing.T) {
	g := &fanRecorder{}
	r := newGatewayRenderer(g, "c1")

	r.handle(context.Background(), contracts.Event{T: "human", Who: "alice", Text: "hi"})
	r.handle(context.Background(), contracts.Event{T: "status", Text: "Edit file.go"})
	r.handle(context.Background(), contracts.Event{T: "chunk", Text: "thinking"})
	r.handle(context.Background(), contracts.Event{T: "reset"})
	r.handle(context.Background(), contracts.Event{T: "reply", Text: "done", Done: true})

	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.posted) != 1 || g.posted[0] != "done" {
		t.Fatalf("renderer posted %v, want one final reply 'done'", g.posted)
	}
	if g.upserts != 0 || len(g.statuses) != 0 {
		t.Fatalf("renderer must not render progress; upserts=%d statuses=%v", g.upserts, g.statuses)
	}
}

func TestRendererSkipsEmptyReply(t *testing.T) {
	g := &fanRecorder{}
	r := newGatewayRenderer(g, "c1")
	r.handle(context.Background(), contracts.Event{T: "reply", Done: true})
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.posted) != 0 {
		t.Fatalf("empty reply must not post; got %v", g.posted)
	}
}

func TestRendererPostsChoiceAsText(t *testing.T) {
	g := &fanRecorder{}
	r := newGatewayRenderer(g, "c1")
	r.handle(context.Background(), contracts.Event{T: "reply", Text: "Pick:\n1) yes\n2) no", Done: true})
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.posted) != 1 || !strings.Contains(g.posted[0], "Pick:") {
		t.Fatalf("choice text not posted: %v", g.posted)
	}
}

func TestRendererChunksLongReply(t *testing.T) {
	g := &fanRecorder{}
	r := newGatewayRenderer(g, "c1")
	long := strings.Repeat("x", gatewayMaxLen+50)
	r.handle(context.Background(), contracts.Event{T: "reply", Text: long, Done: true})
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.posted) != 2 {
		t.Fatalf("posted %d chunks, want 2", len(g.posted))
	}
}

func TestChunkRuneSafeAndNewlinePreferred(t *testing.T) {
	input := strings.Repeat("é", gatewayMaxLen+10)
	parts := chunk(input, gatewayMaxLen)
	for i, p := range parts {
		if !utf8.ValidString(p) {
			t.Fatalf("part %d not valid UTF-8: %q", i, p)
		}
		if n := utf8.RuneCountInString(p); n > gatewayMaxLen {
			t.Fatalf("part %d has %d runes, want <= %d", i, n, gatewayMaxLen)
		}
	}
	if got := strings.Join(parts, ""); got != input {
		t.Fatalf("rejoined chunks lost runes")
	}

	head := strings.Repeat("a", 1500)
	nl := head + "\n" + strings.Repeat("b", 1000)
	parts = chunk(nl, gatewayMaxLen)
	if len(parts) != 2 || parts[0] != head {
		t.Fatalf("newline break: got %d parts, first len=%d", len(parts), len(parts[0]))
	}
}
