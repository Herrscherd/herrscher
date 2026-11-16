package main

import (
	"os"

	"github.com/Herrscherd/herrscher/e2e/vendortest"
)

func main() {
	os.Exit(vendortest.Claude(os.Args[1:], vendortest.Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}, vendortest.FilesFrom(os.Getenv)))
}
