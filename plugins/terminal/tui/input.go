package tui

import (
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Shift+Enter is the gesture every chat UI teaches, and a plain terminal cannot
// deliver it: it sends a bare CR, byte for byte what Enter sends, so the composer
// has no way to tell "break the line" from "send it". The only fix is to ask the
// terminal to stop conflating them — the enhanced keyboard protocol, whose
// "disambiguate" mode reports such keys as `CSI <code>;<mods> u` instead.
//
// Bubble Tea v1 does not speak that protocol, so we turn it on ourselves and
// translate the reports back into the legacy bytes its parser already knows
// (legacyKey). A terminal that does not implement the protocol ignores the
// request and never sends a CSI-u report, so the translation simply stays idle —
// nothing to detect, nothing to configure, and nothing breaks where it is absent.
const (
	// Push mode 1 (disambiguate escape codes) onto the terminal's own stack, so
	// popping it restores whatever was set before us rather than assuming none.
	enhancedKeysOn  = "\x1b[>1u"
	enhancedKeysOff = "\x1b[<u"
)

// legacyKey renders one `CSI <code>;<mods> u` report as the bytes Bubble Tea's
// parser expects for that key. seq is the whole sequence including the leading
// ESC '[' and the final 'u'.
//
// The modifier field is a bitmask biased by one (1 = none, 2 = shift, 3 = alt,
// 5 = ctrl, …); anything we cannot render legacy bytes for is dropped rather
// than replayed, since a CSI-u sequence reaching the parser is junk keys on
// screen — the exact failure this file exists to prevent.
func legacyKey(seq []byte) []byte {
	// A field may carry alternate-key variants after ':'; the base value is enough.
	fields := strings.Split(string(seq[2:len(seq)-1]), ";")
	code, err := strconv.Atoi(strings.SplitN(fields[0], ":", 2)[0])
	if err != nil || code <= 0 {
		return nil
	}
	mods := 1
	if len(fields) > 1 {
		if m, err := strconv.Atoi(strings.SplitN(fields[1], ":", 2)[0]); err == nil && m > 0 {
			mods = m
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
	case 13: // Enter — the whole reason we asked for this protocol
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
			b = []byte{byte(r) & 0x1f} // ctrl+letter is its control byte
		} else {
			if mask&modShift != 0 {
				r = toUpper(r)
			}
			b = []byte(string(r))
		}
	}
	// Shift on Enter is what we came for: hand it over as Alt+Enter, the chord
	// the composer already binds to a line break, so one binding serves both.
	if code == 13 && mask&modShift != 0 {
		return []byte{ctrlESC, '\r'}
	}
	if mask&modAlt != 0 {
		return append([]byte{ctrlESC}, b...)
	}
	return b
}

func toUpper(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - 32
	}
	return r
}

// The terminal answers colour and cursor queries in-band, mixed into stdin. Two
// such answers can surface as garbage in the input box:
//
//   - OSC responses: Bubble Tea probes the background colour at init (via
//     lipgloss.HasDarkBackground → an OSC 11 query). The terminal replies with
//     "\x1b]11;rgb:rrrr/gggg/bbbb\x1b\\" (or …\a). On terminals that answer the
//     cursor-position probe first, termenv abandons the read, and the leftover
//     OSC reply reaches Bubble Tea's key parser as a stray Esc plus typed text.
//   - CSI cursor-position reports: "\x1b[<row>;<col>R", the answer to a DSR (6n)
//     probe, which the parser would likewise turn into junk keys.
//
// filteredStdin drops both, upstream of Bubble Tea's parser, without disturbing
// real keys, arrows, or mouse events. It embeds *os.File so Bubble Tea's
// term.File check still sees the real stdin fd (Fd/Name/Write/Close are
// promoted): raw mode and TTY detection apply to the true terminal, and only
// Read is filtered.
type filteredStdin struct {
	*os.File // os.Stdin: the byte source and the promoted term.File methods

	state filterState
	seq   []byte     // buffered CSI bytes, replayed verbatim unless it is a report
	out   []byte     // filtered bytes not yet handed to the caller
	raw   [4096]byte // scratch for one underlying Read
}

type filterState int

const (
	filterGround filterState = iota // outside any escape sequence
	filterOSC                       // inside an OSC string, dropping to BEL or ST
	filterOSCEsc                    // inside an OSC string, saw ESC (maybe ST)
	filterCSI                       // inside a CSI sequence, buffering to its final byte
)

const (
	ctrlESC = 0x1b
	ctrlBEL = 0x07
	// csiSeqCap bounds how many bytes a CSI sequence may buffer before it is
	// treated as malformed and replayed as-is, so a stray ESC-[ can never make
	// Read withhold input indefinitely.
	csiSeqCap = 64
)

func newFilteredStdin() *filteredStdin { return &filteredStdin{File: os.Stdin} }

// Read hands back filtered bytes. It performs at most one underlying read per
// call: if a whole chunk is dropped (e.g. the startup colour-query response) it
// returns (0, nil), and the caller (Bubble Tea's readAnsiInputs) simply reads
// again through its cancelable epoll wait. It must NOT loop on an empty result —
// a second blocking f.File.Read here would sit outside that cancel point and
// wedge shutdown until the next keypress.
func (f *filteredStdin) Read(p []byte) (int, error) {
	if len(f.out) == 0 {
		n, err := f.File.Read(f.raw[:])
		if n > 0 {
			f.feed(f.raw[:n])
		}
		if err != nil && len(f.out) == 0 {
			return 0, err // nothing buffered to hand back first: surface the error
		}
	}
	n := copy(p, f.out)
	f.out = f.out[n:]
	return n, nil
}

// feed runs the byte-at-a-time filter state machine, appending surviving bytes
// to f.out and dropping query responses.
func (f *filteredStdin) feed(b []byte) {
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch f.state {
		case filterGround:
			if c != ctrlESC {
				f.out = append(f.out, c)
				continue
			}
			// Classify the ESC by the next byte in this same chunk. Query
			// responses arrive as one atomic tty read, so a trailing ESC (no next
			// byte yet) is a real Esc keypress and passes through untouched — the
			// Esc key is never swallowed or delayed.
			if i+1 >= len(b) {
				f.out = append(f.out, c)
				continue
			}
			switch b[i+1] {
			case ']': // OSC: a colour-query response — drop the whole string
				f.state = filterOSC
				i++
			case '[': // CSI: an arrow/function key, or a cursor-position report
				f.state = filterCSI
				f.seq = append(f.seq[:0], ctrlESC, '[')
				i++
			default: // ESC + anything else: leave it for the parser
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
			// ST is ESC '\'; any byte here ends the OSC string either way.
			f.state = filterGround
		case filterCSI:
			f.seq = append(f.seq, c)
			switch {
			case c >= 0x40 && c <= 0x7e: // CSI final byte
				switch {
				case c == 'R': // a cursor-position report, not a key
				case c == 'u': // a key report from the enhanced protocol
					f.out = append(f.out, legacyKey(f.seq)...)
				default:
					f.out = append(f.out, f.seq...)
				}
				f.seq = f.seq[:0]
				f.state = filterGround
			case len(f.seq) > csiSeqCap: // malformed: replay rather than withhold
				f.out = append(f.out, f.seq...)
				f.seq = f.seq[:0]
				f.state = filterGround
			}
		}
	}
}
