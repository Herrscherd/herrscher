package bridge

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/obs"
)

// captureLog swaps the package logger for one writing into a buffer, so a test
// can read what an operator would have seen.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := logger
	logger = obs.NewLogger(&buf, slog.LevelDebug)
	t.Cleanup(func() { logger = prev })
	return &buf
}

// A dropped attachment used to vanish without a word, which is how an allowlist
// that was never populated went unnoticed: every screenshot was thrown away, no
// turn ever failed, and nothing said so. Each reason for dropping one now names
// the file it dropped.
func TestEveryDroppedAttachmentIsLogged(t *testing.T) {
	cases := []struct {
		name string
		att  contracts.Attachment
		want string
	}{
		{
			name: "off-allowlist",
			att:  contracts.Attachment{Filename: "shot.png", URL: "https://evil.test/shot.png"},
			want: "shot.png",
		},
		{
			name: "unsupported type",
			att:  contracts.Attachment{Filename: "payload.bin", URL: "https://cdn.example.test/payload.bin"},
			want: "payload.bin",
		},
		{
			name: "oversized",
			att: contracts.Attachment{
				Filename: "huge.png", URL: "https://cdn.example.test/huge.png",
				ContentType: "image/png", Size: maxAttachmentBytes + 1,
			},
			want: "huge.png",
		},
		{
			name: "bad file url",
			att:  contracts.Attachment{Filename: "escape.png", URL: "file:///etc/passwd"},
			want: "escape.png",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureLog(t)
			hosts := map[string]bool{"cdn.example.test": true}
			m := contracts.Message{ID: "1", Attachments: []contracts.Attachment{tc.att}}
			if paths := ResolveAttachments(context.Background(), http.DefaultClient, m, "drops", hosts); len(paths) != 0 {
				t.Fatalf("want nothing resolved, got %v", paths)
			}
			if !strings.Contains(buf.String(), tc.want) {
				t.Fatalf("log = %q, want it to name %q", buf.String(), tc.want)
			}
		})
	}
}

// Going past the per-message cap is the one drop that is not about a single
// file, and it is the one an operator is most likely to misread as the model
// ignoring an image — so it says the cap out loud.
func TestPassingTheCapIsLogged(t *testing.T) {
	buf := captureLog(t)
	m := contracts.Message{ID: "1"}
	for i := 0; i < maxAttachmentsPerMessage+1; i++ {
		m.Attachments = append(m.Attachments, contracts.Attachment{
			Filename: "shot.png", URL: "https://evil.test/shot.png", ContentType: "image/png",
		})
	}
	ResolveAttachments(context.Background(), http.DefaultClient, m, "cap", map[string]bool{})
	if !strings.Contains(buf.String(), "per-message cap") {
		t.Fatalf("log = %q, want the cap named", buf.String())
	}
}

// A CDN link commonly carries a signature that hands the file to whoever holds
// it. That url now travels into a log line, so the signature has to come off
// first — a log outlives the link it quotes.
func TestTheAttachmentErrorDropsTheURLSignature(t *testing.T) {
	err := validateCDNURL("https://evil.test/shot.png?ex=67&is=67&hm=deadbeefsignature", allowedHosts{})
	if err == nil {
		t.Fatal("want an error for an off-allowlist url")
	}
	if strings.Contains(err.Error(), "hm=") || strings.Contains(err.Error(), "deadbeefsignature") {
		t.Fatalf("err = %v, want the signature stripped", err)
	}
	if !strings.Contains(err.Error(), "shot.png") {
		t.Fatalf("err = %v, want it to still name the file", err)
	}
}
