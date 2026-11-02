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

	contracts "github.com/Herrscherd/herrscher-contracts"
)

var (
	errNotFileURL        = errors.New("not a file url")
	errHostedFileURL     = errors.New("file url must be host-less")
	errEmptyFilePath     = errors.New("empty file path")
	errNotRegularFile    = errors.New("not a regular file")
	errTooLarge          = errors.New("exceeds the size cap")
	errNotStaged         = errors.New("not staged")
	errTooManyRedirects  = errors.New("too many redirects")
	errNoRoutableAddress = errors.New("no routable address")
	errNotRoutable       = errors.New("not routable")
	errUnexpectedStatus  = errors.New("unexpected status")
	errNotAllowedCDN     = errors.New("not an allowed CDN https url")
)

func supportedExts() map[string]bool {
	return map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
		".webp": true, ".bmp": true,
		".pdf": true, ".txt": true, ".md": true, ".markdown": true,
		".csv": true, ".tsv": true, ".log": true, ".json": true,
		".yaml": true, ".yml": true, ".toml": true, ".xml": true,
	}
}

func supportedTypes() map[string]bool {
	return map[string]bool{
		"application/pdf": true, "application/json": true,
		"application/xml": true, "application/toml": true,
		"application/x-yaml": true, "application/yaml": true,
	}
}

func supported(a contracts.Attachment) bool {
	if a.ContentType != "" {
		return supportedType(a.ContentType)
	}
	return supportedExts()[strings.ToLower(filepath.Ext(a.Filename))]
}

func supportedType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	if strings.HasPrefix(ct, "image/") || strings.HasPrefix(ct, "text/") {
		return true
	}
	return supportedTypes()[ct]
}

const maxAttachmentBytes = 10 << 20

const maxAttachmentsPerMessage = 8

type allowedHosts map[string]bool

// StagingRoot is the one tree an attachment may live in on local disk. Every
// gateway that hands the host a local file stages it under here, and the bridge's
// own downloads land here too, so "the gateway staged this file" is an invariant
// the core can check rather than a promise it has to take on faith. It is exported
// so a gateway names the same tree the core enforces instead of guessing at it.
func StagingRoot() string { return filepath.Join(os.TempDir(), "herrscher-attachments") }

func attachmentDir(session string) string {
	name := session
	if name == "" {
		name = "default"
	}
	return filepath.Join(StagingRoot(), sanitize(name))
}

// ResolveAttachments turns a message's attachments into local file paths a
// backend can reference: images to look at, PDFs and text to read. file://
// attachments are validated and passed through (the gateway already staged them
// on local disk, e.g. the terminal TUI's clipboard paste); every other (https
// CDN) attachment is downloaded through the SSRF allowlist. Unsupported,
// oversized, missing, off-allowlist, and beyond-cap attachments are skipped so a
// turn is never lost over a file, but every skip is logged with its reason: an
// attachment that vanishes without a word is how an empty allowlist went
// unnoticed long enough to drop every screenshot ever sent. Order is preserved;
// at most maxAttachmentsPerMessage files are attempted (a candidate that fails to
// resolve still counts against the cap).
//
// It is the host-side entry point (the turnloop has the Message; the bridge only
// sees Events), producing the paths carried in Event.Attachments.
//
// SECURITY: a file:// attachment is a local-file read into the model context, so
// it is pinned to the staging root; a gateway must copy a file there before it
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
	staged := false
	var slots [maxAttachmentsPerMessage]string
	var fetches sync.WaitGroup
	n := 0
	for i, a := range m.Attachments {
		if !attachmentAdmissible(a) {
			continue
		}
		if n == maxAttachmentsPerMessage {
			logger.Warn("attachments past the per-message cap were skipped",
				"cap", maxAttachmentsPerMessage, "first_skipped", a.Filename)
			break
		}
		slot := n
		n++
		if strings.HasPrefix(a.URL, "file://") {
			slots[slot] = localSlot(a)
			continue
		}
		if !staged {
			if !stagingReady(dir) {
				continue
			}
			staged = true
		}
		fetches.Add(1)
		go func(slot, i int, a contracts.Attachment) {
			defer fetches.Done()
			slots[slot] = fetchSlot(ctx, client, a, m.ID, i, dir, hosts)
		}(slot, i, a)
	}
	fetches.Wait()
	return nonEmpty(slots[:n])
}

func attachmentAdmissible(a contracts.Attachment) bool {
	if !supported(a) {
		logger.Warn("attachment skipped: not a type the model can be handed",
			"file", a.Filename, "type", a.ContentType)
		return false
	}
	if a.Size > 0 && a.Size > maxAttachmentBytes {
		logger.Warn("attachment skipped: bigger than the cap",
			"file", a.Filename, "bytes", a.Size, "cap", maxAttachmentBytes)
		return false
	}
	return true
}

func localSlot(a contracts.Attachment) string {
	p, err := localAttachmentPath(a)
	if err != nil {
		logger.Warn("attachment skipped", "file", a.Filename, "err", err)
		return ""
	}
	return p
}

func stagingReady(dir string) bool {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		logger.Warn("no attachment can be staged for this turn", "dir", dir, "err", err)
		return false
	}
	return true
}

func fetchSlot(ctx context.Context, client *http.Client, a contracts.Attachment, msgID string, i int, dir string, hosts map[string]bool) string {
	p, err := fetchOne(ctx, client, a, msgID, i, dir, hosts)
	if err != nil {
		logger.Warn("attachment skipped", "file", a.Filename, "err", err)
		return ""
	}
	return p
}

func nonEmpty(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func localAttachmentPath(a contracts.Attachment) (string, error) {
	u, err := url.Parse(a.URL)
	if err != nil || u.Scheme != "file" {
		return "", fmt.Errorf("attachment %q: %w", a.URL, errNotFileURL)
	}
	if u.Host != "" && u.Host != "localhost" {
		return "", fmt.Errorf("attachment %q: %w", a.URL, errHostedFileURL)
	}
	if u.Path == "" {
		return "", fmt.Errorf("attachment %q: %w", a.URL, errEmptyFilePath)
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
		return "", fmt.Errorf("attachment %q: %w", a.URL, errNotRegularFile)
	}
	if info.Size() > maxAttachmentBytes {
		return "", fmt.Errorf("attachment %q: %w of %d bytes", a.URL, errTooLarge, maxAttachmentBytes)
	}
	return path, nil
}

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
		return "", fmt.Errorf("%s is %w under %s", p, errNotStaged, root)
	}
	return real, nil
}

func pinnedClient(client *http.Client, hosts allowedHosts) *http.Client {
	pinned := *client
	pinned.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errTooManyRedirects
		}
		return validateCDNURL(r.URL.String(), hosts)
	}
	tr, ok := pinned.Transport.(*http.Transport)
	if ok || pinned.Transport == nil {
		if tr == nil {
			if dt, ok := http.DefaultTransport.(*http.Transport); ok {
				tr = dt
			}
		}
		if tr != nil {
			pinned.Transport = pinnedTransport(tr)
		}
	}
	return &pinned
}

var pinnedTransports sync.Map

func pinnedTransport(src *http.Transport) *http.Transport {
	if v, ok := pinnedTransports.Load(src); ok {
		if tr, ok := v.(*http.Transport); ok {
			return tr
		}
	}
	tr := src.Clone()
	tr.DialContext = pinnedDial(&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second})
	stored, _ := pinnedTransports.LoadOrStore(src, tr)
	if cached, ok := stored.(*http.Transport); ok {
		return cached
	}
	return tr
}

func pinnedDial(d *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if net.ParseIP(host) != nil {
			return d.DialContext(ctx, network, addr)
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		last := fmt.Errorf("attachment host %q: %w", host, errNoRoutableAddress)
		for _, ip := range ips {
			if !routable(ip.IP) {
				last = fmt.Errorf("attachment host %q resolves to %s: %w", host, ip.IP, errNotRoutable)
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

func routable(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
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
	defer closeAndWarn(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %w %d", a.Filename, errUnexpectedStatus, resp.StatusCode)
	}
	dest := filepath.Join(dir, fmt.Sprintf("%s-%d-%s", sanitize(msgID), idx, sanitize(a.Filename)))
	f, err := os.OpenFile(filepath.Clean(dest), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", dest, err)
	}
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, maxAttachmentBytes+1))
	closeErr := f.Close()
	if copyErr != nil {
		return discardPartial(dest, fmt.Errorf("download %s: %w", a.Filename, copyErr))
	}
	if closeErr != nil {
		return discardPartial(dest, fmt.Errorf("write %s: %w", dest, closeErr))
	}
	if n > maxAttachmentBytes {
		return discardPartial(dest, fmt.Errorf("download %s: %w of %d bytes", a.Filename, errTooLarge, maxAttachmentBytes))
	}
	return dest, nil
}

func discardPartial(dest string, err error) (string, error) {
	if rmErr := os.Remove(dest); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
		logger.Warn("could not remove a partial attachment download", "path", dest, "err", rmErr)
	}
	return "", err
}

func validateCDNURL(raw string, hosts allowedHosts) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("attachment url %q: %w", unsigned(raw), err)
	}
	if u.Scheme != "https" || !hosts[strings.ToLower(u.Hostname())] {
		return fmt.Errorf("attachment url %q: %w", unsigned(raw), errNotAllowedCDN)
	}
	return nil
}

func unsigned(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i]
	}
	return raw
}

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
