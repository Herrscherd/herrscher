package devices

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

const (
	shortFingerprintBytes = 10
	byteBits              = 8
	symbolBits            = 5
	symbolCount           = 16
	symbolGroup           = symbolCount / 2
	symbolMask            = 1<<symbolBits - 1
)

// KeyFingerprint is the SHA-256 of a public key in DER form, in hexadecimal.
func KeyFingerprint(spki []byte) string {
	sum := sha256.Sum256(spki)
	return hex.EncodeToString(sum[:])
}

// ShortFingerprint is the short authentication string shown on both ends of a pairing, so an operator can
// compare what the device shows with what the daemon shows. It carries 80 bits, as sixteen symbols of the pairing alphabet in two groups of eight.
func ShortFingerprint(spki []byte) string {
	sum := sha256.Sum256(spki)
	symbols := symbolsOf(sum[:shortFingerprintBytes], symbolCount)
	return symbols[:symbolGroup] + "-" + symbols[symbolGroup:]
}

func symbolsOf(raw []byte, count int) string {
	out := make([]byte, 0, count)
	var acc, bits uint
	for _, b := range raw {
		acc = acc<<byteBits | uint(b)
		bits += byteBits
		for bits >= symbolBits && len(out) < count {
			bits -= symbolBits
			out = append(out, pairingAlphabet[(acc>>bits)&symbolMask])
		}
		acc &= (1 << bits) - 1
	}
	return string(out)
}

// Fingerprint is a short fingerprint an operator typed in, normalised to upper case without separators.
type Fingerprint struct {
	value string
}

// ParseFingerprint accepts the eight symbols in either case, with or without the dash ShortFingerprint writes.
func ParseFingerprint(text string) (Fingerprint, error) {
	normalised := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(text))
	if len(normalised) != symbolCount {
		return Fingerprint{}, ErrMalformedFingerprint
	}
	for idx := range len(normalised) {
		if strings.IndexByte(pairingAlphabet, normalised[idx]) < 0 {
			return Fingerprint{}, ErrMalformedFingerprint
		}
	}
	return Fingerprint{value: normalised}, nil
}

// String writes the fingerprint in the grouped form ShortFingerprint uses.
func (f Fingerprint) String() string {
	if len(f.value) != symbolCount {
		return ""
	}
	return f.value[:symbolGroup] + "-" + f.value[symbolGroup:]
}

func (f Fingerprint) matches(short string) bool {
	expected := strings.ReplaceAll(short, "-", "")
	return len(f.value) == symbolCount && subtle.ConstantTimeCompare([]byte(f.value), []byte(expected)) == 1
}
