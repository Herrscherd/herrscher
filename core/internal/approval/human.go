package approval

import (
	"path"
	"slices"
	"strings"
)

// RequiresHuman reports whether req is a Bash command that may administer herrscher's devices or roles, which only a
// person may decide: no rule, no remembered answer and no mode lets it through. It is a coarse text barrier and it
// over-blocks on purpose. It needs a person when the command, with every character but ASCII letters removed, holds
// herr or scher together with device, role or xargs, or when it names herrscher through a glob that can expand to it.
// It also needs a person when the command runs code from a pipe or inline into an interpreter and holds a decoder or
// an encoding. It does not see a payload that spells none of these letters and passes through no decoder it knows.
func RequiresHuman(req Request) bool {
	if CanonicalTool(req.Tool) != toolBash {
		return false
	}
	letters := lettersOnly(req.Subject)
	names := strings.Contains(letters, "herr") || strings.Contains(letters, "scher") || namesHerrscherByGlob(req.Subject)
	verbs := strings.Contains(letters, "device") || strings.Contains(letters, "role") || strings.Contains(letters, "xargs")
	if names && verbs {
		return true
	}
	return runsDecodedCode(req.Subject)
}

func lettersOnly(command string) string {
	var letters strings.Builder
	for _, char := range strings.ToLower(command) {
		if char >= 'a' && char <= 'z' {
			letters.WriteRune(char)
		}
	}
	return letters.String()
}

func namesHerrscherByGlob(command string) bool {
	unquoted := strings.NewReplacer(`\`, "", `"`, "", `'`, "", "`", "").Replace(command)
	for _, field := range strings.Fields(unquoted) {
		if !strings.ContainsAny(field, "*?[") {
			continue
		}
		segment := field[strings.LastIndex(field, "/")+1:]
		if matched, err := path.Match(segment, "herrscher"); err == nil && matched {
			return true
		}
	}
	return false
}

func tokenize(command string) []string {
	spaced := strings.NewReplacer("<<", " << ", "<(", " <( ", "||", " || ", "&&", " && ", "|&", " | ", "|", " | ", "&", " & ", ";", " ; ", "\n", " ; ", "(", " ( ", ")", " ) ", "`", " ` ", `"`, " ", "'", " ").Replace(command)
	return strings.Fields(spaced)
}

func runsDecodedCode(command string) bool {
	tokens := tokenize(command)
	return runsInterpreter(tokens) && hasDecoder(command, tokens)
}

func wrapperValueOptions(name string) ([]string, bool) {
	switch name {
	case "xargs":
		return []string{"-d", "-E", "-I", "-L", "-n", "-P", "-s", "-a", "--delimiter", "--max-args", "--max-procs", "--max-lines", "--max-chars", "--arg-file"}, true
	case "env":
		return []string{"-u", "-C", "-S", "--unset", "--chdir", "--split-string"}, true
	case "nice":
		return []string{"-n", "--adjustment"}, true
	case "timeout":
		return []string{"-s", "-k", "--signal", "--kill-after"}, true
	case "stdbuf":
		return []string{"-i", "-o", "-e", "--input", "--output", "--error"}, true
	case "exec":
		return []string{"-a"}, true
	case "sudo":
		return []string{"-u", "-g", "-C", "-D", "-p", "-r", "-t", "-U", "-T"}, true
	case "doas":
		return []string{"-u", "-C"}, true
	case "command", "nohup", "busybox":
		return nil, true
	}
	return nil, false
}

func runsInterpreter(tokens []string) bool {
	for idx, token := range tokens {
		if token == "|" && isInterpreter(tokenAt(tokens, pipedCommand(tokens, idx+1))) {
			return true
		}
		if isInterpreter(token) && takesInlineCode(tokens, idx) {
			return true
		}
	}
	return false
}

func pipedCommand(tokens []string, idx int) int {
	for idx < len(tokens) {
		token := tokens[idx]
		if token == "(" || token == ")" || token == "`" || token == "$" || isAssignment(token) {
			idx++
			continue
		}
		name := commandName(token)
		valueOptions, isWrapper := wrapperValueOptions(name)
		if !isWrapper {
			return idx
		}
		idx = skipWrapperArguments(tokens, idx+1, valueOptions, name == "timeout")
	}
	return idx
}

func skipWrapperArguments(tokens []string, idx int, valueOptions []string, takesDuration bool) int {
	for idx < len(tokens) && strings.HasPrefix(tokens[idx], "-") {
		option := tokens[idx]
		idx++
		if slices.Contains(valueOptions, option) {
			idx++
		}
	}
	if takesDuration {
		idx++
	}
	return idx
}

func isAssignment(token string) bool {
	name, _, found := strings.Cut(token, "=")
	if !found || name == "" {
		return false
	}
	for idx, char := range name {
		isLetter := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char == '_'
		isDigit := char >= '0' && char <= '9'
		if !isLetter && (idx == 0 || !isDigit) {
			return false
		}
	}
	return true
}

func takesInlineCode(tokens []string, idx int) bool {
	args := argumentsAfter(tokens, idx)
	next := strings.ToLower(tokenAt(args, 0))
	if commandName(tokens[idx]) == "php" && next == "-r" {
		return true
	}
	if isInlineCodeOption(next) {
		return true
	}
	if next == "-" && strings.HasPrefix(tokenAt(args, 1), "<<") {
		return true
	}
	return strings.HasPrefix(next, "<<") || strings.HasPrefix(next, "<(")
}

func argumentsAfter(tokens []string, idx int) []string {
	if idx+1 >= len(tokens) {
		return nil
	}
	return tokens[idx+1:]
}

func tokenAt(tokens []string, idx int) string {
	if idx >= len(tokens) {
		return ""
	}
	return tokens[idx]
}

func isInlineCodeOption(token string) bool {
	if token == "--eval" || strings.HasPrefix(token, "--eval=") {
		return true
	}
	if !strings.HasPrefix(token, "-") || strings.HasPrefix(token, "--") {
		return false
	}
	letters := token[1:]
	return strings.Trim(letters, "abcdefghijklmnopqrstuvwxyz") == "" && strings.ContainsAny(letters, "ce")
}

func commandName(token string) string {
	return strings.ToLower(path.Base(token))
}

func isInterpreter(token string) bool {
	name := commandName(token)
	switch name {
	case "sh", "bash", "zsh", "dash", "ksh", "fish", "eval", "source", "python", "python2", "python3",
		"perl", "node", "deno", "bun", "ruby", "php", "lua", "tclsh":
		return true
	}
	version, isPython := strings.CutPrefix(name, "python")
	return isPython && strings.Trim(version, "0123456789.") == ""
}

func isDecoder(name string) bool {
	switch name {
	case "base64", "base64_decode", "base32", "xxd", "od", "uudecode", "zcat", "gunzip", "openssl", "rev", "tr":
		return true
	}
	return false
}

func hasDecoder(command string, tokens []string) bool {
	for idx, token := range tokens {
		name := commandName(token)
		if isDecoder(name) {
			return true
		}
		if name == "printf" && strings.Contains(command, `\`) {
			return true
		}
		if name == "echo" && idx+1 < len(tokens) && strings.EqualFold(tokens[idx+1], "-e") {
			return true
		}
	}
	return strings.Contains(command, "$'")
}
