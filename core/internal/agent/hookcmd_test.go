package agent

import (
	"strings"
	"testing"
)

func TestIsApprovalCommandMatchesOnlyTheHerrscherVerbWords(t *testing.T) {
	cases := map[string]bool{
		`'/bin/herrscher' approve hook`:               true,
		`'/bin/herrscher' approve hook --rtk`:         true,
		`'/opt/it'\''s/herrscher' approve hook`:       true,
		`/usr/local/bin/herrscher approve hook`:       true,
		`'/usr/bin/lint' approve hook`:                false,
		`'/opt/herrscher-lint/tool' approve hook`:     false,
		`'/usr/bin/herrscher-lint' approve hook`:      false,
		`'/bin/herrscher' approve hook --debug`:       false,
		`'/bin/herrscher' approve hook --rtk --debug`: false,
		`'/bin/herrscher' approve`:                    false,
		`'/bin/herrscher' approve hooks`:              false,
		`echo '/bin/herrscher' approve hook`:          false,
		`'/bin/herrscher' approve hook; rm -rf x`:     false,
		`'/bin/my approve hook'`:                      false,
		`'/bin/herrscher approve hook`:                false,
		``:                                            false,
	}
	for command, want := range cases {
		if got := isApprovalCommand(command); got != want {
			t.Errorf("isApprovalCommand(%q) = %v, want %v", command, got, want)
		}
	}
}

func TestInjectApprovalHookKeepsANeighbouringHookThatNamesTheVerb(t *testing.T) {
	prior := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"'/usr/bin/lint' approve hook"}]}]}}`)
	out, err := injectApprovalHook(prior, "/bin/herrscher", true)
	if err != nil {
		t.Fatalf("injectApprovalHook: %v", err)
	}
	if !strings.Contains(string(out), "'/usr/bin/lint' approve hook") {
		t.Fatalf("the neighbouring hook was dropped: %s", out)
	}
	if n := strings.Count(string(out), "approve hook"); n != 2 {
		t.Fatalf("%d approve hook commands, want the neighbour and ours: %s", n, out)
	}
}
