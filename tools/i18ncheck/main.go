// Command i18ncheck ratchets the human text written in the presentation packages down to its baseline.
//
// It reads the presentation packages of a module and reports every string literal that reads as a
// sentence and reaches the user without going through the message catalog: the value assigned to a
// flash or a notice field, the text of a contracts.DataResult, and the message of a fmt.Errorf or an
// errors.New. Syntax such as flags, slash commands and single words is not prose and is never reported.
//
// The baseline holds one line per package, "<package> <count>", with the number of literals that package
// may still carry. A package may only lose literals: a count above its line is a new hard-coded sentence,
// a count below its line means the line must be lowered, and a package at zero has no line at all.
//
// Usage:
//
//	i18ncheck -baseline <file> <root>
//
// Exit codes:
//
//	0   every package matches its baseline line
//	1   a package has more literals than its line, or fewer, and its line must change
//	2   the command line is wrong
//	66  the baseline or a source file cannot be read or parsed
//	74  the report cannot be written
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
	found, err := collect(flags.Arg(0))
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
