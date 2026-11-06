package worktree

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

func TestRemoteMaterializeFailureReadsInFrench(t *testing.T) {
	f := &fakeCommander{fail: true}
	err := NewRemote(f, "hs", "inst").Materialize(context.Background(), "/srv/work/proj/wt", strings.NewReader(""))
	if err == nil {
		t.Fatal("want a failure from the remote materialize")
	}
	var failure FailureError
	if !errors.As(err, &failure) {
		t.Fatalf("err = %T %v, want a FailureError", err, err)
	}
	french := failure.Text(i18n.French)
	if !strings.HasPrefix(french, "matérialisation sur fake : ") || !strings.Contains(french, "boom") {
		t.Fatalf("french = %q", french)
	}
	if strings.Contains(french, "materialize on") {
		t.Fatalf("english prefix reached the french operator: %q", french)
	}
	if english := err.Error(); !strings.HasPrefix(english, "materialize on fake: ") {
		t.Fatalf("english = %q", english)
	}
}

func TestEveryFailureKeyRendersInEachLanguage(t *testing.T) {
	cause := errors.New("cause")
	failures := []FailureError{
		{Key: clitext.WorktreeFlagBase, Params: clitext.Params{clitext.ParamRef: `"-x"`}},
		{Key: clitext.WorktreeOrphanRecovery, Params: clitext.Params{clitext.ParamRemove: FailureError{Key: clitext.WorktreeRemoveFailed, Params: clitext.Params{clitext.ParamOutput: "o", clitext.ParamErr: cause}}, clitext.ParamRecovery: cause}},
		{Key: clitext.WorktreeOtherBranch, Params: clitext.Params{clitext.ParamPath: `"p"`, clitext.ParamTip: `"t"`, clitext.ParamBranch: `"b"`}},
	}
	for _, failure := range failures {
		for _, lang := range []i18n.Language{i18n.English, i18n.French} {
			text := failure.Text(lang)
			if text == string(failure.Key) {
				t.Fatalf("%s did not render in %v", failure.Key, lang)
			}
		}
	}
}
