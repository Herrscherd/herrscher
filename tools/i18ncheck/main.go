// Command i18ncheck refuses any sentence shown to a person in the presentation packages that is not a typed exemption.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

type exit uint8

const (
	exitClean    exit = 0
	exitFindings exit = 1
	exitUsage    exit = 2
	exitNoInput  exit = 66
	exitIOError  exit = 74
)

var errUsage = errors.New("usage: i18ncheck <root>")

func main() {
	os.Exit(int(run(os.Args[1:], os.Stdout, os.Stderr)))
}

func run(args []string, stdout, stderr io.Writer) exit {
	if len(args) != 1 {
		return report(stderr, errUsage, exitUsage)
	}
	found, err := collect(args[0], presentationScope())
	if err != nil {
		return report(stderr, err, exitNoInput)
	}
	return reportFindings(found, stdout, stderr)
}

func report(stderr io.Writer, err error, code exit) exit {
	if _, writeErr := fmt.Fprintf(stderr, "i18ncheck: %v\n", err); writeErr != nil {
		return exitIOError
	}
	return code
}
