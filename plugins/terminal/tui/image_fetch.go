package tui

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// An image posted as a link is the common case in a conversation, and the TUI
// used to render it as the characters of its URL. Fetching one means this process
// opens a connection somewhere the agent chose, which is exactly the question the
// host's attachment allowlist already answers — so this feature does not get an
// answer of its own. Off the list, the URL stays a URL.

// fetchImage retrieves rawURL's bytes through get, but only when its host is on
// allow. The fetcher is a parameter rather than an http.Client so the decision
// can be tested without a socket, and so the transport stays the caller's choice.
//
// A refusal names the host: the operator has to be able to see which one to add.
func fetchImage(ctx context.Context, get func(context.Context, string) ([]byte, error), allow []string, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse image url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("image url scheme %q is not fetchable", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return nil, fmt.Errorf("image url %q names no host", rawURL)
	}
	if !hostAllowed(host, allow) {
		return nil, fmt.Errorf("image host %s is not on the attachment allowlist", host)
	}
	data, err := get(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", host, err)
	}
	return data, nil
}

// hostAllowed matches a host against the allowlist, case-insensitively on both
// sides. An empty allowlist allows nothing — which is what a gateway declaring
// no attachment hosts (the terminal's own default) must get.
func hostAllowed(host string, allow []string) bool {
	for _, h := range allow {
		if strings.EqualFold(strings.TrimSpace(h), host) {
			return true
		}
	}
	return false
}

// urlPattern matches a bare http(s) URL, stopping before the punctuation that
// ends a sentence rather than a URL.
var urlPattern = regexp.MustCompile(`https?://[^\s<>"']+[^\s<>"'.,;:!?)\]]`)

// imageExts are the extensions worth trying to draw. Matching on the extension
// rather than on a HEAD request means the decision costs nothing and no request
// is made for a URL that was never a picture.
var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
}

// imageURLs returns the http(s) URLs in text that name an image file, in the
// order they appear.
func imageURLs(text string) []string {
	var out []string
	for _, raw := range urlPattern.FindAllString(text, -1) {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		if imageExts[strings.ToLower(path.Ext(u.Path))] {
			out = append(out, raw)
		}
	}
	return out
}

// imageReadyMsg carries a fetched, encoded image back into Update, tagged with
// the entry that referenced it. A turn that has since been cleared simply drops
// it — an image arriving late is never a reason to resurrect a transcript.
type imageReadyMsg struct {
	channel string
	entry   int
	escape  string
}

// fetchEntryImages returns the commands that fetch the images a finished entry
// links to. It yields nothing when no fetcher is wired or the allowlist is empty,
// which is the terminal gateway's own default — the feature stays dark until a
// gateway declares the hosts it serves attachments from.
//
// Every failure is silent by design: an unreachable image leaves its URL as text,
// which is exactly what the reader had before.
func (m *model) fetchEntryImages(tb *tab, idx int) tea.Cmd {
	if m.imageFetcher == nil || len(m.imageHosts) == 0 || idx < 0 || idx >= len(tb.entries) {
		return nil
	}
	var cmds []tea.Cmd
	channel, caps, get, allow := tb.channel, m.caps, m.imageFetcher, m.imageHosts
	for _, raw := range imageURLs(tb.entries[idx].text) {
		if !hostAllowed(strings.ToLower(urlHost(raw)), allow) {
			continue // refused before a command is even scheduled
		}
		cmds = append(cmds, func() tea.Msg {
			data, err := fetchImage(context.Background(), get, allow, raw)
			if err != nil {
				return nil
			}
			img, err := decodeImage(data)
			if err != nil {
				return nil
			}
			// Through the same bound a staged attachment gets: this escape lands on
			// a transcript line the viewport re-measures on every repaint, and the
			// bytes behind it came from the network rather than from the operator.
			esc := boundedEscape(img, caps)
			if esc == "" {
				return nil
			}
			return imageReadyMsg{channel: channel, entry: idx, escape: esc}
		})
	}
	return tea.Batch(cmds...)
}

// urlHost is the host of a URL that has already been matched as one, or "".
func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// applyImage attaches a fetched image under the entry that referenced it,
// ignoring a message for a tab or an entry that no longer exists.
func (m *model) applyImage(msg imageReadyMsg) {
	tb := m.tabs[msg.channel]
	if tb == nil || msg.entry < 0 || msg.entry >= len(tb.entries) || msg.escape == "" {
		return
	}
	e := &tb.entries[msg.entry]
	if e.preview != "" {
		e.preview += "\n"
	}
	e.preview += msg.escape
	m.tsCache.valid = false // the cache key cannot see a preview change
	m.syncViewport()
}
