package devices

import (
	"crypto/sha256"
	"encoding/hex"
)

const (
	shortFingerprintBytes = 5
	byteBits              = 8
	symbolBits            = 5
	symbolCount           = 8
	symbolGroup           = symbolCount / 2
	symbolMask            = 1<<symbolBits - 1
)

// KeyFingerprint is the SHA-256 of a public key in DER form, in hexadecimal. The registry keeps it
// to tell that a certificate still carries the key the device was paired with.
func KeyFingerprint(spki []byte) string {
	sum := sha256.Sum256(spki)
	return hex.EncodeToString(sum[:])
}

// ShortFingerprint is the short authentication string shown on both ends of a pairing, so an
// operator can compare what the device shows with what the daemon shows. It is eight symbols of the
// pairing alphabet, grouped in two fours.
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
