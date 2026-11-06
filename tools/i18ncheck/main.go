// Command i18ncheck ratchets the sentences shown to a person in the presentation packages down to their baseline.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

type exit uint8

const (
	exitClean   exit = 0
	exitRatchet exit = 1
	exitUsage   exit = 2
	exitNoInput exit = 66
	exitIOError exit = 74
)

var errUsage = errors.New("usage: i18ncheck -baseline <file> <root>")

func main() {
	os.Exit(int(run(os.Args[1:], os.Stdout, os.Stderr)))
}

func run(args []string, stdout, stderr io.Writer) exit {
	flags := flag.NewFlagSet("i18ncheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	baselinePath := flags.String("baseline", "", "baseline file, one line per package")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}
	if *baselinePath == "" || flags.NArg() != 1 {
		return report(stderr, errUsage, exitUsage)
	}
	allowed, err := readBaseline(*baselinePath)
	if err != nil {
		return report(stderr, err, exitNoInput)
	}
	found, err := collect(flags.Arg(0), presentationScope())
	if err != nil {
		return report(stderr, err, exitNoInput)
	}
	return compare(found, allowed, stdout, stderr)
}

func report(stderr io.Writer, err error, code exit) exit {
	if _, writeErr := fmt.Fprintf(stderr, "i18ncheck: %v\n", err); writeErr != nil {
		return exitIOError
	}
	return code
}
