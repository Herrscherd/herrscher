package bridge

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func hostsFor(t *testing.T, srv *httptest.Server) allowedHosts {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	return allowedHosts{u.Hostname(): true}
}

func TestResolveAttachmentsFetchesSupportedTypes(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOrLog(w, []byte("DATA"+r.URL.Path))
	}))
	defer srv.Close()

	sess := "fetch-supported-types"
	defer removeAllOrFail(t, attachmentDir(sess))
	m := contracts.Message{
		ID: "42",
		Attachments: []contracts.Attachment{
			{Filename: "shot.png", URL: srv.URL + "/shot.png"},
			{Filename: "spec.pdf", URL: srv.URL + "/spec.pdf", ContentType: "application/pdf"},
			{Filename: "notes.txt", URL: srv.URL + "/notes.txt", ContentType: "text/plain; charset=utf-8"},
			{Filename: "archive.zip", URL: srv.URL + "/archive.zip", ContentType: "application/zip"},
		},
	}
	paths := ResolveAttachments(context.Background(), srv.Client(), m, sess, hostsFor(t, srv))
	if len(paths) != 3 {
		t.Fatalf("want image+pdf+text and no archive, got %d: %v", len(paths), paths)
	}
	want := []string{"42-0-shot.png", "42-1-spec.pdf", "42-2-notes.txt"}
	for i, w := range want {
		if got := filepath.Base(paths[i]); got != w {
			t.Errorf("path %d = %s, want %s", i, got, w)
		}
	}
	b, err := os.ReadFile(paths[1])
	if err != nil || string(b) != "DATA/spec.pdf" {
		t.Errorf("downloaded pdf content = %q, err=%v", b, err)
	}
}

func TestResolveAttachmentsOrderAndCollision(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOrLog(w, []byte(r.URL.Path))
	}))
	defer srv.Close()

	sess := "order-and-collision"
	defer removeAllOrFail(t, attachmentDir(sess))
	m := contracts.Message{
		ID: "7",
		Attachments: []contracts.Attachment{
			{Filename: "shot.png", URL: srv.URL + "/1"},
			{Filename: "shot.png", URL: srv.URL + "/2"},
		},
	}
	paths := ResolveAttachments(context.Background(), srv.Client(), m, sess, hostsFor(t, srv))
	if len(paths) != 2 {
		t.Fatalf("same-named images should not collide, got %v", paths)
	}
	if paths[0] == paths[1] {
		t.Fatalf("dest collision: %v", paths)
	}
	if b := mustReadFile(t, paths[0]); string(b) != "/1" {
		t.Errorf("first path content = %q, want /1", b)
	}
}

func TestResolveAttachmentsOrderSurvivesASlowFirstFetch(t *testing.T) {
	others := make(chan struct{}, 2)
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/1" {
			<-release
		} else {
			others <- struct{}{}
		}
		writeOrLog(w, []byte(r.URL.Path))
	}))
	defer srv.Close()
	go releaseAfterArrivals(t, others, 2, release)

	sess := "slow-first"
	defer removeAllOrFail(t, attachmentDir(sess))
	m := contracts.Message{
		ID: "9",
		Attachments: []contracts.Attachment{
			{Filename: "a.png", URL: srv.URL + "/1"},
			{Filename: "b.png", URL: srv.URL + "/2"},
			{Filename: "c.png", URL: srv.URL + "/3"},
		},
	}
	paths := ResolveAttachments(context.Background(), srv.Client(), m, sess, hostsFor(t, srv))
	if len(paths) != 3 {
		t.Fatalf("got %d paths, want 3: %v", len(paths), paths)
	}
	for i, want := range []string{"/1", "/2", "/3"} {
		if b := mustReadFile(t, paths[i]); string(b) != want {
			t.Errorf("paths[%d] content = %q, want %q", i, b, want)
		}
	}
}

func TestResolveAttachmentsFetchesConcurrently(t *testing.T) {
	arrivals := make(chan struct{}, maxAttachmentsPerMessage)
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrivals <- struct{}{}
		<-release
		writeOrLog(w, []byte(r.URL.Path))
	}))
	defer srv.Close()

	sess := "concurrent"
	defer removeAllOrFail(t, attachmentDir(sess))
	m := contracts.Message{ID: "10"}
	for i := range maxAttachmentsPerMessage {
		m.Attachments = append(m.Attachments, contracts.Attachment{
			Filename: fmt.Sprintf("f%d.png", i),
			URL:      fmt.Sprintf("%s/%d", srv.URL, i),
		})
	}

	go releaseAfterArrivals(t, arrivals, maxAttachmentsPerMessage, release)
	paths := ResolveAttachments(context.Background(), srv.Client(), m, sess, hostsFor(t, srv))
	if len(paths) != maxAttachmentsPerMessage {
		t.Fatalf("got %d paths, want %d", len(paths), maxAttachmentsPerMessage)
	}
}

func releaseAfterArrivals(t *testing.T, arrivals <-chan struct{}, n int, release chan struct{}) {
	t.Helper()
	defer close(release)
	for range n {
		select {
		case <-arrivals:
		case <-time.After(10 * time.Second):
			t.Errorf("fewer than %d requests were in flight at once", n)
			return
		}
	}
}

func TestResolveAttachmentsHTTPError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeOrLog(w, []byte("ok"))
	}))
	defer srv.Close()

	sess := "http-error"
	defer removeAllOrFail(t, attachmentDir(sess))
	m := contracts.Message{
		ID: "9",
		Attachments: []contracts.Attachment{
			{Filename: "bad.png", URL: srv.URL + "/bad"},
			{Filename: "good.png", URL: srv.URL + "/good"},
		},
	}
	paths := ResolveAttachments(context.Background(), srv.Client(), m, sess, hostsFor(t, srv))
	if len(paths) != 1 {
		t.Fatalf("want 1 surviving path, got %v", paths)
	}
}

func TestResolveAttachmentsRejectsOffAllowlist(t *testing.T) {
	m := contracts.Message{
		ID: "1",
		Attachments: []contracts.Attachment{
			{Filename: "x.png", URL: "https://169.254.169.254/latest"},
		},
	}
	hosts := map[string]bool{"cdn.example.com": true}
	if paths := ResolveAttachments(context.Background(), http.DefaultClient, m, "off-allowlist", hosts); len(paths) != 0 {
		t.Fatalf("off-allowlist url must be rejected, got paths=%v", paths)
	}
}

func TestResolveAttachmentsRejectsRedirectOffAllowlist(t *testing.T) {
	cdn := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://169.254.169.254/latest", http.StatusFound)
	}))
	defer cdn.Close()

	sess := "redirect-off-allowlist"
	defer removeAllOrFail(t, attachmentDir(sess))
	m := contracts.Message{
		ID:          "1",
		Attachments: []contracts.Attachment{{Filename: "x.png", URL: cdn.URL + "/x.png"}},
	}
	if paths := ResolveAttachments(context.Background(), cdn.Client(), m, sess, hostsFor(t, cdn)); len(paths) != 0 {
		t.Fatalf("redirect to off-allowlist host must be rejected, got paths=%v", paths)
	}
}

func TestValidateCDNURL(t *testing.T) {
	hosts := allowedHosts{"cdn.example.com": true, "media.example.com": true}
	ok := []string{
		"https://cdn.example.com/attachments/1/2/a.png",
		"https://media.example.com/attachments/1/2/a.png",
		"https://CDN.Example.COM/attachments/1/2/a.png",
	}
	for _, u := range ok {
		if err := validateCDNURL(u, hosts); err != nil {
			t.Errorf("validateCDNURL(%q) = %v, want nil", u, err)
		}
	}
	bad := []string{
		"http://cdn.example.com/a.png",
		"https://evil.com/a.png",
		"https://cdn.example.com.evil.com/a.png",
		"file:///etc/passwd",
		"://bad",
	}
	for _, u := range bad {
		if err := validateCDNURL(u, hosts); err == nil {
			t.Errorf("validateCDNURL(%q) = nil, want error", u)
		}
	}
}

func TestFetchOneRemovesPartialFileOnCopyError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "999999")
		w.WriteHeader(http.StatusOK)
		writeOrLog(w, []byte("xx"))
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				closeAndWarn(conn)
			}
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	a := contracts.Attachment{Filename: "shot.png", URL: srv.URL + "/shot.png"}
	if _, err := fetchOne(context.Background(), srv.Client(), a, "1", 0, dir, hostsFor(t, srv)); err == nil {
		t.Fatal("want copy error")
	}
	entries := mustReadDir(t, dir)
	if len(entries) != 0 {
		t.Fatalf("partial file not cleaned up: %v", entries)
	}
}

func TestResolveAttachmentsContextCancelled(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeOrLog(w, []byte("data"))
	}))
	defer srv.Close()

	sess := "ctx-cancelled"
	defer removeAllOrFail(t, attachmentDir(sess))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := contracts.Message{
		ID:          "1",
		Attachments: []contracts.Attachment{{Filename: "x.png", URL: srv.URL + "/x.png"}},
	}
	if paths := ResolveAttachments(ctx, srv.Client(), m, sess, hostsFor(t, srv)); len(paths) != 0 {
		t.Fatalf("cancelled ctx should abort the fetch, got paths=%v", paths)
	}
}

func TestFetchOneSkipsOversizedBody(t *testing.T) {
	big := maxAttachmentBytes + 100
	chunk := bytes.Repeat([]byte{'A'}, 64<<10)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for written := 0; written < big && r.Context().Err() == nil; written += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	a := contracts.Attachment{Filename: "x.png", URL: srv.URL + "/x.png"}
	p, err := fetchOne(context.Background(), srv.Client(), a, "1", 0, dir, hostsFor(t, srv))
	if err == nil {
		t.Fatalf("want error for oversized body, got path %q", p)
	}
	if entries := mustReadDir(t, dir); len(entries) != 0 {
		t.Errorf("oversized fetch left %d file(s) behind, want none", len(entries))
	}
}

func TestResolveAttachmentsSkipsDeclaredOversized(t *testing.T) {
	called := false
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		writeOrLog(w, []byte("x"))
	}))
	defer srv.Close()

	sess := "declared-oversized"
	defer removeAllOrFail(t, attachmentDir(sess))
	m := contracts.Message{ID: "1", Attachments: []contracts.Attachment{
		{Filename: "big.png", URL: srv.URL + "/big.png", Size: maxAttachmentBytes + 1},
	}}
	if paths := ResolveAttachments(context.Background(), srv.Client(), m, sess, hostsFor(t, srv)); len(paths) != 0 {
		t.Fatalf("want no paths, got %v", paths)
	}
	if called {
		t.Error("oversized attachment was fetched; pre-check should have skipped it")
	}
}

func TestSupportedPrefersContentType(t *testing.T) {
	cases := []struct {
		a    contracts.Attachment
		want bool
	}{
		{contracts.Attachment{Filename: "x.dat", ContentType: "image/png"}, true},
		{contracts.Attachment{Filename: "x.png", ContentType: "application/octet-stream"}, false},
		{contracts.Attachment{Filename: "x.jpeg"}, true},
		{contracts.Attachment{Filename: "x.txt"}, true},
		{contracts.Attachment{Filename: "x.pdf"}, true},
		{contracts.Attachment{Filename: "notes", ContentType: "text/plain; charset=utf-8"}, true},
		{contracts.Attachment{Filename: "x.md", ContentType: "TEXT/MARKDOWN"}, true},
		{contracts.Attachment{Filename: "x.pdf", ContentType: "application/zip"}, false},
		{contracts.Attachment{Filename: "x.zip"}, false},
		{contracts.Attachment{Filename: "x.exe"}, false},
		{contracts.Attachment{Filename: "noext"}, false},
	}
	for _, c := range cases {
		if got := supported(c.a); got != c.want {
			t.Errorf("supported(%+v) = %v, want %v", c.a, got, c.want)
		}
	}
}

func TestResolveAttachmentsCapsCount(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeOrLog(w, []byte("x"))
	}))
	defer srv.Close()

	sess := "caps-count"
	defer removeAllOrFail(t, attachmentDir(sess))
	var atts []contracts.Attachment
	for range maxAttachmentsPerMessage + 3 {
		atts = append(atts, contracts.Attachment{Filename: "x.png", URL: srv.URL + "/x.png"})
	}
	m := contracts.Message{ID: "1", Attachments: atts}
	paths := ResolveAttachments(context.Background(), srv.Client(), m, sess, hostsFor(t, srv))
	if len(paths) != maxAttachmentsPerMessage {
		t.Fatalf("want %d images (capped), got %d", maxAttachmentsPerMessage, len(paths))
	}
}

func TestResolveAttachmentsNothingToResolveNoDir(t *testing.T) {
	sess := "nothing-to-resolve-no-dir"
	dir := attachmentDir(sess)
	defer removeAllOrFail(t, dir)
	m := contracts.Message{ID: "1", Attachments: []contracts.Attachment{{Filename: "a.zip"}}}
	if paths := ResolveAttachments(context.Background(), http.DefaultClient, m, sess, nil); len(paths) != 0 {
		t.Fatalf("want no paths, got %v", paths)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("dir should not be created when nothing resolves")
	}
}

func TestResolveAttachmentsTightModes(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeOrLog(w, []byte("PNGDATA"))
	}))
	defer srv.Close()

	sess := "tight-modes"
	defer removeAllOrFail(t, attachmentDir(sess))
	m := contracts.Message{ID: "1", Attachments: []contracts.Attachment{
		{Filename: "x.png", URL: srv.URL + "/x.png"},
	}}
	paths := ResolveAttachments(context.Background(), srv.Client(), m, sess, hostsFor(t, srv))
	if len(paths) != 1 {
		t.Fatalf("want 1 path, got %v", paths)
	}
	di, err := os.Stat(attachmentDir(sess))
	if err != nil {
		t.Fatal(err)
	}
	if got := di.Mode().Perm(); got != 0o700 {
		t.Errorf("staging dir mode = %o, want 700", got)
	}
	fi, err := os.Stat(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("downloaded image mode = %o, want 600", got)
	}
}

func TestPinnedDialRefusesNonRoutableAddress(t *testing.T) {
	dial := pinnedDial(&net.Dialer{Timeout: 2 * time.Second})
	if _, err := dial(context.Background(), "tcp", "localhost:80"); err == nil {
		t.Fatal("a name resolving to loopback must not be dialled")
	}
}

func TestPinnedDialAllowsIPLiteral(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer closeAndWarn(ln)
	c, err := pinnedDial(&net.Dialer{Timeout: 2 * time.Second})(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("ip literal must be dialled as given: %v", err)
	}
	closeAndWarn(c)
}

func TestRoutable(t *testing.T) {
	no := []string{
		"127.0.0.1", "::1", "10.0.0.1", "192.168.1.1", "172.16.0.1",
		"169.254.169.254", "0.0.0.0", "fe80::1", "ff02::1", "100.64.0.1",
	}
	for _, s := range no {
		if routable(net.ParseIP(s)) {
			t.Errorf("routable(%s) = true, want false", s)
		}
	}
	for _, s := range []string{"1.1.1.1", "93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946", "99.0.0.1", "128.0.0.1"} {
		if !routable(net.ParseIP(s)) {
			t.Errorf("routable(%s) = false, want true", s)
		}
	}
}

func TestSanitizePreventsEscape(t *testing.T) {
	for _, in := range []string{"../../etc/passwd", "/abs/path", "..", "a/b/c.png"} {
		got := sanitize(in)
		if filepath.Base(got) != got || got == ".." || got == "" {
			t.Errorf("sanitize(%q) = %q is unsafe", in, got)
		}
	}
	if got := sanitize("..."); got != "file" {
		t.Errorf(`sanitize("...") = %q, want "file"`, got)
	}
	if got := sanitize(".hidden"); got != "hidden" {
		t.Errorf(`sanitize(".hidden") = %q, want "hidden"`, got)
	}
}
