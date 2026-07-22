package skills

import (
	"regexp"
	"strings"
)

// useMarker matches an activation marker the model emits to request a skill,
// tolerant of surrounding whitespace and case: <use-skill> NAME </use-skill>.
var useMarker = regexp.MustCompile(`(?i)<\s*use-skill\s*>\s*([^<]+?)\s*<\s*/\s*use-skill\s*>`)

// Engine runs progressive disclosure for one session: it holds the discovered
// skills and the set the model has activated. Menu is injected every turn (cheap
// name+description lines); Expansions carries the full body of activated skills.
type Engine struct {
	roots  []string
	byName map[string]Skill
	order  []string
	active map[string]bool
}

// NewEngine discovers skills under roots and returns an engine over them. An
// engine with no skills is a valid no-op (empty Menu/Expansions).
func NewEngine(roots []string) *Engine {
	e := &Engine{roots: roots, active: map[string]bool{}}
	e.load()
	return e
}

// load (re-)discovers the skills under the engine's roots, replacing the menu.
// The active set is preserved except for skills that have disappeared.
func (e *Engine) load() {
	e.byName = map[string]Skill{}
	e.order = e.order[:0]
	for _, s := range Discover(e.roots) {
		e.byName[s.Name] = s
		e.order = append(e.order, s.Name)
	}
	for name := range e.active {
		if _, ok := e.byName[name]; !ok {
			delete(e.active, name)
		}
	}
}

// Refresh re-scans the roots so a SKILL.md added or edited mid-session is picked
// up without restarting the session. Already-activated skills stay active.
func (e *Engine) Refresh() { e.load() }

// Menu renders the activation instruction and one line per discovered skill. It
// returns "" when no skills exist so the caller injects nothing.
func (e *Engine) Menu() string {
	if len(e.order) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<skills data-only=\"true\">\n")
	b.WriteString("Skills available for this session. To use one, output the marker <use-skill>NAME</use-skill> in your reply; its full instructions arrive next turn.\n")
	for _, name := range e.order {
		b.WriteString("- ")
		b.WriteString(name)
		b.WriteString(": ")
		b.WriteString(e.byName[name].Description)
		b.WriteByte('\n')
	}
	b.WriteString("</skills>")
	return b.String()
}

// Detect activates every known skill named by a marker in reply. Unknown names
// are ignored.
func (e *Engine) Detect(reply string) {
	for _, m := range useMarker.FindAllStringSubmatch(reply, -1) {
		name := strings.TrimSpace(m[1])
		if _, ok := e.byName[name]; ok {
			e.active[name] = true
		}
	}
}

// Strip removes activation markers from a reply so the marker (an internal
// model→engine signal) never reaches the human-visible message. Surrounding
// whitespace left by a removed marker is collapsed.
func (e *Engine) Strip(reply string) string {
	return strings.TrimSpace(useMarker.ReplaceAllString(reply, ""))
}

// Expansions returns the bodies of all active skills, each fenced with its name
// and absolute directory so the model can Read bundled files. A body that fails
// to load is skipped. Returns "" when nothing is active.
func (e *Engine) Expansions() string {
	var b strings.Builder
	for _, name := range e.order {
		if !e.active[name] {
			continue
		}
		s := e.byName[name]
		body, err := s.Body()
		if err != nil {
			continue
		}
		b.WriteString("<skill name=\"")
		b.WriteString(name)
		b.WriteString("\" dir=\"")
		b.WriteString(s.Dir)
		b.WriteString("\">\n")
		b.WriteString(body)
		b.WriteString("\n</skill>\n")
	}
	return b.String()
}
