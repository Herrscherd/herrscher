package devices

import (
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
)

const (
	pairingCodeLength = 10
	pairingAlphabet   = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	pairingSaltBytes  = 16
)

// PairingCode is the one-time code an operator reads to a device. Its zero value is not a code.
type PairingCode struct {
	value string
}

// NewPairingCode draws ten characters of the pairing alphabet from rnd. The alphabet has
// thirty-two symbols, which divides the byte range evenly, so no symbol is favoured.
func NewPairingCode(rnd io.Reader) (PairingCode, error) {
	raw := make([]byte, pairingCodeLength)
	if _, err := io.ReadFull(rnd, raw); err != nil {
		return PairingCode{}, fmt.Errorf("cannot draw a pairing code: %w", err)
	}
	out := make([]byte, pairingCodeLength)
	for idx, b := range raw {
		out[idx] = pairingAlphabet[int(b)%len(pairingAlphabet)]
	}
	return PairingCode{value: string(out)}, nil
}

// ParsePairingCode accepts the code in either case, with or without the dash String writes.
func ParsePairingCode(text string) (PairingCode, error) {
	normalized := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(text))
	if len(normalized) != pairingCodeLength {
		return PairingCode{}, ErrMalformedCode
	}
	for idx := range len(normalized) {
		if strings.IndexByte(pairingAlphabet, normalized[idx]) < 0 {
			return PairingCode{}, ErrMalformedCode
		}
	}
	return PairingCode{value: normalized}, nil
}

// String writes the code as two groups of five, the form an operator reads aloud.
func (c PairingCode) String() string {
	if len(c.value) != pairingCodeLength {
		return ""
	}
	return c.value[:pairingCodeLength/2] + "-" + c.value[pairingCodeLength/2:]
}

func (c PairingCode) digest(salt []byte) []byte {
	sum := sha256.Sum256(append(append([]byte{}, salt...), c.value...))
	return sum[:]
}

func newSalt(rnd io.Reader) ([]byte, error) {
	salt := make([]byte, pairingSaltBytes)
	if _, err := io.ReadFull(rnd, salt); err != nil {
		return nil, fmt.Errorf("cannot salt a pairing code: %w", err)
	}
	return salt, nil
}
