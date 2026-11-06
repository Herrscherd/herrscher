package tui

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func TestShownWrapSitesKeepTheirSentinel(t *testing.T) {
	cases := []struct {
		name     string
		sentinel error
		produce  func(t *testing.T) error
	}{
		{"colour", errInvalidColor, func(*testing.T) error {
			text := "nope"
			var pal palette
			return colorsFile{Text: &text}.applyTo(&pal)
		}},
		{"unknown key", errUnknownKey, func(t *testing.T) error {
			t.Helper()
			path := filepath.Join(t.TempDir(), "x.toml")
			if err := os.WriteFile(path, []byte("nonsense = 1\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := readThemeFile(path)
			return err
		}},
		{"unknown base", errUnknownBase, func(*testing.T) error {
			_, err := resolveTheme("x", map[string]themeFile{"x": {Base: "nope"}}, make(map[string]bool))
			return err
		}},
		{"unknown option", errUnknownOption, func(*testing.T) error {
			_, err := parseOption("glyphs", "x", glyphModes())
			return err
		}},
		{"refused scheme", errRefusedTarget, func(*testing.T) error {
			return checkURLTarget("ftp://example.org")
		}},
		{"unparsable target", errUnparsableTarget, func(*testing.T) error {
			return checkURLTarget("http://[::1")
		}},
		{"option target", errOptionTarget, func(*testing.T) error {
			return checkTarget("-rf")
		}},
		{"control byte", errControlByte, func(*testing.T) error {
			return checkTarget("a\x01b")
		}},
		{"unsafe draft dir", errUnsafeDraftDir, func(t *testing.T) error {
			t.Helper()
			dir := t.TempDir()
			if err := os.Chmod(dir, 0o777); err != nil {
				t.Fatal(err)
			}
			return checkPrivateDir(dir)
		}},
		{"not regular file", errNotRegularFile, func(t *testing.T) error {
			t.Helper()
			_, err := attachLocalFile(t.TempDir(), 1)
			return err
		}},
		{"attach failure", fs.ErrNotExist, func(t *testing.T) error {
			t.Helper()
			_, err := attachLocalFile(filepath.Join(t.TempDir(), "missing"), 1)
			return err
		}},
		{"unknown change", errUnknownChange, func(*testing.T) error {
			_, err := parseChangeKind("Z")
			return err
		}},
		{"unknown key chord", errKeyUnknown, func(*testing.T) error {
			_, err := canonicalBase("nope", false)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.produce(t)
			if err == nil {
				t.Fatal("the production site returned no error")
			}
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("%q does not wrap its sentinel %q", err, tc.sentinel)
			}
			french := localizedReason(newTranslator().in(i18n.French), err)
			if french == err.Error() || strings.Contains(french, tc.sentinel.Error()) {
				t.Fatalf("the French reason %q was not translated from %q", french, err)
			}
		})
	}
}

func TestAttachFailureKeepsTheOSCause(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	_, err := attachLocalFile(path, 1)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the attach failure %q must keep the OS cause", err)
	}
	french := localizedReason(newTranslator().in(i18n.French), err)
	if want := "impossible de joindre " + path + ": "; !strings.HasPrefix(french, want) {
		t.Fatalf("localizedReason = %q, want prefix %q", french, want)
	}
}
