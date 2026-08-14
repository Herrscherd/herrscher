package manage

import (
	"flag"
	"reflect"
	"testing"
)

func TestSetPlugins(t *testing.T) {
	mods := []string{"example.com/a", "example.com/b"}
	out, err := setPlugins(sample, mods)
	if err != nil {
		t.Fatal(err)
	}
	got, err := listPlugins(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, mods) {
		t.Fatalf("got %v, want %v", got, mods)
	}
}

func TestResolveStackDefault(t *testing.T) {
	mods, err := resolveStack(defaultStack, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		inTree[0],
		catalog["gateway"]["discord"],
		catalog["backend"]["claude"],
		catalog["memory"]["obsidian"],
		catalog["orchestrator"]["basic"],
		catalog["extractor"]["llm"],
		catalog["skills"]["superset"],
	}
	if !reflect.DeepEqual(mods, want) {
		t.Fatalf("got %v, want %v", mods, want)
	}
}

func TestResolveStackNoneDropsCategory(t *testing.T) {
	choices := map[string]string{"gateway": "discord", "backend": "none", "memory": "none", "orchestrator": "none"}
	mods, err := resolveStack(choices, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{inTree[0], catalog["gateway"]["discord"]}
	if !reflect.DeepEqual(mods, want) {
		t.Fatalf("got %v, want %v", mods, want)
	}
}

func TestResolveStackExtrasDeduped(t *testing.T) {
	choices := map[string]string{"gateway": "discord"}
	mods, err := resolveStack(choices, []string{"example.com/x", catalog["gateway"]["discord"]})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{inTree[0], catalog["gateway"]["discord"], "example.com/x"}
	if !reflect.DeepEqual(mods, want) {
		t.Fatalf("got %v, want %v", mods, want)
	}
}

func TestResolveStackRejectsFlagLikeExtra(t *testing.T) {
	if _, err := resolveStack(map[string]string{"gateway": "discord"}, []string{"-insecure"}); err == nil {
		t.Fatal("expected error for flag-like --with value")
	}
}

func TestResolveStackUnknownKind(t *testing.T) {
	if _, err := resolveStack(map[string]string{"gateway": "bogus"}, nil); err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

// An all-"none" stack is no longer empty: the in-tree terminal gateway is always
// compiled in, so the host still has a usable front end and `init` must not fail.
func TestResolveStackAllNoneKeepsInTree(t *testing.T) {
	choices := map[string]string{"gateway": "none", "backend": "none", "memory": "none", "orchestrator": "none", "extractor": "none"}
	mods, err := resolveStack(choices, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mods, inTree) {
		t.Fatalf("got %v, want %v", mods, inTree)
	}
}

// The in-tree terminal gateway must survive every recomposition — dropping it
// would silently remove the only front end that needs no Discord token.
func TestResolveStackAlwaysKeepsTerminal(t *testing.T) {
	mods, err := resolveStack(map[string]string{"gateway": "discord"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range mods {
		if m == inTree[0] {
			return
		}
	}
	t.Fatalf("in-tree terminal gateway missing from %v", mods)
}

// Every flag that shapes the resolved stack must suppress the wizard —
// otherwise the wizard's answers silently overwrite what the operator asked
// for. --extractor was omitted from that set once; this pins the whole list.
func TestStackFlagsSetCoversEveryStackFlag(t *testing.T) {
	for _, name := range []string{"gateway", "backend", "memory", "orchestrator", "extractor", "with"} {
		fs := flag.NewFlagSet("init", flag.ContinueOnError)
		fs.String("gateway", "", "")
		fs.String("backend", "", "")
		fs.String("memory", "", "")
		fs.String("orchestrator", "", "")
		fs.String("extractor", "", "")
		fs.String("with", "", "")
		if err := fs.Parse([]string{"-" + name, "x"}); err != nil {
			t.Fatalf("parse -%s: %v", name, err)
		}
		if !stackFlagsSet(fs) {
			t.Errorf("stackFlagsSet false after -%s: the wizard would overwrite it", name)
		}
	}
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.String("gateway", "", "")
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if stackFlagsSet(fs) {
		t.Error("stackFlagsSet true with no flags passed")
	}
}

// A skills plugin is composed like any other category, and dropped like one. The
// second half is what makes it a category rather than a hardcoded extra.
func TestResolveStackSkillsIsACategory(t *testing.T) {
	if defaultStack["skills"] == "" {
		t.Fatal("the default composition must hydrate herrscher with playbooks")
	}
	mods, err := resolveStack(map[string]string{"backend": "claude", "skills": "none"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range mods {
		if m == catalog["skills"]["superset"] {
			t.Fatalf("skills=none must drop the module, got %v", mods)
		}
	}
}
