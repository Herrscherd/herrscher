package tui

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const defaultChordTimeout = 3 * time.Second

const keyTab = "tab"

const keyDelete = "delete"

var (
	errKeyEmpty         = errors.New("touche vide")
	errKeyModifier      = errors.New("modificateur inconnu")
	errKeyUnknown       = errors.New("touche inconnue")
	errKeyUnreliable    = errors.New("touche non reliable")
	errKeyReserved      = errors.New("touche réservée par le terminal")
	errKeyChordLength   = errors.New("accord vide")
	errKeyUnknownCtx    = errors.New("contexte inconnu")
	errKeyUnknownAction = errors.New("action inconnue")
	errKeyDuplicate     = errors.New("doublon dans le contexte")
	errKeyBadValue      = errors.New("l’action doit être une chaîne ou null")
)

type keyContext string

type keyAction string

type keyTable struct {
	chords   map[keyContext]map[string]keyAction
	prefixes map[keyContext]map[string]bool
}

type keyPress struct {
	pending string
	started time.Time
}

func newKeyTable() *keyTable {
	return &keyTable{
		chords:   make(map[keyContext]map[string]keyAction),
		prefixes: make(map[keyContext]map[string]bool),
	}
}

func (t *keyTable) bind(ctx keyContext, chord string, action keyAction) {
	if t.chords[ctx] == nil {
		t.chords[ctx] = make(map[string]keyAction)
		t.prefixes[ctx] = make(map[string]bool)
	}
	t.chords[ctx][chord] = action
	tokens := strings.Split(chord, " ")
	for end := 1; end < len(tokens); end++ {
		t.prefixes[ctx][strings.Join(tokens[:end], " ")] = true
	}
}

func (t *keyTable) unbind(ctx keyContext, chord string) {
	delete(t.chords[ctx], chord)
}

func (t *keyTable) lookup(stack []keyContext, chord string) (keyAction, bool) {
	for _, ctx := range stack {
		if action, ok := t.chords[ctx][chord]; ok {
			return action, true
		}
	}
	return "", false
}

func (t *keyTable) isPrefix(stack []keyContext, chord string) bool {
	for _, ctx := range stack {
		if t.prefixes[ctx][chord] {
			return true
		}
	}
	return false
}

func (t *keyTable) press(stack []keyContext, state *keyPress, key string, now time.Time, timeout time.Duration) (keyAction, bool) {
	pending := state.pending
	state.pending = ""
	if pending != "" && now.Sub(state.started) <= timeout {
		chord := pending + " " + key
		if action, ok := t.lookup(stack, chord); ok {
			return action, true
		}
		if t.isPrefix(stack, chord) {
			state.pending, state.started = chord, now
		}
		return "", true
	}
	if action, ok := t.lookup(stack, key); ok {
		return action, true
	}
	if t.isPrefix(stack, key) {
		state.pending, state.started = key, now
		return "", true
	}
	return "", false
}

func (t *keyTable) chordsOf(action keyAction, stack ...keyContext) []string {
	var found []string
	for _, ctx := range stack {
		for chord, bound := range t.chords[ctx] {
			if bound == action {
				found = append(found, chord)
			}
		}
	}
	sort.Slice(found, func(left, right int) bool {
		if depth := strings.Count(found[left], " ") - strings.Count(found[right], " "); depth != 0 {
			return depth < 0
		}
		return found[left] < found[right]
	})
	return found
}

func (t *keyTable) hintFor(action keyAction, stack ...keyContext) string {
	chords := t.chordsOf(action, stack...)
	if len(chords) == 0 {
		return ""
	}
	return keyHintGlyph(chords[0])
}

func keyBaseName(lower string) (string, bool) {
	switch lower {
	case "return":
		return keyEnter, true
	case "escape":
		return keyEsc, true
	case "space":
		return keySpace, true
	case "pageup":
		return keyPgUp, true
	case "pagedown":
		return keyPgDown, true
	case "del":
		return keyDelete, true
	}
	return "", false
}

func isKeyNamedBase(lower string) bool {
	switch lower {
	case keyEnter, keyEsc, keyTab, keySpace, keyBackspace, keyDelete, keyPgUp, keyPgDown,
		"up", "down", "left", "right", "home", "end",
		"f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9", "f10", "f11", "f12":
		return true
	}
	return false
}

func isKeyUnreliable(key string) bool {
	switch key {
	case "ctrl+c", "ctrl+m", "ctrl+[", "ctrl+i", "ctrl+h":
		return true
	}
	return false
}

func isKeyReserved(key string) bool {
	switch key {
	case "ctrl+d", "ctrl+z", "ctrl+\\":
		return true
	}
	return false
}

func canonicalKey(spec string) (string, error) {
	if spec == "" {
		return "", errKeyEmpty
	}
	parts := strings.Split(spec, "+")
	base := parts[len(parts)-1]
	mods := parts[:len(parts)-1]
	var alt, ctrl, shift, cmd bool
	for _, mod := range mods {
		switch strings.ToLower(mod) {
		case "ctrl", "control":
			ctrl = true
		case "alt", "meta", "opt", "option":
			alt = true
		case "shift":
			shift = true
		case "cmd", "command", "super", "win":
			cmd = true
		default:
			return "", fmt.Errorf("%w : %s", errKeyModifier, mod)
		}
	}
	name, err := canonicalBase(base, shift)
	if err != nil {
		return "", err
	}
	if utf8.RuneCountInString(name) == 1 && strings.ToUpper(name) == name && name != strings.ToLower(name) {
		shift = false
	}
	var out strings.Builder
	if alt {
		out.WriteString("alt+")
	}
	if cmd {
		out.WriteString("cmd+")
	}
	if ctrl {
		out.WriteString("ctrl+")
	}
	if shift {
		out.WriteString("shift+")
	}
	out.WriteString(name)
	return out.String(), nil
}

func canonicalBase(base string, shift bool) (string, error) {
	lower := strings.ToLower(base)
	if mapped, ok := keyBaseName(lower); ok {
		return mapped, nil
	}
	if isKeyNamedBase(lower) {
		return lower, nil
	}
	if utf8.RuneCountInString(base) != 1 {
		return "", fmt.Errorf("%w : %s", errKeyUnknown, base)
	}
	if shift && lower != base {
		return base, nil
	}
	if shift {
		return strings.ToUpper(base), nil
	}
	return base, nil
}

func canonicalChord(spec string) (string, error) {
	tokens := strings.Fields(spec)
	if len(tokens) == 0 {
		return "", errKeyChordLength
	}
	canon := make([]string, 0, len(tokens))
	for _, token := range tokens {
		key, err := canonicalKey(token)
		if err != nil {
			return "", err
		}
		canon = append(canon, key)
	}
	return strings.Join(canon, " "), nil
}

func reliableChord(chord string) error {
	for _, key := range strings.Split(chord, " ") {
		if isKeyUnreliable(key) {
			return fmt.Errorf("%w : %s", errKeyUnreliable, key)
		}
		if isKeyReserved(key) {
			return fmt.Errorf("%w : %s", errKeyReserved, key)
		}
	}
	return nil
}

func keyHintGlyph(chord string) string {
	if chord == keySpace {
		return keyHintToken(keySpace)
	}
	tokens := strings.Split(chord, " ")
	parts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		parts = append(parts, keyHintToken(token))
	}
	return strings.Join(parts, "")
}

func keyHintBase(base string) (string, bool) {
	switch base {
	case keyEnter:
		return "⏎", true
	case keyEsc:
		return "échap", true
	case keyTab:
		return "⇥", true
	case keySpace:
		return "espace", true
	case keyBackspace:
		return "⌫", true
	case keyDelete:
		return "suppr", true
	case "up":
		return "↑", true
	case "down":
		return "↓", true
	case "left":
		return "←", true
	case "right":
		return "→", true
	case "home":
		return "début", true
	case "end":
		return "fin", true
	case keyPgUp:
		return keyPgUp, true
	case keyPgDown:
		return "pgdn", true
	}
	return "", false
}

func keyHintToken(token string) string {
	base := token
	var prefix strings.Builder
	for {
		switch {
		case strings.HasPrefix(base, "alt+"):
			prefix.WriteString("⌥")
			base = base[len("alt+"):]
		case strings.HasPrefix(base, "cmd+"):
			prefix.WriteString("⌘")
			base = base[len("cmd+"):]
		case strings.HasPrefix(base, "ctrl+"):
			prefix.WriteString("⌃")
			base = base[len("ctrl+"):]
		case strings.HasPrefix(base, "shift+"):
			prefix.WriteString("⇧")
			base = base[len("shift+"):]
		default:
			if glyph, ok := keyHintBase(base); ok {
				return prefix.String() + glyph
			}
			if prefix.Len() == 0 && len(base) == 1 {
				return base
			}
			return prefix.String() + strings.ToUpper(base)
		}
	}
}

func chordTimeout(seconds float64) time.Duration {
	if seconds <= 0 {
		return defaultChordTimeout
	}
	return time.Duration(seconds * float64(time.Second))
}
