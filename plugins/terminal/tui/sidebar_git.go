package tui

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	gitRefreshEvery = 3 * time.Second
	gitCommandLimit = 2 * time.Second
)

type lineStat struct {
	added   int
	removed int
}

type gitSnapshot struct {
	branch  string
	changes map[string]lineStat
}

type gitCacheEntry struct {
	requested time.Time
	snap      gitSnapshot
}

type gitRefreshMsg struct {
	dir  string
	snap gitSnapshot
}

func (m *model) gitStateOf(dir string) gitSnapshot {
	return m.gitCache[dir].snap
}

func (m *model) requestGit(dir string, force bool) tea.Cmd {
	if dir == "" {
		return nil
	}
	if m.gitCache == nil {
		m.gitCache = map[string]gitCacheEntry{}
	}
	entry := m.gitCache[dir]
	if !force && time.Since(entry.requested) < gitRefreshEvery {
		return nil
	}
	entry.requested = time.Now()
	m.gitCache[dir] = entry
	return func() tea.Msg {
		return gitRefreshMsg{dir: dir, snap: readGitSnapshot(dir)}
	}
}

func (m *model) storeGit(msg gitRefreshMsg) {
	if m.gitCache == nil {
		m.gitCache = map[string]gitCacheEntry{}
	}
	entry := m.gitCache[msg.dir]
	entry.snap = msg.snap
	m.gitCache[msg.dir] = entry
}

func (m *model) gitTick() tea.Cmd {
	info, _ := m.activeInfo()
	return m.requestGit(info.Dir, false)
}

func (m *model) gitAfterTool(text string) tea.Cmd {
	if family, _ := toolVerb(text); family != familyWrite {
		return nil
	}
	info, _ := m.activeInfo()
	return m.requestGit(info.Dir, true)
}

func readGitSnapshot(dir string) gitSnapshot {
	branch, err := gitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return gitSnapshot{}
	}
	numstat, _ := gitOutput(dir, "diff", "--numstat", "--relative", "HEAD")
	return gitSnapshot{branch: strings.TrimSpace(branch), changes: parseNumstat(numstat)}
}

func gitOutput(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitCommandLimit)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	return string(out), err
}

func parseNumstat(out string) map[string]lineStat {
	stats := map[string]lineStat{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		added, errAdded := strconv.Atoi(fields[0])
		removed, errRemoved := strconv.Atoi(fields[1])
		if errAdded != nil || errRemoved != nil {
			continue
		}
		stats[fields[2]] = lineStat{added: added, removed: removed}
	}
	return stats
}

func fileStat(stats map[string]lineStat, file, dir string) (lineStat, bool) {
	stat, ok := stats[relativeTo(dir, file)]
	return stat, ok
}

func relativeTo(dir, file string) string {
	if dir == "" || !filepath.IsAbs(file) {
		return file
	}
	rel, err := filepath.Rel(dir, file)
	if err != nil || strings.HasPrefix(rel, "..") {
		return file
	}
	return rel
}
