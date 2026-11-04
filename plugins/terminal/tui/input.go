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

const ctrlEnterKey = "\x1b[24;3~"

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
	bytes, ok := legacyKeyBytes(code, mask)
	if !ok {
		return nil
	}
	if code == ctrlCR && mask&modCtrl != 0 {
		return []byte(ctrlEnterKey)
	}
	if code == ctrlCR && mask&modShift != 0 {
		return []byte{ctrlESC, '\r'}
	}
	if mask&modAlt != 0 {
		return append([]byte{ctrlESC}, bytes...)
	}
	return bytes
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
	value, err := strconv.Atoi(parts[0])
	if err != nil || value <= 0 {
		return "", false, false
	}
	if value = withoutLocks(value); value > 1 {
		return strconv.Itoa(value), false, true
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

func toUpper(char rune) rune {
	if char >= 'a' && char <= 'z' {
		return char - asciiCaseGap
	}
	return char
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

func (f *filteredStdin) Read(buf []byte) (int, error) {
	if len(f.out) == 0 {
		read, err := f.File.Read(f.raw[:])
		if read > 0 {
			before := len(f.out)
			f.feed(f.raw[:read])
			f.keys.write("raw %q -> %q", f.raw[:read], f.out[before:])
		}
		if err != nil && len(f.out) == 0 {
			return 0, err
		}
	}
	count := copy(buf, f.out)
	f.out = f.out[count:]
	return count, nil
}

func (f *filteredStdin) feed(bytes []byte) {
	for index := 0; index < len(bytes); index++ {
		switch f.state {
		case filterGround:
			index = f.feedGround(bytes, index)
		case filterOSC:
			f.feedOSC(bytes[index])
		case filterOSCEsc:
			f.state = filterGround
		case filterCSI:
			f.feedCSI(bytes[index])
		}
	}
}

func (f *filteredStdin) feedGround(bytes []byte, index int) int {
	char := bytes[index]
	if char != ctrlESC || index+1 >= len(bytes) {
		f.out = append(f.out, char)
		return index
	}
	switch bytes[index+1] {
	case ']':
		f.state = filterOSC
		return index + 1
	case '[':
		f.state = filterCSI
		f.seq = append(f.seq[:0], ctrlESC, '[')
		return index + 1
	default:
		f.out = append(f.out, char)
		return index
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

func (f *filteredStdin) feedCSI(char byte) {
	f.seq = append(f.seq, char)
	switch {
	case char >= 0x40 && char <= 0x7e:
		f.finishCSI(char)
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
