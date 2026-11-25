package host

import (
	"runtime"
	"strings"
)

func goroutinesCreatedIn(files ...string) int {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	count := 0
	for block := range strings.SplitSeq(string(buf), "\n\n") {
		if createdInAny(block, files) {
			count++
		}
	}
	return count
}

func createdInAny(block string, files []string) bool {
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "created by ") || i+1 >= len(lines) {
			continue
		}
		for _, file := range files {
			if strings.Contains(lines[i+1], "/"+file+":") {
				return true
			}
		}
	}
	return false
}
