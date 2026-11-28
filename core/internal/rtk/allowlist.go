package rtk

import "strings"

const (
	shellSyntax = ";&|`$<>()\\\"'*?[]{}~\n\r"
	ghVerbArgs  = 2
)

type argsCheck func(args []string) bool

func readOnlyCheck(program string) (argsCheck, bool) {
	switch program {
	case "ls", "cat", "head", "tail", "wc", "grep":
		return allowAll, true
	case "rg":
		return refusing("--pre"), true
	case "tree":
		return refusingShortLetters("oR"), true
	case "find":
		return refusing("-delete", "-exec", "-ok", "-fprint", "-fls"), true
	case "git":
		return gitReadOnly, true
	case "gh":
		return ghReadOnly, true
	}
	return nil, false
}

func readOnly(command string) bool {
	if strings.ContainsAny(command, shellSyntax) {
		return false
	}
	argv := strings.Fields(command)
	if len(argv) == 0 {
		return false
	}
	check, listed := readOnlyCheck(argv[0])
	return listed && check(argv[1:])
}

func allowAll([]string) bool {
	return true
}

func refusing(prefixes ...string) argsCheck {
	return func(args []string) bool {
		for _, arg := range args {
			for _, prefix := range prefixes {
				if strings.HasPrefix(arg, prefix) {
					return false
				}
			}
		}
		return true
	}
}

func refusingShortLetters(letters string) argsCheck {
	return func(args []string) bool {
		for _, arg := range args {
			if !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") {
				continue
			}
			if strings.ContainsAny(arg[1:], letters) {
				return false
			}
		}
		return true
	}
}

func gitReadOnly(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch {
	case gitReadSubcommand(args[0]):
		return refusing("--output")(args[1:])
	case args[0] == "branch":
		return onlyBranchListing(args[1:])
	}
	return false
}

func gitReadSubcommand(name string) bool {
	switch name {
	case "status", "log", "diff", "show":
		return true
	}
	return false
}

func onlyBranchListing(args []string) bool {
	for _, arg := range args {
		if !gitBranchListingFlag(arg) {
			return false
		}
	}
	return true
}

func gitBranchListingFlag(flag string) bool {
	switch flag {
	case "-a", "--all", "-r", "--remotes", "-v", "-vv", "--verbose", "-l", "--list", "--show-current":
		return true
	}
	return false
}

func ghReadOnly(args []string) bool {
	if len(args) < ghVerbArgs {
		return false
	}
	return ghReadVerb(args[0], args[1]) && refusing("--web", "-w", "--watch")(args[ghVerbArgs:])
}

func ghReadVerb(group, verb string) bool {
	switch group + " " + verb {
	case "pr view", "pr list", "pr diff", "pr checks", "issue view", "issue list", "run view", "run list":
		return true
	}
	return false
}
