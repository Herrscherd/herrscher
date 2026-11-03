package skills

import (
	"regexp"
	"strings"
)

var useMarker = regexp.MustCompile(`(?i)<\s*use-skill\s*>\s*([^<]+?)\s*<\s*/\s*use-skill\s*>`)

// Engine runs progressive disclosure for one session: it holds the discovered
// skills and the set the model has activated. Menu is injected every turn (cheap
// name+description lines); Expansions carries the full body of activated skills.
type Engine struct {
	roots  []string
	byName map[string]Skill
	order  []string
	active map[string]bool
	byPath map[string]Skill
	bodies map[string]string
}

// NewEngine discovers skills under roots and returns an engine over them. An
// engine with no skills is a valid no-op (empty Menu/Expansions).
func NewEngine(roots []string) *Engine {
	engine := &Engine{roots: roots, active: make(map[string]bool), bodies: make(map[string]string)}
	engine.load()
	return engine
}

func (e *Engine) load() {
	found, reread := discover(e.roots, e.byPath)
	e.byName = make(map[string]Skill)
	e.byPath = make(map[string]Skill, len(found))
	e.order = e.order[:0]
	for _, skill := range found {
		e.byName[skill.Name] = skill
		e.byPath[skill.bodyPath] = skill
		e.order = append(e.order, skill.Name)
	}
	for _, p := range reread {
		delete(e.bodies, p)
	}
	for name := range e.bodies {
		if _, ok := e.byPath[name]; !ok {
			delete(e.bodies, name)
		}
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
	var buf strings.Builder
	buf.WriteString("<skills data-only=\"true\">\n")
	buf.WriteString("Skills available for this session. To use one, output the marker <use-skill>NAME</use-skill> in your reply; its full instructions arrive next turn.\n")
	for _, name := range e.order {
		buf.WriteString("- ")
		buf.WriteString(name)
		buf.WriteString(": ")
		buf.WriteString(e.byName[name].Description)
		buf.WriteByte('\n')
	}
	buf.WriteString("</skills>")
	return buf.String()
}

// Detect activates every known skill named by a marker in reply and returns
// those names, in order of first appearance and without repeats. Unknown names
// are ignored and never returned: the caller records usage against what it gets
// back, and a name the engine does not know names nothing to record.
func (e *Engine) Detect(reply string) []string {
	var used []string
	seen := make(map[string]bool)
	for _, m := range useMarker.FindAllStringSubmatch(reply, -1) {
		name := strings.TrimSpace(m[1])
		if _, ok := e.byName[name]; !ok || seen[name] {
			continue
		}
		seen[name] = true
		e.active[name] = true
		used = append(used, name)
	}
	return used
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
	var buf strings.Builder
	for _, name := range e.order {
		if !e.active[name] {
			continue
		}
		skill := e.byName[name]
		body, ok := e.bodies[skill.bodyPath]
		if !ok {
			var err error
			if body, err = skill.Body(); err != nil {
				continue
			}
			e.bodies[skill.bodyPath] = body
		}
		buf.WriteString("<skill name=\"")
		buf.WriteString(name)
		buf.WriteString("\" dir=\"")
		buf.WriteString(skill.Dir)
		buf.WriteString("\">\n")
		buf.WriteString(body)
		buf.WriteString("\n</skill>\n")
	}
	return buf.String()
}
