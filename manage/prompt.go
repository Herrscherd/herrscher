package manage

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func promptLine(input *bufio.Reader, label string) string {
	if label != "" {
		fmt.Fprint(os.Stderr, label)
	}
	line, err := input.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		slog.Warn("read prompt", "err", err)
	}
	return strings.TrimSpace(line)
}
