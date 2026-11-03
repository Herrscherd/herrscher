package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type counter struct {
	calls int
	data  []byte
	err   error
}

func (c *counter) get(context.Context, string) ([]byte, error) {
	c.calls++
	return c.data, c.err
}

func testHosts() []string {
	return []string{"cdn.example.com", "Media.Example.Org"}
}

func TestFetchImageAllowsAListedHost(t *testing.T) {
	c := &counter{data: []byte("bytes")}
	got, err := fetchImage(context.Background(), c.get, testHosts(), "https://cdn.example.com/a.png")
	if err != nil {
		t.Fatalf("an allowed host must fetch: %v", err)
	}
	if string(got) != "bytes" {
		t.Fatalf("fetched %q", got)
	}
	if c.calls != 1 {
		t.Fatalf("fetcher ran %d times, want exactly 1", c.calls)
	}
	if _, err := fetchImage(context.Background(), c.get, testHosts(), "https://media.example.org/b.png"); err != nil {
		t.Fatalf("host matching must be case-insensitive: %v", err)
	}
}

func TestFetchImageRefusesAnUnlistedHostAndNamesIt(t *testing.T) {
	c := &counter{}
	_, err := fetchImage(context.Background(), c.get, testHosts(), "https://evil.example.net/a.png")
	if err == nil {
		t.Fatal("an unlisted host must be refused")
	}
	if !strings.Contains(err.Error(), "evil.example.net") {
		t.Errorf("the refusal must name the host: %v", err)
	}
	if c.calls != 0 {
		t.Fatalf("a refused host must never reach the fetcher, ran %d times", c.calls)
	}
}

func TestFetchImageRefusesAnUnparseableURL(t *testing.T) {
	c := &counter{}
	if _, err := fetchImage(context.Background(), c.get, testHosts(), "://not a url"); err == nil {
		t.Fatal("an unparseable URL must error")
	}
	if c.calls != 0 {
		t.Fatalf("nothing to parse means nothing to fetch, ran %d times", c.calls)
	}
}

func TestFetchImageErrorPropagates(t *testing.T) {
	boom := errors.New("connection refused")
	c := &counter{err: boom}
	_, err := fetchImage(context.Background(), c.get, testHosts(), "https://cdn.example.com/a.png")
	if !errors.Is(err, boom) {
		t.Fatalf("the fetcher's error must reach the caller: %v", err)
	}
}

func TestFetchImageRefusesEverythingWithoutAnAllowlist(t *testing.T) {
	c := &counter{}
	if _, err := fetchImage(context.Background(), c.get, nil, "https://cdn.example.com/a.png"); err == nil {
		t.Fatal("an empty allowlist must allow nothing")
	}
	if c.calls != 0 {
		t.Fatalf("ran %d times with no allowlist", c.calls)
	}
}

func TestImageURLsFindsOnlyImages(t *testing.T) {
	got := imageURLs("see https://cdn.example.com/a.png and https://cdn.example.com/doc.html\nalso http://x.test/b.JPEG")
	want := []string{"https://cdn.example.com/a.png", "http://x.test/b.JPEG"}
	if len(got) != len(want) {
		t.Fatalf("imageURLs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("imageURLs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTranscriptImageIsFetchedAndRendered(t *testing.T) {
	m := newTestModel()
	m.caps = Capabilities{Graphics: GraphicsKitty}
	m.imageHosts = []string{"cdn.example.com"}
	png := encodeFixture(t, "png", testImage(4, 4))
	m.imageFetcher = func(context.Context, string) ([]byte, error) { return png, nil }

	tb := m.ensureTab("a")
	tb.appendEntry(entry{role: roleAgent, text: "here: https://cdn.example.com/a.png"})
	msg := runCmd(m.fetchEntryImages(tb, 0))
	ready, ok := msg.(imageReadyMsg)
	if !ok {
		t.Fatalf("an allowed image URL must produce an imageReadyMsg, got %T", msg)
	}
	m.Update(ready)
	if !strings.Contains(tb.entries[0].preview, "\x1b_G") {
		t.Fatalf("the fetched image must land under its entry: %q", tb.entries[0].preview)
	}
}

func TestTranscriptImageOffTheAllowlistIsNotFetched(t *testing.T) {
	m := newTestModel()
	m.imageHosts = []string{"cdn.example.com"}
	c := &counter{}
	m.imageFetcher = c.get

	tb := m.ensureTab("a")
	tb.appendEntry(entry{role: roleAgent, text: "here: https://evil.example.net/a.png"})
	runCmd(m.fetchEntryImages(tb, 0))
	if c.calls != 0 {
		t.Fatalf("an off-allowlist transcript image must not be fetched, ran %d times", c.calls)
	}
	if tb.entries[0].preview != "" {
		t.Fatalf("nothing must be drawn for it: %q", tb.entries[0].preview)
	}
}

func TestFetchedImageTooLargeToDrawIsDropped(t *testing.T) {
	m := newTestModel()
	m.caps = Capabilities{Graphics: GraphicsKitty}
	m.imageHosts = []string{"cdn.example.com"}
	m.imageFetcher = func(context.Context, string) ([]byte, error) {
		return encodeFixture(t, "png", noiseImage(4000, 200)), nil
	}

	tb := m.ensureTab("a")
	tb.appendEntry(entry{role: roleAgent, text: "here: https://cdn.example.com/huge.png"})
	if msg := runCmd(m.fetchEntryImages(tb, 0)); msg != nil {
		t.Fatalf("an oversized fetched image must produce no message, got %T", msg)
	}
	if tb.entries[0].preview != "" {
		t.Fatalf("nothing must be drawn for it: %q", tb.entries[0].preview)
	}
}
