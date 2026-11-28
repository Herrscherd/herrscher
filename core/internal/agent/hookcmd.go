package agent

import (
	"path/filepath"
	"strings"
)

func isApprovalCommand(command string) bool {
	words, ok := shellWords(command)
	if !ok || len(words) < 3 || len(words) > 4 {
		return false
	}
	if filepath.Base(words[0]) != herrscherName || words[1] != approveWord || words[2] != hookWord {
		return false
	}
	return len(words) == 3 || words[3] == rtkFlagWord
}

func shellWords(command string) ([]string, bool) {
	var words []string
	var word strings.Builder
	inWord := false
	for pos := 0; pos < len(command); pos++ {
		switch char := command[pos]; {
		case char == '\'':
			end := strings.IndexByte(command[pos+1:], '\'')
			if end < 0 {
				return nil, false
			}
			word.WriteString(command[pos+1 : pos+1+end])
			pos += end + 1
			inWord = true
		case char == '\\' && pos+1 < len(command) && command[pos+1] == '\'':
			word.WriteByte('\'')
			pos++
			inWord = true
		case char == ' ':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteByte(char)
			inWord = true
		}
	}
	if inWord {
		words = append(words, word.String())
	}
	return words, true
}
