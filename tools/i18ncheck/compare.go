package main

import (
	"fmt"
	"io"
	"strconv"
)

func compare(found map[string][]finding, allowed baseline, stdout, stderr io.Writer) exit {
	var problems []string
	var listing []finding
	for _, key := range packagesOf(found, allowed) {
		got := len(found[key])
		want := allowed[key]
		switch {
		case got > want:
			problems = append(problems, overMessage(key, got, want))
			listing = append(listing, found[key]...)
		case got < want:
			problems = append(problems, underMessage(key, got, want))
		}
	}
	if len(problems) == 0 {
		return exitClean
	}
	if err := writeReport(stdout, listing, problems); err != nil {
		return report(stderr, err, exitIOError)
	}
	return exitRatchet
}

func overMessage(key string, got, want int) string {
	return key + ": " + strconv.Itoa(got) + " hard-coded sentences, the baseline allows " + strconv.Itoa(want) +
		"; move the new ones to the message catalog (listed above)"
}

func underMessage(key string, got, want int) string {
	if got == 0 {
		return key + ": no hard-coded sentence left, the baseline still counts " + strconv.Itoa(want) +
			"; delete its line from the baseline"
	}
	return key + ": " + strconv.Itoa(got) + " hard-coded sentences left, the baseline still counts " +
		strconv.Itoa(want) + "; lower its line in the baseline to " + strconv.Itoa(got)
}

func writeReport(stdout io.Writer, listing []finding, problems []string) error {
	for _, literal := range listing {
		if _, err := fmt.Fprintf(stdout, "%s: %q\n", literal.position, literal.text); err != nil {
			return err
		}
	}
	for _, problem := range problems {
		if _, err := fmt.Fprintln(stdout, problem); err != nil {
			return err
		}
	}
	return nil
}
