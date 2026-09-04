package bridge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestResolveAttachmentsContainsTraversalMessageIDs(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("PNG"))
	}))
	defer srv.Close()
	hosts := map[string]bool(hostsFor(t, srv))

	for _, msgID := range []string{"../../evil", "../..", "a/../../b", ""} {
		t.Run(msgID, func(t *testing.T) {
			m := contracts.Message{
				ID:          msgID,
				Attachments: []contracts.Attachment{{Filename: "x.png", URL: srv.URL + "/x.png", ContentType: "image/png"}},
			}
			got := ResolveAttachments(context.Background(), srv.Client(), m, "sess", hosts)
			if len(got) != 1 {
				t.Fatalf("want 1 resolved path, got %v", got)
			}
			root := filepath.Clean(StagingRoot())
			if !strings.HasPrefix(filepath.Clean(got[0]), root+string(filepath.Separator)) {
				t.Fatalf("staged outside %s: %s", root, got[0])
			}
		})
	}
}
