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

func enableEnhancedKeys() tea.Msg {
	os.Stdout.WriteString(enhancedKeysOn)
	return nil
}

func legacyKey(seq []byte) []byte {
	fields := strings.Split(string(seq[2:len(seq)-1]), ";")
	code, err := strconv.Atoi(strings.SplitN(fields[0], ":", 2)[0])
	if err != nil || code <= 0 {
		return nil
	}
	mods := 1
	if len(fields) > 1 {
		parts := strings.SplitN(fields[1], ":", 2)
		if len(parts) == 2 && parts[1] == keyRelease {
			return nil
		}
		if m, err := strconv.Atoi(parts[0]); err == nil && m > 0 {
			mods = withoutLocks(m)
		}
	}
	mask := mods - 1
	const (
		modShift = 1
		modAlt   = 2
		modCtrl  = 4
	)

	var b []byte
	switch code {
	case 13:
		b = []byte{'\r'}
	case 9:
		b = []byte{'\t'}
	case 27:
		b = []byte{ctrlESC}
	case 127:
		b = []byte{0x7f}
	default:
		if code > utf8.MaxRune {
			return nil
		}
		r := rune(code)
		if mask&modCtrl != 0 && r >= 'a' && r <= 'z' {
			b = []byte{byte(r) & 0x1f}
		} else {
			if mask&modShift != 0 {
				r = toUpper(r)
			}
			b = []byte(string(r))
		}
	}
	if code == 13 && mask&modShift != 0 {
		return []byte{ctrlESC, '\r'}
	}
	if mask&modAlt != 0 {
		return append([]byte{ctrlESC}, b...)
	}
	return b
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
	number := strings.SplitN(fields[0], ":", 2)[0]
	if number != "" {
		if _, err := strconv.Atoi(number); err != nil {
			return seq
		}
	}
	mods := ""
	if len(fields) > 1 {
		parts := strings.SplitN(fields[1], ":", 2)
		if len(parts) == 2 && parts[1] == keyRelease {
			return nil
		}
		if parts[0] != "" {
			m, err := strconv.Atoi(parts[0])
			if err != nil || m <= 0 {
				return seq
			}
			if m = withoutLocks(m); m > 1 {
				mods = strconv.Itoa(m)
			}
		}
	}
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
		return r - 32
	}
	return r
}

type filteredStdin struct {
	*os.File

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
	ctrlESC   = 0x1b
	ctrlBEL   = 0x07
	csiSeqCap = 64
)

func newFilteredStdin() *filteredStdin { return &filteredStdin{File: os.Stdin} }

func (f *filteredStdin) Read(p []byte) (int, error) {
	if len(f.out) == 0 {
		n, err := f.File.Read(f.raw[:])
		if n > 0 {
			before := len(f.out)
			f.feed(f.raw[:n])
			keylog("raw %q -> %q", f.raw[:n], f.out[before:])
		}
		if err != nil && len(f.out) == 0 {
			return 0, err
		}
	}
	n := copy(p, f.out)
	f.out = f.out[n:]
	return n, nil
}

func (f *filteredStdin) feed(b []byte) {
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch f.state {
		case filterGround:
			if c != ctrlESC {
				f.out = append(f.out, c)
				continue
			}
			if i+1 >= len(b) {
				f.out = append(f.out, c)
				continue
			}
			switch b[i+1] {
			case ']':
				f.state = filterOSC
				i++
			case '[':
				f.state = filterCSI
				f.seq = append(f.seq[:0], ctrlESC, '[')
				i++
			default:
				f.out = append(f.out, c)
			}
		case filterOSC:
			switch c {
			case ctrlBEL:
				f.state = filterGround
			case ctrlESC:
				f.state = filterOSCEsc
			}
		case filterOSCEsc:
			f.state = filterGround
		case filterCSI:
			f.seq = append(f.seq, c)
			switch {
			case c >= 0x40 && c <= 0x7e:
				switch {
				case c == 'R':
				case c == 'u':
					f.out = append(f.out, legacyKey(f.seq)...)
				default:
					f.out = append(f.out, legacyFunc(f.seq)...)
				}
				f.seq = f.seq[:0]
				f.state = filterGround
			case len(f.seq) > csiSeqCap:
				f.out = append(f.out, f.seq...)
				f.seq = f.seq[:0]
				f.state = filterGround
			}
		}
	}
}
