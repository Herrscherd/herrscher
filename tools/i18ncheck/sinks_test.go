package main

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func textsOf(found []finding) []string {
	texts := make([]string, 0, len(found))
	for _, sentence := range found {
		texts = append(texts, sentence.text)
	}
	slices.Sort(texts)
	return texts
}

func collectMutant(t *testing.T, directory, file, source string) map[string][]finding {
	t.Helper()
	root := t.TempDir()
	target := filepath.Join(root, filepath.FromSlash(directory))
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, file), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	found, err := collect(root, []string{directory})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func TestExtendedFixtureFindsExactlyTheSentences(t *testing.T) {
	t.Parallel()
	found, err := collect("testdata/extended", []string{"manage", "core/cli", "core/host", "core/internal/manager", "plugins/terminal"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"manage": {
			"Count: ",
			"Route policy was changed",
			"Session  was stopped",
			"Session stopped unexpectedly",
			"Session stopped unexpectedly",
			"The reply was cut short",
			"The session has stopped",
		},
		"core/cli":              {"cli: programmer message outside the list", "needs a value", "unknown command"},
		"core/host":             {"no session is running here"},
		"core/internal/manager": {"manager has no active lease"},
		"plugins/terminal":      {"plugin has no terminal surface"},
	}
	for key, texts := range want {
		if got := textsOf(found[key]); !slices.Equal(got, texts) {
			t.Errorf("%s: got %q, want %q", key, got, texts)
		}
	}
	for key, sentences := range found {
		if _, expected := want[key]; !expected && len(sentences) > 0 {
			t.Errorf("%s: unexpected findings %q", key, textsOf(sentences))
		}
	}
}

func TestProgrammerErrorsListIsExplicit(t *testing.T) {
	t.Parallel()
	var texts []string
	for _, entry := range exemptions() {
		if entry.why != programmerError {
			continue
		}
		if entry.directory != "core/cli" || entry.file != "registry.go" {
			t.Errorf("programmer error %q is outside core/cli/registry.go", entry.text)
		}
		texts = append(texts, entry.text)
	}
	want := []string{"cli: command with empty path", "cli: duplicate command", "command %q: %w", "param %q: %w"}
	if !slices.Equal(texts, want) {
		t.Fatalf("programmer errors %q, want exactly %q", texts, want)
	}
}

func TestProgrammerErrorIsCountedOutsideItsFile(t *testing.T) {
	t.Parallel()
	found := collectMutant(t, "manage", "elsewhere.go", "package manage\n\nvar errTwice = errors.New(\"cli: duplicate command\")\n")
	if got := textsOf(found["manage"]); !slices.Equal(got, []string{"cli: duplicate command"}) {
		t.Fatalf("the programmer exemption leaked outside core/cli/registry.go: %q", got)
	}
}

func TestMutationsOnePerExtension(t *testing.T) {
	t.Parallel()
	for name, mutant := range map[string]struct {
		directory string
		file      string
		source    string
		want      string
	}{
		"key value on a model flash": {
			directory: "manage",
			file:      "mutant.go",
			source:    "package manage\n\nfunc mutant(m *model) {\n\tm.tr = model{flash: \"Clipboard handed over to the terminal\"}\n}\n",
			want:      "Clipboard handed over to the terminal",
		},
		"key value on a contracts event text": {
			directory: "manage",
			file:      "mutant.go",
			source:    "package manage\n\nvar mutant = contracts.Event{T: \"notice\", Text: \"The upload was refused\"}\n",
			want:      "The upload was refused",
		},
		"binary concatenation of strings": {
			directory: "manage",
			file:      "mutant.go",
			source:    "package manage\n\nfunc mutant(m *model, name string) {\n\tm.flash = \"Upload of \" + name + \" was refused\"\n}\n",
			want:      "Upload of  was refused",
		},
		"package constant reaching a sink": {
			directory: "manage",
			file:      "mutant.go",
			source:    "package manage\n\nconst refusedNotice = \"The upload was refused\"\n\nfunc mutant(m *model) {\n\tm.flash = refusedNotice\n}\n",
			want:      "The upload was refused",
		},
		"contracts text argument": {
			directory: "manage",
			file:      "mutant.go",
			source:    "package manage\n\nvar mutant = contracts.Text(\"Reason\", \"The upload was refused\")\n",
			want:      "The upload was refused",
		},
		"help text of a command": {
			directory: "manage",
			file:      "mutant.go",
			source:    "package manage\n\nvar mutant = contracts.New(\"upload\", \"retry\").Help(\"Start the upload again\")\n",
			want:      "Start the upload again",
		},
		"help text of a param": {
			directory: "manage",
			file:      "mutant.go",
			source:    "package manage\n\nvar mutant = contracts.New(\"upload\").Param(\"name\", \"Name of the upload to retry\", true)\n",
			want:      "Name of the upload to retry",
		},
		"help text of a value param": {
			directory: "manage",
			file:      "mutant.go",
			source:    "package manage\n\nvar mutant = contracts.New(\"upload\").ValueParam(\"name\", \"Name of the upload to retry\", true)\n",
			want:      "Name of the upload to retry",
		},
		"core host scope": {
			directory: "core/host",
			file:      "mutant.go",
			source:    "package host\n\nvar errMutant = errors.New(\"the upload was refused\")\n",
			want:      "the upload was refused",
		},
		"core internal manager scope": {
			directory: "core/internal/manager",
			file:      "mutant.go",
			source:    "package manager\n\nvar errMutant = errors.New(\"the upload was refused\")\n",
			want:      "the upload was refused",
		},
		"plugins terminal scope": {
			directory: "plugins/terminal",
			file:      "mutant.go",
			source:    "package terminal\n\nvar errMutant = errors.New(\"the upload was refused\")\n",
			want:      "the upload was refused",
		},
		"joined literal list": {
			directory: "manage",
			file:      "mutant.go",
			source:    "package manage\n\nfunc mutant(m *model) {\n\tm.flash = strings.Join([]string{\"Session\", \"was\", \"stopped\"}, \" \")\n}\n",
			want:      "Session was stopped",
		},
		"format passed as a parameter": {
			directory: "manage",
			file:      "mutant.go",
			source:    "package manage\n\nfunc mutant(m *model, format string) {\n\tm.flash = fmt.Sprintf(format, 3)\n}\n",
			want:      dynamicFormatText,
		},
		"banner flash field": {
			directory: "manage",
			file:      "mutant.go",
			source:    "package manage\n\nfunc mutant(m *model) {\n\tm.bannerFlash = \"The upload was refused\"\n}\n",
			want:      "The upload was refused",
		},
		"status field": {
			directory: "manage",
			file:      "mutant.go",
			source:    "package manage\n\nfunc mutant(m *model) {\n\tm.status = \"The upload was refused\"\n}\n",
			want:      "The upload was refused",
		},
		"contracts arg value": {
			directory: "manage",
			file:      "mutant.go",
			source:    "package manage\n\nvar mutant = contracts.Arg{Name: \"Reason\", Value: \"The upload was refused\"}\n",
			want:      "The upload was refused",
		},
		"programmer message not in the closed list": {
			directory: "core/cli",
			file:      "registry.go",
			source:    "package cli\n\nvar errMutant = errors.New(\"cli: duplicate command again\")\n",
			want:      "cli: duplicate command again",
		},
	} {
		found := collectMutant(t, mutant.directory, mutant.file, mutant.source)
		if got := textsOf(found[mutant.directory]); !slices.Contains(got, mutant.want) {
			t.Errorf("%s: %q not found in %q", name, mutant.want, got)
		}
	}
}

func TestCyclicConstantsDoNotLoop(t *testing.T) {
	t.Parallel()
	found := collectMutant(t, "manage", "cycle.go", "package manage\n\nconst pingNotice = pongNotice\n\nconst pongNotice = pingNotice\n\nconst selfNotice = selfNotice\n\nfunc mutant(m *model) {\n\tm.flash = pingNotice\n\tm.flash = selfNotice\n}\n")
	if got := textsOf(found["manage"]); len(got) != 0 {
		t.Fatalf("a cyclic constant produced findings %q", got)
	}
}

func TestProgrammerErrorNeedsAPathBoundary(t *testing.T) {
	t.Parallel()
	const message = "cli: duplicate command"
	if !isExempt(token.Position{Filename: "/work/core/cli/registry.go"}, message) {
		t.Fatal("the programmer message in core/cli/registry.go is not exempt")
	}
	if isExempt(token.Position{Filename: "/work/xcore/cli/registry.go"}, message) {
		t.Fatal("a file whose path only ends with core/cli/registry.go is exempt")
	}
}

func TestExemptionReasonsAreClosed(t *testing.T) {
	t.Parallel()
	unknown := []exemption{{directory: "manage", file: "x.go", text: "a b", why: reasonUnknown}}
	if err := validateExemptions(unknown); !errors.Is(err, errUnknownReason) {
		t.Fatalf("an exemption without a known reason passed validation: %v", err)
	}
	bare := []exemption{{directory: "manage", file: "x.go", text: "read config", why: technicalWrap}}
	if err := validateExemptions(bare); !errors.Is(err, errWrapNotFormat) {
		t.Fatalf("a technical wrap that does not end with %%w passed validation: %v", err)
	}
	if err := validateExemptions(exemptions()); err != nil {
		t.Fatalf("the repository exemptions are refused: %v", err)
	}
}

func TestKeyedSentinelAcceptsOnlyCatalogReferences(t *testing.T) {
	t.Parallel()
	source := "package manage\n\nvar errWrapped = keyed{errKnown, clitext.CommandUnknown}\nvar errOther = keyed{errLocal, other.Name}\n"
	file, err := parser.ParseFile(token.NewFileSet(), "sentinels.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	translated := keyedSentinelNames([]*ast.File{file})
	if !translated["errKnown"] {
		t.Fatal("a sentinel keyed by a clitext reference is not treated as translated")
	}
	if translated["errLocal"] {
		t.Fatal("a sentinel keyed by a foreign selector is treated as translated")
	}
}

func TestMissingScopeDirectoryIsAnError(t *testing.T) {
	t.Parallel()
	if _, err := collect(t.TempDir(), []string{"manage"}); err == nil {
		t.Fatal("a missing directory of the scope was read as if it were empty")
	}
}

func TestStandardStreamAndLogPrintsAreSinks(t *testing.T) {
	t.Parallel()
	for name, mutant := range map[string]struct {
		source string
		want   string
	}{
		"fmt.Println": {
			source: "package manage\n\nfunc mutant() {\n\tfmt.Println(\"The upload was refused\")\n}\n",
			want:   "The upload was refused",
		},
		"fmt.Printf format": {
			source: "package manage\n\nfunc mutant() {\n\tfmt.Printf(\"The upload was refused\\n\")\n}\n",
			want:   "The upload was refused\n",
		},
		"fmt.Fprintln to os.Stderr": {
			source: "package manage\n\nfunc mutant() {\n\tfmt.Fprintln(os.Stderr, \"The upload was refused\")\n}\n",
			want:   "The upload was refused",
		},
		"fmt.Fprintf to os.Stdout": {
			source: "package manage\n\nfunc mutant() {\n\tfmt.Fprintf(os.Stdout, \"The upload was refused\")\n}\n",
			want:   "The upload was refused",
		},
		"log.Print": {
			source: "package manage\n\nfunc mutant() {\n\tlog.Print(\"The upload was refused\")\n}\n",
			want:   "The upload was refused",
		},
		"log.Printf": {
			source: "package manage\n\nfunc mutant() {\n\tlog.Printf(\"The upload was refused: %s\", reason)\n}\n",
			want:   "The upload was refused: %s",
		},
	} {
		found := collectMutant(t, "manage", "mutant.go", mutant.source)
		if got := textsOf(found["manage"]); !slices.Contains(got, mutant.want) {
			t.Errorf("%s: %q not found in %q", name, mutant.want, got)
		}
	}
}

func TestWriterOtherThanAStandardStreamIsNotShown(t *testing.T) {
	t.Parallel()
	found := collectMutant(t, "manage", "mutant.go", "package manage\n\nfunc mutant(buf *bytes.Buffer) {\n\tfmt.Fprintln(buf, \"The upload was refused\")\n\tfmt.Fprintf(buf, \"The upload was refused\")\n}\n")
	if got := textsOf(found["manage"]); len(got) != 0 {
		t.Fatalf("a write to a buffer was reported as shown to a person: %q", got)
	}
}

func TestLocalLiteralReachesItsSinks(t *testing.T) {
	t.Parallel()
	found := collectMutant(t, "manage", "mutant.go", "package manage\n\nfunc mutant(m *model) error {\n\tmsg := \"The upload was refused\"\n\tm.flash = msg\n\treturn errors.New(msg)\n}\n")
	want := []string{"The upload was refused", "The upload was refused"}
	if got := textsOf(found["manage"]); !slices.Equal(got, want) {
		t.Fatalf("local literal findings %q, want %q", got, want)
	}
}

func TestLocalWithSeveralAssignmentsReportsEachValue(t *testing.T) {
	t.Parallel()
	found := collectMutant(t, "manage", "mutant.go", "package manage\n\nfunc mutant(m *model, late bool) {\n\tmsg := \"The upload was refused\"\n\tif late {\n\t\tmsg = \"The upload was cancelled\"\n\t}\n\tm.flash = msg\n}\n")
	got := textsOf(found["manage"])
	for _, want := range []string{"The upload was refused", "The upload was cancelled"} {
		if !slices.Contains(got, want) {
			t.Fatalf("%q not reported among %q", want, got)
		}
	}
}

func TestLocalDeclaredThenAssignedIsReported(t *testing.T) {
	t.Parallel()
	found := collectMutant(t, "manage", "mutant.go", "package manage\n\nfunc mutant(m *model, ok bool) {\n\tvar msg string\n\tif ok {\n\t\tmsg = \"Upload was refused by the server\"\n\t}\n\tm.flash = msg\n}\n")
	if got := textsOf(found["manage"]); !slices.Contains(got, "Upload was refused by the server") {
		t.Fatalf("the literal assigned to a declared local was not reported: %q", got)
	}
}

func TestLocalMixingLiteralAndCallReportsTheLiteral(t *testing.T) {
	t.Parallel()
	found := collectMutant(t, "manage", "mutant.go", "package manage\n\nfunc mutant(m *model, reason func() string, ok bool) {\n\tmsg := reason()\n\tif ok {\n\t\tmsg = \"Upload was refused by the server\"\n\t}\n\tm.flash = msg\n\tm.notice = reason()\n}\n")
	got := textsOf(found["manage"])
	if !slices.Contains(got, "Upload was refused by the server") {
		t.Fatalf("the literal next to a call was not reported: %q", got)
	}
	if len(got) != 1 {
		t.Fatalf("a call was reported as a sentence: %q", got)
	}
}

func TestWriterLocalToStandardStreamIsShown(t *testing.T) {
	t.Parallel()
	found := collectMutant(t, "manage", "mutant.go", "package manage\n\nfunc mutant() {\n\tout := os.Stdout\n\tfmt.Fprintln(out, \"The upload was refused\")\n}\n")
	if got := textsOf(found["manage"]); !slices.Contains(got, "The upload was refused") {
		t.Fatalf("a write through a local bound to os.Stdout was not reported: %q", got)
	}
}

func TestWriterLocalToBufferIsNotShown(t *testing.T) {
	t.Parallel()
	found := collectMutant(t, "manage", "mutant.go", "package manage\n\nfunc mutant(buf *bytes.Buffer) {\n\tout := buf\n\tfmt.Fprintln(out, \"The upload was refused\")\n}\n")
	if got := textsOf(found["manage"]); len(got) != 0 {
		t.Fatalf("a write through a local bound to a buffer was reported: %q", got)
	}
}

func TestLocalShadowsPackageConstant(t *testing.T) {
	t.Parallel()
	found := collectMutant(t, "manage", "mutant.go", "package manage\n\nconst refusedNotice = \"The upload was refused\"\n\nfunc mutant(m *model) {\n\trefusedNotice := \"The upload was cancelled\"\n\tm.flash = refusedNotice\n}\n")
	if got := textsOf(found["manage"]); !slices.Equal(got, []string{"The upload was cancelled"}) {
		t.Fatalf("the local did not shadow the package constant: %q", got)
	}
}

func TestLocalBuiltFromAFormatIsResolved(t *testing.T) {
	t.Parallel()
	found := collectMutant(t, "manage", "mutant.go", "package manage\n\nfunc mutant(m *model, name string) {\n\tmsg := fmt.Sprintf(\"Upload of %s was refused\", name)\n\tm.flash = msg\n}\n")
	if got := textsOf(found["manage"]); !slices.Equal(got, []string{"Upload of %s was refused"}) {
		t.Fatalf("a local built with fmt.Sprintf was not resolved to its format: %q", got)
	}
}

func TestJoinWithALocalAssignedAtRunTimeIsDynamic(t *testing.T) {
	t.Parallel()
	found := collectMutant(t, "manage", "mutant.go", "package manage\n\nfunc mutant(m *model, reason func() string) {\n\tmsg := reason()\n\tm.flash = msg + \" now\"\n}\n")
	got := textsOf(found["manage"])
	if !slices.Contains(got, dynamicJoinText) {
		t.Fatalf("a literal joined with a run-time local was not reported as dynamic: %q", got)
	}
	if slices.Contains(got, " now") {
		t.Fatalf("the fragment alone was reported as a sentence: %q", got)
	}
}

func TestJoinResolvesTheLocalUnion(t *testing.T) {
	t.Parallel()
	found := collectMutant(t, "manage", "mutant.go", "package manage\n\nfunc mutant(m *model, late bool, reason func() string) {\n\tmsg := \"The upload was refused\"\n\tif late {\n\t\tmsg = reason()\n\t}\n\tm.flash = msg + \" now\"\n}\n")
	got := textsOf(found["manage"])
	if !slices.Contains(got, "The upload was refused now") {
		t.Fatalf("the literal alternative of the local was not reported: %q", got)
	}
	if slices.Contains(got, " now") {
		t.Fatalf("the fragment alone was reported as a sentence: %q", got)
	}
}

func TestJoinOfRunTimeValuesWithoutProseIsNotReported(t *testing.T) {
	t.Parallel()
	found := collectMutant(t, "manage", "mutant.go", "package manage\n\nfunc mutant(m *model, note string, help func() string) {\n\tmsg := help()\n\tm.flash = msg + \"\\n\" + note\n}\n")
	if got := textsOf(found["manage"]); len(got) != 0 {
		t.Fatalf("a join of run-time values without words was reported: %q", got)
	}
}
