package drives

import (
	"errors"
	"testing"
)

func TestParsePathRefusesWhatCouldLeaveTheDrive(t *testing.T) {
	cases := []struct {
		raw    string
		clean  string
		refuse bool
	}{
		{raw: "", clean: ""},
		{raw: "Neublox", clean: "Neublox"},
		{raw: "Neublox/notes.md", clean: "Neublox/notes.md"},
		{raw: "Neublox/", clean: "Neublox"},
		{raw: "Mes documents/plan v2.md", clean: "Mes documents/plan v2.md"},
		{raw: "/etc/passwd", refuse: true},
		{raw: "../secret", refuse: true},
		{raw: "Neublox/../../x", refuse: true},
		{raw: "Neublox/./x", refuse: true},
		{raw: "Neublox//x", refuse: true},
		{raw: `Neublox\x`, refuse: true},
		{raw: "Neublox/x\x00", refuse: true},
		{raw: "Neublox/x\nrm", refuse: true},
		{raw: "\xff", refuse: true},
		{raw: "Neublox/caf\u00e9 \u65e5\u672c.md", clean: "Neublox/caf\u00e9 \u65e5\u672c.md"},
		{raw: "Neublox/\u202egnp.exe", refuse: true},
		{raw: "Neu\u200bblox/a.md", refuse: true},
		{raw: "Neublox/a\u00a0b", refuse: true},
		{raw: "Neublox/\u3164", refuse: true},
		{raw: "Neublox/a\ufe0f", refuse: true},
		{raw: "Neublox/a\u034fb", refuse: true},
		{raw: "Neublox/a\u2028b", refuse: true},
		{raw: "Neublox/\ue000", refuse: true},
	}
	for _, tc := range cases {
		path, err := ParsePath(tc.raw)
		if tc.refuse {
			if _, isPath := errors.AsType[*InvalidPathError](err); !isPath {
				t.Errorf("ParsePath(%q) err = %v, want an InvalidPathError", tc.raw, err)
			}
			continue
		}
		if err != nil || path.String() != tc.clean {
			t.Errorf("ParsePath(%q) = %q, %v, want %q", tc.raw, path.String(), err, tc.clean)
		}
	}
}

func TestParseNameKeepsTheRcloneRemoteNameSafe(t *testing.T) {
	cases := map[string]bool{
		"gdrive":                            true,
		"r2-prod_1":                         true,
		"0backup":                           true,
		"":                                  false,
		"-gdrive":                           false,
		"GDrive":                            false,
		"g:drive":                           false,
		"g drive":                           false,
		"a23456789012345678901234567890123": false,
	}
	for raw, valid := range cases {
		_, err := ParseName(raw)
		if (err == nil) != valid {
			t.Errorf("ParseName(%q) err = %v, want valid %v", raw, err, valid)
		}
	}
}

func TestATargetStaysInsideTheExposedFolders(t *testing.T) {
	drive := Drive{Name: "gdrive", Folders: []string{"Neublox", "Karamon/docs"}}
	cases := []struct {
		raw    string
		target string
	}{
		{raw: "Neublox", target: "gdrive:Neublox"},
		{raw: "Neublox/a.md", target: "gdrive:Neublox/a.md"},
		{raw: "Karamon/docs/x", target: "gdrive:Karamon/docs/x"},
		{raw: "Karamon"},
		{raw: "NeubloxOther/a.md"},
		{raw: "Private/a.md"},
	}
	for _, tc := range cases {
		path, err := ParsePath(tc.raw)
		if err != nil {
			t.Fatalf("ParsePath(%q): %v", tc.raw, err)
		}
		target, err := drive.Target(path)
		if tc.target == "" {
			if !errors.Is(err, ErrOutsideFolders) {
				t.Errorf("Target(%q) = %q, %v, want ErrOutsideFolders", tc.raw, target, err)
			}
			continue
		}
		if err != nil || target != tc.target {
			t.Errorf("Target(%q) = %q, %v, want %q", tc.raw, target, err, tc.target)
		}
	}
	whole := Drive{Name: "bucket"}
	anywhere, err := ParsePath("any/where")
	if err != nil {
		t.Fatal(err)
	}
	if target, err := whole.Target(anywhere); err != nil || target != "bucket:any/where" {
		t.Fatalf("a drive with no folder exposes the whole remote, got %q, %v", target, err)
	}
}

func TestAnAgentTheDriveDoesNotNameHoldsTheDefault(t *testing.T) {
	drive := Drive{Name: "gdrive", Default: AccessRead}.WithAccess("writer", AccessWrite).WithAccess("banned", AccessNone)
	cases := map[string]Access{"writer": AccessWrite, "banned": AccessNone, "other": AccessRead, "": AccessRead}
	for agent, want := range cases {
		if got := drive.AccessOf(agent); got != want {
			t.Errorf("AccessOf(%q) = %s, want %s", agent, got, want)
		}
	}
	drive = drive.WithAccess(AllAgents, AccessNone)
	if got := drive.AccessOf("other"); got != AccessNone {
		t.Fatalf("granting * sets the default, got %s", got)
	}
	if !AccessWrite.Allows(AccessRead) || AccessRead.Allows(AccessWrite) || AccessNone.Allows(AccessRead) {
		t.Fatal("write covers read, read does not cover write, none covers nothing")
	}
}
