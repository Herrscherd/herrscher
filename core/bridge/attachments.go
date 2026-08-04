package bridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Herrscherd/herrscher-contracts"
)

// supportedExts are the filename extensions accepted when an attachment carries
// no content-type: images to look at, documents to read. It is an allowlist, so
// an archive or a binary is skipped rather than staged on the operator's disk.
var supportedExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".bmp": true,
	".pdf": true, ".txt": true, ".md": true, ".markdown": true,
	".csv": true, ".tsv": true, ".log": true, ".json": true,
	".yaml": true, ".yml": true, ".toml": true, ".xml": true,
}

// supportedTypes are the non-text, non-image media types accepted by name. Any
// text/* or image/* type is accepted by prefix instead.
var supportedTypes = map[string]bool{
	"application/pdf": true, "application/json": true,
	"application/xml": true, "application/toml": true,
	"application/x-yaml": true, "application/yaml": true,
}

// supported reports whether the bridge will resolve an attachment. It prefers the
// declared content-type and falls back to the filename extension, so a file with
// an odd or missing extension is still recognized.
func supported(a contracts.Attachment) bool {
	if a.ContentType != "" {
		return supportedType(a.ContentType)
	}
	return supportedExts[strings.ToLower(filepath.Ext(a.Filename))]
}

func supportedType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	// "text/plain; charset=utf-8" — the parameters say nothing about whether we
	// can handle the type, and they break an exact match.
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	if strings.HasPrefix(ct, "image/") || strings.HasPrefix(ct, "text/") {
		return true
	}
	return supportedTypes[ct]
}

// maxAttachmentBytes bounds a single downloaded attachment. Anything larger is
// skipped: the bridge must never let an oversized upload stall or OOM a turn.
const maxAttachmentBytes = 10 << 20 // 10 MiB

// maxAttachmentsPerMessage caps how many files one message can pull down, so an
// author can't fan a single message into an unbounded number of fetches/files.
const maxAttachmentsPerMessage = 8

// allowedHosts is the SSRF allowlist for attachment downloads: the caller (the
// gateway that produced the message) supplies the CDN hosts its attachments may
// point at, so the core pins host/scheme without knowing any concrete platform.
// A gateway populates attachments[].url itself, but we still pin it so a future
// change (or a spoofed field) can't turn this into an SSRF primitive.
type allowedHosts map[string]bool

// StagingRoot is the one tree an attachment may live in on local disk. Every
// gateway that hands the host a local file stages it under here, and the bridge's
// own downloads land here too, so "the gateway staged this file" is an invariant
// the core can check rather than a promise it has to take on faith. It is exported
// so a gateway names the same tree the core enforces instead of guessing at it.
func StagingRoot() string { return filepath.Join(os.TempDir(), "herrscher-attachments") }

// attachmentDir is where downloads land, namespaced per session so concurrent
// bridges don't collide.
func attachmentDir(session string) string {
	name := session
	if name == "" {
		name = "default"
	}
	return filepath.Join(StagingRoot(), sanitize(name))
}

// ResolveAttachments turns a message's attachments into local file paths a
// backend can reference — images to look at, PDFs and text to read. file://
// attachments are validated and passed through (the gateway already staged them
// on local disk — e.g. the terminal TUI's clipboard paste); every other (https
// CDN) attachment is downloaded through the SSRF allowlist. Unsupported,
// oversized, missing, off-allowlist, and beyond-cap attachments are skipped so a
// turn is never lost over a file. Order is preserved; at most
// maxAttachmentsPerMessage files are attempted (a candidate that fails to resolve
// still counts against the cap).
//
// It is the host-side entry point (the turnloop has the Message; the bridge only
// sees Events), producing the paths carried in Event.Attachments.
//
// SECURITY: a file:// attachment is a local-file read into the model context, so
// it is pinned to the staging root — a gateway must copy a file there before it
// can name it, and a path outside is refused. A gateway that forwards attachment
// URLs influenced by a remote author must still use https rather than staging
// whatever it is handed, so the SSRF allowlist applies. What a resolved file
// *contains* is untrusted either way: the backend is handed a path, and the text
// inside a document an author uploaded is that author's words, not instructions.
func ResolveAttachments(ctx context.Context, client *http.Client, m contracts.Message, session string, hosts map[string]bool) []string {
	if client == nil {
		client = http.DefaultClient
	}
	client = pinnedClient(client, hosts)
	dir := attachmentDir(session)
	mkdirDone := false
	out := make([]string, 0, maxAttachmentsPerMessage)
	n := 0
	for i, a := range m.Attachments {
		if !supported(a) || (a.Size > 0 && a.Size > maxAttachmentBytes) {
			continue
		}
		if n == maxAttachmentsPerMessage {
			break
		}
		n++
		if strings.HasPrefix(a.URL, "file://") {
			if p, err := localAttachmentPath(a); err == nil {
				out = append(out, p)
			}
			continue
		}
		if !mkdirDone {
			// 0700: a downloaded attachment is one operator's private material, and
			// the staging tree lives in a world-writable temp dir.
			if err := os.MkdirAll(dir, 0o700); err != nil {
				continue
			}
			mkdirDone = true
		}
		if p, err := fetchOne(ctx, client, a, m.ID, i, dir, hosts); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// localAttachmentPath validates a file:// attachment already staged on local
// disk and returns its path, rejecting non-file URLs, non-regular files, and
// oversized ones so a crafted file:// url can't smuggle a device node or huge
// file into a turn. The gateway owns the file's lifetime; the bridge only reads it.
func localAttachmentPath(a contracts.Attachment) (string, error) {
	u, err := url.Parse(a.URL)
	if err != nil || u.Scheme != "file" {
		return "", fmt.Errorf("attachment %q: not a file url", a.URL)
	}
	// Pin to a local, host-less file URL so file://evil/etc/passwd (whose Path is
	// /etc/passwd) can't slip through — a genuine local file URL has no host.
	if u.Host != "" && u.Host != "localhost" {
		return "", fmt.Errorf("attachment %q: file url must be host-less", a.URL)
	}
	if u.Path == "" {
		return "", fmt.Errorf("attachment %q: empty file path", a.URL)
	}
	path, err := stagedPath(u.Path)
	if err != nil {
		return "", fmt.Errorf("attachment %q: %w", a.URL, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("attachment %q: %w", a.URL, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("attachment %q: not a regular file", a.URL)
	}
	if info.Size() > maxAttachmentBytes {
		return "", fmt.Errorf("attachment %q: exceeds %d bytes", a.URL, maxAttachmentBytes)
	}
	return path, nil
}

// stagedPath confirms a local path really sits inside the staging root and
// returns it resolved. Symlinks are resolved on both sides before comparing:
// /tmp is itself a symlink on macOS, and a symlink planted inside the staging
// tree would otherwise read any file on the machine into the model's context.
func stagedPath(p string) (string, error) {
	root, err := filepath.EvalSymlinks(StagingRoot())
	if err != nil {
		return "", fmt.Errorf("staging root: %w", err)
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is not staged under %s", p, root)
	}
	return real, nil
}

// pinnedClient returns a copy of client — the caller's shared client is left
// untouched — that enforces the allowlist all the way down to the socket.
//
// Two holes are closed. Redirects: the default client follows them blindly, so an
// allowlisted CDN that 302s to an internal host would defeat the pin; every hop is
// re-validated. And the name/address gap: validateCDNURL checks a *name*, while
// the connection goes to whatever that name resolves to — which DNS can answer
// differently the second time, or answer 127.0.0.1 the first. The dialer resolves
// once, refuses anything not publicly routable, and connects to the exact address
// it checked.
//
// A caller supplying a RoundTripper that is not an *http.Transport owns its own
// dialing and keeps it: there is no dialer to wrap. Only the tests do that.
func pinnedClient(client *http.Client, hosts allowedHosts) *http.Client {
	pinned := *client
	pinned.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return validateCDNURL(r.URL.String(), hosts)
	}
	tr, ok := pinned.Transport.(*http.Transport)
	if ok || pinned.Transport == nil {
		if tr == nil {
			tr, _ = http.DefaultTransport.(*http.Transport)
		}
		if tr != nil {
			pinned.Transport = pinnedTransport(tr)
		}
	}
	return &pinned
}

// pinnedTransports memoizes the wrapped transport per source transport. Without
// it every message with an attachment would get a freshly cloned transport, hence
// a fresh connection pool: no reuse, and a set of idle sockets left to time out.
var pinnedTransports sync.Map // *http.Transport -> *http.Transport

func pinnedTransport(src *http.Transport) *http.Transport {
	if v, ok := pinnedTransports.Load(src); ok {
		return v.(*http.Transport)
	}
	tr := src.Clone()
	tr.DialContext = pinnedDial(&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second})
	v, _ := pinnedTransports.LoadOrStore(src, tr)
	return v.(*http.Transport)
}

// pinnedDial resolves the host itself and dials the literal address it approved,
// so no second lookup can land the connection somewhere else.
func pinnedDial(d *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		// An allowlist entry that is already a literal IP was pinned exactly: there
		// is no name left to re-resolve, so there is nothing to rebind. Whoever put
		// an address in the allowlist meant that address.
		if net.ParseIP(host) != nil {
			return d.DialContext(ctx, network, addr)
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		last := fmt.Errorf("attachment host %q has no routable address", host)
		for _, ip := range ips {
			if !routable(ip.IP) {
				last = fmt.Errorf("attachment host %q resolves to %s, which is not routable", host, ip.IP)
				continue
			}
			c, err := d.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return c, nil
			}
			last = err
		}
		return nil, last
	}
}

// routable reports whether ip is an address on the public internet — the only
// kind a CDN legitimately lives on. Loopback, private, link-local, carrier-NAT
// and multicast are where an SSRF wants to land, never where an attachment is.
func routable(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// 100.64.0.0/10 is neither private nor public: behind carrier NAT it reaches
	// other tenants of the same ISP, and Go does not count it as private.
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return false
	}
	return true
}

func fetchOne(ctx context.Context, client *http.Client, a contracts.Attachment, msgID string, idx int, dir string, hosts allowedHosts) (string, error) {
	if err := validateCDNURL(a.URL, hosts); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", fmt.Errorf("attachment request %s: %w", a.Filename, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", a.Filename, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: status %d", a.Filename, resp.StatusCode)
	}
	// Include the per-message index so two same-named images on one message don't
	// clobber each other (msgID alone collides within a message).
	dest := filepath.Join(dir, fmt.Sprintf("%s-%d-%s", msgID, idx, sanitize(a.Filename)))
	// 0600 rather than os.Create's 0666&umask: this is one operator's material
	// sitting in a directory every user on the machine can walk into.
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", dest, err)
	}
	// Bound the copy so a server lying about Size can't exhaust the disk. Read one
	// byte past the cap so an oversized body is detected and skipped rather than
	// silently truncated into a corrupt-but-valid file.
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, maxAttachmentBytes+1))
	closeErr := f.Close()
	if copyErr != nil {
		return discardPartial(dest, fmt.Errorf("download %s: %w", a.Filename, copyErr))
	}
	if closeErr != nil {
		return discardPartial(dest, fmt.Errorf("write %s: %w", dest, closeErr))
	}
	if n > maxAttachmentBytes {
		return discardPartial(dest, fmt.Errorf("download %s: exceeds %d bytes", a.Filename, maxAttachmentBytes))
	}
	return dest, nil
}

// discardPartial deletes a half-written download and hands back the failure that
// caused it. The removal's own error is logged rather than swallowed: what is
// left behind is a truncated image sitting in the session's attachment dir, and
// the next turn would happily hand it to the backend as if it were whole.
func discardPartial(dest string, err error) (string, error) {
	if rmErr := os.Remove(dest); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
		logger.Warn("could not remove a partial attachment download", "path", dest, "err", rmErr)
	}
	return "", err
}

// validateCDNURL pins an attachment URL to https on one of the caller-supplied
// allowlist hosts, rejecting anything else before it is fetched.
func validateCDNURL(raw string, hosts allowedHosts) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("attachment url %q: %w", raw, err)
	}
	// A host name is case-insensitive; the allowlist is keyed lowercase.
	if u.Scheme != "https" || !hosts[strings.ToLower(u.Hostname())] {
		return fmt.Errorf("attachment url %q: not an allowed CDN https url", raw)
	}
	return nil
}

// sanitize keeps a path component to a safe, flat token so a crafted filename or
// session name can't escape the attachment directory.
func sanitize(s string) string {
	s = filepath.Base(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.TrimLeft(b.String(), ".")
	if out == "" {
		return "file"
	}
	return out
}
