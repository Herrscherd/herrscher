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
	err := NewRemote(clitext.NewMessages(), f, "hs", "inst").Materialize(context.Background(), "/srv/work/proj/wt", strings.NewReader(""))
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
	messages := clitext.NewMessages()
	failures := []FailureError{
		NewFailure(messages, clitext.WorktreeFlagBase, clitext.Params{clitext.ParamRef: `"-x"`}, nil),
		NewFailure(messages, clitext.WorktreeOrphanRecovery, clitext.Params{clitext.ParamRemove: NewFailure(messages, clitext.WorktreeRemoveFailed, clitext.Params{clitext.ParamOutput: "o", clitext.ParamErr: cause}, nil), clitext.ParamRecovery: cause}, nil),
		NewFailure(messages, clitext.WorktreeOtherBranch, clitext.Params{clitext.ParamPath: `"p"`, clitext.ParamTip: `"t"`, clitext.ParamBranch: `"b"`}, nil),
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

func TestFailureWithAKeyNoCatalogDeclaresShowsTheKey(t *testing.T) {
	failure := NewFailure(clitext.NewMessages(), clitext.Key("cli.worktree.undeclared"), nil, nil)
	if text := failure.Text(i18n.French); text != "cli.worktree.undeclared" {
		t.Fatalf("undeclared key rendered %q", text)
	}
}
