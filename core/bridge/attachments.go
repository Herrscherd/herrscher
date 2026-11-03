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

func supported(att contracts.Attachment) bool {
	if att.ContentType != "" {
		return supportedType(att.ContentType)
	}
	return supportedExts()[strings.ToLower(filepath.Ext(att.Filename))]
}

func supportedType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.IndexByte(contentType, ';'); i >= 0 {
		contentType = strings.TrimSpace(contentType[:i])
	}
	if strings.HasPrefix(contentType, "image/") || strings.HasPrefix(contentType, "text/") {
		return true
	}
	return supportedTypes()[contentType]
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
func ResolveAttachments(ctx context.Context, client *http.Client, msg contracts.Message, session string, hosts map[string]bool) []string {
	if client == nil {
		client = http.DefaultClient
	}
	client = pinnedClient(client, hosts)
	dir := attachmentDir(session)
	staged := false
	var slots [maxAttachmentsPerMessage]string
	var fetches sync.WaitGroup
	count := 0
	for idx, att := range msg.Attachments {
		if !attachmentAdmissible(att) {
			continue
		}
		if count == maxAttachmentsPerMessage {
			logger.Warn("attachments past the per-message cap were skipped",
				"cap", maxAttachmentsPerMessage, "first_skipped", att.Filename)
			break
		}
		slot := count
		count++
		if strings.HasPrefix(att.URL, "file://") {
			slots[slot] = localSlot(att)
			continue
		}
		if !staged {
			if !stagingReady(dir) {
				continue
			}
			staged = true
		}
		fetches.Add(1)
		go func(slot, index int, att contracts.Attachment) {
			defer fetches.Done()
			slots[slot] = fetchSlot(ctx, client, att, msg.ID, index, dir, hosts)
		}(slot, idx, att)
	}
	fetches.Wait()
	return nonEmpty(slots[:count])
}

func attachmentAdmissible(att contracts.Attachment) bool {
	if !supported(att) {
		logger.Warn("attachment skipped: not a type the model can be handed",
			"file", att.Filename, "type", att.ContentType)
		return false
	}
	if att.Size > 0 && att.Size > maxAttachmentBytes {
		logger.Warn("attachment skipped: bigger than the cap",
			"file", att.Filename, "bytes", att.Size, "cap", maxAttachmentBytes)
		return false
	}
	return true
}

func localSlot(att contracts.Attachment) string {
	path, err := localAttachmentPath(att)
	if err != nil {
		logger.Warn("attachment skipped", "file", att.Filename, "err", err)
		return ""
	}
	return path
}

func stagingReady(dir string) bool {
	if err := os.MkdirAll(dir, dirPrivate); err != nil {
		logger.Warn("no attachment can be staged for this turn", "dir", dir, "err", err)
		return false
	}
	return true
}

func fetchSlot(ctx context.Context, client *http.Client, att contracts.Attachment, msgID string, i int, dir string, hosts map[string]bool) string {
	path, err := fetchOne(ctx, client, att, msgID, i, dir, hosts)
	if err != nil {
		logger.Warn("attachment skipped", "file", att.Filename, "err", err)
		return ""
	}
	return path
}

func nonEmpty(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if path != "" {
			out = append(out, path)
		}
	}
	return out
}

func localAttachmentPath(att contracts.Attachment) (string, error) {
	parsed, err := url.Parse(att.URL)
	if err != nil || parsed.Scheme != "file" {
		return "", fmt.Errorf("attachment %q: %w", att.URL, errNotFileURL)
	}
	if parsed.Host != "" && parsed.Host != "localhost" {
		return "", fmt.Errorf("attachment %q: %w", att.URL, errHostedFileURL)
	}
	if parsed.Path == "" {
		return "", fmt.Errorf("attachment %q: %w", att.URL, errEmptyFilePath)
	}
	path, err := stagedPath(parsed.Path)
	if err != nil {
		return "", fmt.Errorf("attachment %q: %w", att.URL, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("attachment %q: %w", att.URL, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("attachment %q: %w", att.URL, errNotRegularFile)
	}
	if info.Size() > maxAttachmentBytes {
		return "", fmt.Errorf("attachment %q: %w of %d bytes", att.URL, errTooLarge, maxAttachmentBytes)
	}
	return path, nil
}

func stagedPath(path string) (string, error) {
	root, err := filepath.EvalSymlinks(StagingRoot())
	if err != nil {
		return "", fmt.Errorf("staging root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is %w under %s", path, errNotStaged, root)
	}
	return resolved, nil
}

func pinnedClient(client *http.Client, hosts allowedHosts) *http.Client {
	pinned := *client
	pinned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return errTooManyRedirects
		}
		return validateCDNURL(req.URL.String(), hosts)
	}
	transport, ok := pinned.Transport.(*http.Transport)
	if ok || pinned.Transport == nil {
		if transport == nil {
			if dt, ok := http.DefaultTransport.(*http.Transport); ok {
				transport = dt
			}
		}
		if transport != nil {
			pinned.Transport = pinnedTransport(transport)
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
	transport := src.Clone()
	transport.DialContext = pinnedDial(&net.Dialer{Timeout: dialTimeout, KeepAlive: dialKeepAlive})
	stored, _ := pinnedTransports.LoadOrStore(src, transport)
	if cached, ok := stored.(*http.Transport); ok {
		return cached
	}
	return transport
}

func pinnedDial(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if net.ParseIP(host) != nil {
			return dialer.DialContext(ctx, network, addr)
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		last := fmt.Errorf("attachment host %q: %w", host, errNoRoutableAddress)
		for _, addr := range ips {
			if !routable(addr.IP) {
				last = fmt.Errorf("attachment host %q resolves to %s: %w", host, addr.IP, errNotRoutable)
				continue
			}
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}
}

func routable(addr net.IP) bool {
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsUnspecified() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() || addr.IsMulticast() {
		return false
	}
	if v4 := addr.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return false
	}
	return true
}

func fetchOne(ctx context.Context, client *http.Client, att contracts.Attachment, msgID string, idx int, dir string, hosts allowedHosts) (string, error) {
	if err := validateCDNURL(att.URL, hosts); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, att.URL, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("attachment request %s: %w", att.Filename, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", att.Filename, err)
	}
	defer closeAndWarn(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %w %d", att.Filename, errUnexpectedStatus, resp.StatusCode)
	}
	dest := filepath.Join(dir, fmt.Sprintf("%s-%d-%s", sanitize(msgID), idx, sanitize(att.Filename)))
	file, err := os.OpenFile(filepath.Clean(dest), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileSecret)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", dest, err)
	}
	written, copyErr := io.Copy(file, io.LimitReader(resp.Body, maxAttachmentBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return discardPartial(dest, fmt.Errorf("download %s: %w", att.Filename, copyErr))
	}
	if closeErr != nil {
		return discardPartial(dest, fmt.Errorf("write %s: %w", dest, closeErr))
	}
	if written > maxAttachmentBytes {
		return discardPartial(dest, fmt.Errorf("download %s: %w of %d bytes", att.Filename, errTooLarge, maxAttachmentBytes))
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
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("attachment url %q: %w", unsigned(raw), err)
	}
	if parsed.Scheme != "https" || !hosts[strings.ToLower(parsed.Hostname())] {
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

func sanitize(raw string) string {
	raw = filepath.Base(raw)
	var builder strings.Builder
	for _, char := range raw {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9',
			char == '.', char == '-', char == '_':
			_, _ = builder.WriteRune(char)
		default:
			_, _ = builder.WriteRune('_')
		}
	}
	out := strings.TrimLeft(builder.String(), ".")
	if out == "" {
		return "file"
	}
	return out
}
