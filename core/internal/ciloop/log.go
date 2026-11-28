// Package ciloop holds the policy and the data handling of the session CI loop.
package ciloop

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	fenceTagBytes  = 8
	fenceDrawLimit = 8
)

var errNoFreeFenceTag = errors.New("ci log fence: no free tag")

// Block is the data of a CI nudge: the check name, its page and the log. Body is what sits
// between the two markers that carry Tag, and Tag is drawn anew for every message.
type Block struct {
	Tag  string
	Body string
}

// NewBlock builds the data block of a CI nudge. Its tag is drawn from entropy and redrawn
// whenever the body already contains it, so no body can name the closing marker of its own
// block. Invalid UTF-8 in the body is replaced, and nothing else in it is changed.
func NewBlock(entropy io.Reader, check, page, log string) (Block, error) {
	body := strings.ToValidUTF8("check: "+check+"\npage: "+page+"\n"+log, "�")
	for range fenceDrawLimit {
		tag, err := drawTag(entropy)
		if err != nil {
			return Block{}, err
		}
		if !strings.Contains(body, tag) {
			return Block{Tag: tag, Body: body}, nil
		}
	}
	return Block{}, fmt.Errorf("%w in %d draws", errNoFreeFenceTag, fenceDrawLimit)
}

func drawTag(entropy io.Reader) (string, error) {
	var raw [fenceTagBytes]byte
	if _, err := io.ReadFull(entropy, raw[:]); err != nil {
		return "", fmt.Errorf("ci log fence: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
