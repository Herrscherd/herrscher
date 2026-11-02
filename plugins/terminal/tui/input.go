package tui

import (
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	enhancedKeysOn  = "\x1b[>1u"
	enhancedKeysOff = "\x1b[<u"
)

const (
	funcFinals = "ABCDEFHPQRS"
	keyRelease = "3"
)

const (
	modCapsLock = 64
	modNumLock  = 128
)

func withoutLocks(mods int) int {
	return (mods-1)&^(modCapsLock|modNumLock) + 1
}

type enhancedKeysFailedMsg struct {
	err error
}

func enableEnhancedKeys() tea.Msg {
	if err := writeStdout(enhancedKeysOn); err != nil {
		return enhancedKeysFailedMsg{err: err}
	}
	return nil
}

func writeStdout(sequence string) error {
	_, err := os.Stdout.WriteString(sequence)
	return err
}

const minModFields = 2

const (
	modShift = 1
	modAlt   = 2
	modCtrl  = 4
)

func legacyKey(seq []byte) []byte {
	fields := strings.Split(string(seq[2:len(seq)-1]), ";")
	code, err := strconv.Atoi(strings.SplitN(fields[0], ":", maxColonFields)[0])
	if err != nil || code <= 0 {
		return nil
	}
	mods, released := legacyKeyMods(fields)
	if released {
		return nil
	}
	mask := mods - 1
	b, ok := legacyKeyBytes(code, mask)
	if !ok {
		return nil
	}
	if code == ctrlCR && mask&modShift != 0 {
		return []byte{ctrlESC, '\r'}
	}
	if mask&modAlt != 0 {
		return append([]byte{ctrlESC}, b...)
	}
	return b
}

func legacyKeyMods(fields []string) (int, bool) {
	if len(fields) < minModFields {
		return 1, false
	}
	parts := strings.SplitN(fields[1], ":", maxColonFields)
	if len(parts) == 2 && parts[1] == keyRelease {
		return 0, true
	}
	if m, err := strconv.Atoi(parts[0]); err == nil && m > 0 {
		return withoutLocks(m), false
	}
	return 1, false
}

func legacyKeyBytes(code, mask int) ([]byte, bool) {
	switch code {
	case ctrlCR:
		return []byte{'\r'}, true
	case ctrlHT:
		return []byte{'\t'}, true
	case ctrlESC:
		return []byte{ctrlESC}, true
	case ctrlDEL:
		return []byte{ctrlDEL}, true
	}
	if code < 0 || code > utf8.MaxRune {
		return nil, false
	}
	char := rune(code)
	if mask&modCtrl != 0 && char >= 'a' && char <= 'z' {
		return []byte{byte(char) & 0x1f}, true
	}
	if mask&modShift != 0 {
		char = toUpper(char)
	}
	return []byte(string(char)), true
}

func legacyFunc(seq []byte) []byte {
	final := seq[len(seq)-1]
	if final != '~' && !strings.ContainsRune(funcFinals, rune(final)) {
		return seq
	}
	body := string(seq[2 : len(seq)-1])
	if !strings.ContainsAny(body, ";:") {
		return seq
	}
	fields := strings.Split(body, ";")
	number := strings.SplitN(fields[0], ":", maxColonFields)[0]
	if number != "" {
		if _, err := strconv.Atoi(number); err != nil {
			return seq
		}
	}
	mods, released, ok := legacyFuncMods(fields)
	if released {
		return nil
	}
	if !ok {
		return seq
	}
	return legacyFuncSequence(seq, final, number, mods)
}

func legacyFuncMods(fields []string) (string, bool, bool) {
	if len(fields) < minModFields {
		return "", false, true
	}
	parts := strings.SplitN(fields[1], ":", maxColonFields)
	if len(parts) == 2 && parts[1] == keyRelease {
		return "", true, true
	}
	if parts[0] == "" {
		return "", false, true
	}
	m, err := strconv.Atoi(parts[0])
	if err != nil || m <= 0 {
		return "", false, false
	}
	if m = withoutLocks(m); m > 1 {
		return strconv.Itoa(m), false, true
	}
	return "", false, true
}

func legacyFuncSequence(seq []byte, final byte, number, mods string) []byte {
	if final == '~' {
		if number == "" {
			return seq
		}
		if mods == "" {
			return []byte("\x1b[" + number + "~")
		}
		return []byte("\x1b[" + number + ";" + mods + "~")
	}
	if mods == "" {
		return []byte{ctrlESC, '[', final}
	}
	return []byte("\x1b[1;" + mods + string(final))
}

func toUpper(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - asciiCaseGap
	}
	return r
}

type filteredStdin struct {
	*os.File

	keys  *keyLog
	state filterState
	seq   []byte
	out   []byte
	raw   [4096]byte
}

type filterState int

const (
	filterGround filterState = iota
	filterOSC
	filterOSCEsc
	filterCSI
)

const (
	ctrlESC        = 0x1b
	ctrlBEL        = 0x07
	ctrlHT         = 0x09
	ctrlCR         = 0x0d
	ctrlDEL        = 0x7f
	maxColonFields = 2
	asciiCaseGap   = 'a' - 'A'
	csiSeqCap      = 64
)

func newFilteredStdin(keys *keyLog) *filteredStdin { return &filteredStdin{File: os.Stdin, keys: keys} }

func (f *filteredStdin) Read(p []byte) (int, error) {
	if len(f.out) == 0 {
		n, err := f.File.Read(f.raw[:])
		if n > 0 {
			before := len(f.out)
			f.feed(f.raw[:n])
			f.keys.write("raw %q -> %q", f.raw[:n], f.out[before:])
		}
		if err != nil && len(f.out) == 0 {
			return 0, err
		}
	}
	count := copy(p, f.out)
	f.out = f.out[count:]
	return count, nil
}

func (f *filteredStdin) feed(b []byte) {
	for i := 0; i < len(b); i++ {
		switch f.state {
		case filterGround:
			i = f.feedGround(b, i)
		case filterOSC:
			f.feedOSC(b[i])
		case filterOSCEsc:
			f.state = filterGround
		case filterCSI:
			f.feedCSI(b[i])
		}
	}
}

func (f *filteredStdin) feedGround(b []byte, i int) int {
	c := b[i]
	if c != ctrlESC || i+1 >= len(b) {
		f.out = append(f.out, c)
		return i
	}
	switch b[i+1] {
	case ']':
		f.state = filterOSC
		return i + 1
	case '[':
		f.state = filterCSI
		f.seq = append(f.seq[:0], ctrlESC, '[')
		return i + 1
	default:
		f.out = append(f.out, c)
		return i
	}
}

func (f *filteredStdin) feedOSC(c byte) {
	switch c {
	case ctrlBEL:
		f.state = filterGround
	case ctrlESC:
		f.state = filterOSCEsc
	}
}

func (f *filteredStdin) feedCSI(c byte) {
	f.seq = append(f.seq, c)
	switch {
	case c >= 0x40 && c <= 0x7e:
		f.finishCSI(c)
	case len(f.seq) > csiSeqCap:
		f.out = append(f.out, f.seq...)
		f.seq = f.seq[:0]
		f.state = filterGround
	}
}

func (f *filteredStdin) finishCSI(final byte) {
	switch final {
	case 'R':
	case 'u':
		f.out = append(f.out, legacyKey(f.seq)...)
	default:
		f.out = append(f.out, legacyFunc(f.seq)...)
	}
	f.seq = f.seq[:0]
	f.state = filterGround
}
