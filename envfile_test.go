package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "herrscher.env")
	content := "# a comment\n" +
		"\n" +
		"HERRSCHER_TEST_TOKEN=abc.def-ghi\n" +
		"export HERRSCHER_TEST_EXPORTED=fromfile\n" +
		"export\tHERRSCHER_TEST_TABEXPORT=tabbed\n" +
		`HERRSCHER_TEST_QUOTED="a b c"` + "\n" +
		"HERRSCHER_TEST_EQUALS=k=v=w\n" +
		"   HERRSCHER_TEST_SPACED   =  trimmed  \n" +
		"#HERRSCHER_TEST_COMMENTED=nope\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{
		"HERRSCHER_TEST_TOKEN", "HERRSCHER_TEST_EXPORTED", "HERRSCHER_TEST_TABEXPORT", "HERRSCHER_TEST_QUOTED",
		"HERRSCHER_TEST_EQUALS", "HERRSCHER_TEST_SPACED", "HERRSCHER_TEST_COMMENTED",
	} {
		os.Unsetenv(k)
		t.Cleanup(func() { os.Unsetenv(k) })
	}
	if err := loadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"HERRSCHER_TEST_TOKEN":     "abc.def-ghi",
		"HERRSCHER_TEST_EXPORTED":  "fromfile", // leading `export ` stripped
		"HERRSCHER_TEST_TABEXPORT": "tabbed",   // `export\t` (tab) stripped too
		"HERRSCHER_TEST_QUOTED":    "a b c",    // surrounding quotes stripped, space preserved
		"HERRSCHER_TEST_EQUALS":    "k=v=w",    // only the first '=' splits
		"HERRSCHER_TEST_SPACED":    "trimmed",  // key and value trimmed
		"HERRSCHER_TEST_COMMENTED": "",         // commented line ignored
	}
	for k, want := range cases {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestLoadEnvFileRespectsExistingEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "herrscher.env")
	if err := os.WriteFile(path, []byte("HERRSCHER_TEST_PRESET=fromfile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERRSCHER_TEST_PRESET", "fromenv")
	if err := loadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("HERRSCHER_TEST_PRESET"); got != "fromenv" {
		t.Errorf("real env should win over the file, got %q", got)
	}
}

func TestLoadEnvFileMissingIsOK(t *testing.T) {
	if err := loadEnvFile(filepath.Join(t.TempDir(), "absent.env")); err != nil {
		t.Errorf("missing env file should be a no-op, got %v", err)
	}
}
