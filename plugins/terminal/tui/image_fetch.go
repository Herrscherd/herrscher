package tui

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

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

func hostAllowed(host string, allow []string) bool {
	for _, h := range allow {
		if strings.EqualFold(strings.TrimSpace(h), host) {
			return true
		}
	}
	return false
}

var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
}

func imageURLs(text string) []string {
	var out []string
	for _, raw := range linkURLPattern.FindAllString(text, -1) {
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

type imageReadyMsg struct {
	channel string
	entry   int
	entryID uint64
	escape  string
}

func (m *model) fetchEntryImages(tb *tab, idx int) tea.Cmd {
	if m.imageFetcher == nil || len(m.imageHosts) == 0 || idx < 0 || idx >= len(tb.entries) {
		return nil
	}
	var cmds []tea.Cmd
	ctx := m.runContext()
	channel, caps, get, allow := tb.channel, m.caps, m.imageFetcher, m.imageHosts
	entryID := tb.entries[idx].id
	for _, raw := range imageURLs(tb.entries[idx].text) {
		if !hostAllowed(strings.ToLower(urlHost(raw)), allow) {
			continue
		}
		cmds = append(cmds, func() tea.Msg {
			data, err := fetchImage(ctx, get, allow, raw)
			if err != nil {
				return nil
			}
			img, err := decodeImage(data)
			if err != nil {
				return nil
			}
			esc := boundedEscape(img, caps)
			if esc == "" {
				return nil
			}
			return imageReadyMsg{channel: channel, entry: idx, entryID: entryID, escape: esc}
		})
	}
	return tea.Batch(cmds...)
}

func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func (m *model) applyImage(msg imageReadyMsg) {
	tb := m.tabs[msg.channel]
	if tb == nil || msg.entry < 0 || msg.entry >= len(tb.entries) || msg.escape == "" {
		return
	}
	if tb.entries[msg.entry].id != msg.entryID {
		return
	}
	e := &tb.entries[msg.entry]
	if e.preview != "" {
		e.preview += "\n"
	}
	e.preview += msg.escape
	m.tsCache.valid = false
	m.syncViewport()
}
