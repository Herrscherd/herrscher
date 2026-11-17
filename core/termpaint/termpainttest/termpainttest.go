// Package termpainttest holds the test oracles shared by the packages that check
// terminal escaping.
package termpainttest

// BidiControls lists every bidirectional control that termpaint.Escape writes as a
// \u escape, written out independently of the implementation.
func BidiControls() []rune {
	return []rune{
		0x061c, 0x200e, 0x200f, 0x2028, 0x2029,
		0x202a, 0x202b, 0x202c, 0x202d, 0x202e,
		0x2066, 0x2067, 0x2068, 0x2069,
	}
}
