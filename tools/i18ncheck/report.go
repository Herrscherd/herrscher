package main

import (
	"fmt"
	"io"
	"maps"
	"slices"
)

func reportFindings(found map[string][]finding, stdout, stderr io.Writer) exit {
	var listing []finding
	for _, key := range slices.Sorted(maps.Keys(found)) {
		listing = append(listing, found[key]...)
	}
	if len(listing) == 0 {
		return exitClean
	}
	for _, literal := range listing {
		if _, err := fmt.Fprintf(stdout, "%s: %q\n", literal.position, literal.text); err != nil {
			return report(stderr, err, exitIOError)
		}
	}
	if _, err := fmt.Fprintf(stdout, "%d sentences shown to a person outside a typed exemption; move them to the message catalog\n", len(listing)); err != nil {
		return report(stderr, err, exitIOError)
	}
	return exitFindings
}
