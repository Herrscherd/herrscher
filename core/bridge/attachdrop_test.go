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

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := logger
	logger = obs.NewLogger(&buf, slog.LevelDebug)
	t.Cleanup(func() { logger = prev })
	return &buf
}

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

func TestPassingTheCapIsLogged(t *testing.T) {
	buf := captureLog(t)
	m := contracts.Message{ID: "1"}
	for range maxAttachmentsPerMessage + 1 {
		m.Attachments = append(m.Attachments, contracts.Attachment{
			Filename: "shot.png", URL: "https://evil.test/shot.png", ContentType: "image/png",
		})
	}
	ResolveAttachments(context.Background(), http.DefaultClient, m, "cap", map[string]bool{})
	if !strings.Contains(buf.String(), "per-message cap") {
		t.Fatalf("log = %q, want the cap named", buf.String())
	}
}

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
