package approval

import (
	"path"
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
	spaced := strings.NewReplacer("<<", " << ", "<(", " <( ", "|", " | ", ";", " ; ", "&", " & ", "(", " ( ", ")", " ) ", "`", " ` ", `"`, " ", "'", " ").Replace(command)
	return strings.Fields(strings.ToLower(spaced))
}

func runsDecodedCode(command string) bool {
	tokens := tokenize(command)
	return runsInterpreter(tokens) && hasDecoder(command, tokens)
}

func runsInterpreter(tokens []string) bool {
	afterPipe := false
	for idx, token := range tokens {
		if token == "|" {
			afterPipe = true
			continue
		}
		if !isInterpreter(token) {
			continue
		}
		if afterPipe || takesInlineCode(tokens, idx) {
			return true
		}
	}
	return false
}

func takesInlineCode(tokens []string, idx int) bool {
	if idx+1 >= len(tokens) {
		return false
	}
	next := tokens[idx+1]
	switch next {
	case "-c", "-e", "--eval":
		return true
	}
	return strings.HasPrefix(next, "<<") || strings.HasPrefix(next, "<(")
}

func isInterpreter(token string) bool {
	name := path.Base(token)
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
	case "base64", "base32", "xxd", "od", "uudecode", "zcat", "gunzip", "openssl", "rev", "tr":
		return true
	}
	return false
}

func hasDecoder(command string, tokens []string) bool {
	for idx, token := range tokens {
		name := path.Base(token)
		if isDecoder(name) {
			return true
		}
		if name == "printf" && strings.Contains(command, `\`) {
			return true
		}
		if name == "echo" && idx+1 < len(tokens) && tokens[idx+1] == "-e" {
			return true
		}
	}
	return strings.Contains(command, "$'")
}
