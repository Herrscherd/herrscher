package devices

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

const (
	shortFingerprintBytes = 5
	byteBits              = 8
	symbolBits            = 5
	symbolCount           = 8
	symbolGroup           = symbolCount / 2
	symbolMask            = 1<<symbolBits - 1
)

// KeyFingerprint is the SHA-256 of a public key in DER form, in hexadecimal.
func KeyFingerprint(spki []byte) string {
	sum := sha256.Sum256(spki)
	return hex.EncodeToString(sum[:])
}

// ShortFingerprint is the short authentication string shown on both ends of a pairing, so an operator can compare what the device shows with what the daemon shows.
func ShortFingerprint(spki []byte) string {
	sum := sha256.Sum256(spki)
	var bits uint64
	for _, b := range sum[:shortFingerprintBytes] {
		bits = bits<<byteBits | uint64(b)
	}
	out := make([]byte, 0, symbolCount+1)
	for idx := range symbolCount {
		if idx == symbolGroup {
			out = append(out, '-')
		}
		shift := uint(shortFingerprintBytes*byteBits - symbolBits*(idx+1))
		out = append(out, pairingAlphabet[(bits>>shift)&symbolMask])
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
