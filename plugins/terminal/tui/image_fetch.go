package tui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

var (
	errNotFetchable   = errors.New("is not fetchable")
	errNoHost         = errors.New("names no host")
	errNotAllowlisted = errors.New("is not on the attachment allowlist")
)

func fetchImage(ctx context.Context, get func(context.Context, string) ([]byte, error), allow []string, rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse image url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("image url scheme %q %w", parsed.Scheme, errNotFetchable)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return nil, fmt.Errorf("image url %q %w", rawURL, errNoHost)
	}
	if !hostAllowed(host, allow) {
		return nil, fmt.Errorf("image host %s %w", host, errNotAllowlisted)
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

func imageExts() map[string]bool {
	return map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, extGIF: true, ".webp": true,
	}
}

func imageURLs(text string) []string {
	var out []string
	for _, raw := range linkURLPattern.FindAllString(text, -1) {
		parsed, err := url.Parse(raw)
		if err != nil {
			continue
		}
		if imageExts()[strings.ToLower(path.Ext(parsed.Path))] {
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

func (m *model) fetchEntryImages(tab *tab, idx int) tea.Cmd {
	if m.imageFetcher == nil || len(m.imageHosts) == 0 || idx < 0 || idx >= len(tab.entries) {
		return nil
	}
	var cmds []tea.Cmd
	ctx := m.runContext()
	channel, caps, get, allow := tab.channel, m.caps, m.imageFetcher, m.imageHosts
	entryID := tab.entries[idx].id
	for _, raw := range imageURLs(tab.entries[idx].text) {
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
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func (m *model) applyImage(msg imageReadyMsg) {
	tab := m.tabs[msg.channel]
	if tab == nil || msg.entry < 0 || msg.entry >= len(tab.entries) || msg.escape == "" {
		return
	}
	if tab.entries[msg.entry].id != msg.entryID {
		return
	}
	failure := &tab.entries[msg.entry]
	if failure.preview != "" {
		failure.preview += "\n"
	}
	failure.preview += msg.escape
	m.tsCache.valid = false
	m.syncViewport()
}
