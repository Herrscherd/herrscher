package service

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteFileNeverOverwritesTemplate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "herrscher.env")
	if err := os.WriteFile(path, []byte("GW_TOKEN=real-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(FileWrite{Path: path, Content: renderEnvTemplate(testEnvVars()), Mode: 0o600, Template: true}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	must(t, err)
	if !strings.Contains(string(got), "real-secret") {
		t.Fatalf("template clobbered existing secrets: %q", got)
	}
	fresh := filepath.Join(dir, "sub", "new.env")
	if err := writeFile(FileWrite{Path: fresh, Content: renderEnvTemplate(testEnvVars()), Mode: 0o600, Template: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("template not written to fresh path: %v", err)
	}
}

func testConfig(goos string) Config {
	return Config{
		GOOS:       goos,
		BinPath:    "/home/me/.local/bin/herrscher",
		Home:       "/home/me",
		User:       "me",
		EnvFile:    "/home/me/.config/herrscher/herrscher.env",
		HealthAddr: "127.0.0.1:8787",
		EnvVars:    testEnvVars(),
	}
}

func testEnvVars() []EnvVar {
	return []EnvVar{
		{Key: "GW_TOKEN", Help: "gateway token", Required: true},
		{Key: "HERRSCHER_OWNER_ID", Help: "owner id (per-daemon instance-id fallback)"},
	}
}

func TestServeArgs(t *testing.T) {
	got := serveArgs(Config{EnvFile: "/e", ExtraArgs: []string{"--status-channel", "42"}})
	want := "serve --env-file /e --status-channel 42"
	if strings.Join(got, " ") != want {
		t.Fatalf("serveArgs = %q, want %q", strings.Join(got, " "), want)
	}
	if strings.Join(serveArgs(Config{HealthAddr: "127.0.0.1:8787"}), " ") != "serve" {
		t.Fatalf("health-addr must not be baked into serveArgs, got %v", serveArgs(Config{HealthAddr: "127.0.0.1:8787"}))
	}
}

func TestLinuxPlan(t *testing.T) {
	p, err := BuildPlan(testConfig("linux"))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) != 2 {
		t.Fatalf("want unit + env file, got %d files", len(p.Files))
	}
	unit := p.Files[0]
	if !strings.HasSuffix(unit.Path, "/.config/systemd/user/herrscher.service") {
		t.Fatalf("unit path = %s", unit.Path)
	}
	for _, want := range []string{
		"ExecStart=/home/me/.local/bin/herrscher serve --env-file /home/me/.config/herrscher/herrscher.env",
		"Restart=always",
		"StartLimitBurst=5",
		"WantedBy=default.target",
	} {
		if !strings.Contains(unit.Content, want) {
			t.Errorf("unit missing %q\n---\n%s", want, unit.Content)
		}
	}
	if strings.Contains(unit.Content, "EnvironmentFile") {
		t.Error("unit should not declare EnvironmentFile anymore")
	}
	if strings.Contains(unit.Content, "TOKEN=") {
		t.Error("unit file embeds a secret value")
	}
	env := p.Files[1]
	if !env.Template || env.Mode != 0o600 {
		t.Fatalf("env file should be a 0600 template, got mode=%o template=%v", env.Mode, env.Template)
	}
	if strings.Contains(env.Content, "GW_TOKEN=x") || !strings.Contains(env.Content, "GW_TOKEN=") {
		t.Errorf("env template should list the var with no value:\n%s", env.Content)
	}
	assertCmd(t, p, "systemctl --user enable --now herrscher.service")
	assertCmd(t, p, "loginctl enable-linger me")
}

func TestUnitCarriesTheOperatorsPath(t *testing.T) {
	c := testConfig("linux")
	c.Path = "/opt/tool/bin:/usr/bin"
	p, err := BuildPlan(c)
	if err != nil {
		t.Fatal(err)
	}
	line := unitPathOf(t, p.Files[0].Content)
	for _, want := range []string{
		"/opt/tool/bin",
		"/home/me/.local/bin",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("PATH missing %q: %s", want, line)
		}
	}
}

func TestUnitPathFallsBackToTheUsualDirs(t *testing.T) {
	p, err := BuildPlan(testConfig("linux"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/home/me/.local/bin", "/usr/bin"} {
		if !strings.Contains(p.Files[0].Content, want) {
			t.Errorf("default PATH missing %q:\n%s", want, p.Files[0].Content)
		}
	}
}

func TestPlistCarriesThePath(t *testing.T) {
	c := testConfig("darwin")
	c.Path = "/opt/tool/bin:/usr/bin"
	p, err := BuildPlan(c)
	if err != nil {
		t.Fatal(err)
	}
	plist := p.Files[0].Content
	if !strings.Contains(plist, "<key>EnvironmentVariables</key>") || !strings.Contains(plist, "/opt/tool/bin") {
		t.Errorf("plist carries no PATH:\n%s", plist)
	}
}

func TestUnitPathSurvivesASpaceInADirectory(t *testing.T) {
	c := testConfig("linux")
	c.Path = "/opt/my tools/bin:/usr/bin"
	p, err := BuildPlan(c)
	if err != nil {
		t.Fatal(err)
	}
	line := unitPathOf(t, p.Files[0].Content)
	if !strings.HasPrefix(line, `Environment="PATH=`) {
		t.Errorf("a PATH with a space must be quoted as one value: %s", line)
	}
	if !strings.Contains(line, "/opt/my tools/bin") {
		t.Errorf("PATH lost the entry with a space: %s", line)
	}
	c2, unit := writeUnit(t, "[Unit]\n\n[Service]\nEnvironment=PATH=/usr/bin\nExecStart=/x serve\n\n[Install]\n")
	c2.Path = c.Path
	if changed, syncErr := syncUnitPath(c2); syncErr != nil || !changed {
		t.Fatalf("changed=%v err=%v, want a rewrite", changed, syncErr)
	}
	b, err := os.ReadFile(unit)
	must(t, err)
	if strings.Count(string(b), "PATH=") != 1 {
		t.Errorf("want exactly one PATH directive:\n%s", b)
	}
	if changed, err := syncUnitPath(c2); err != nil || changed {
		t.Fatalf("changed=%v err=%v, want a no-op on the second run", changed, err)
	}
}

func unitPathOf(t *testing.T, unit string) string {
	t.Helper()
	for _, l := range strings.Split(unit, "\n") {
		if isUnitPathLine(l) {
			return l
		}
	}
	t.Fatalf("unit declares no PATH:\n%s", unit)
	return ""
}

func TestUnitPathCannotInjectDirectives(t *testing.T) {
	c := testConfig("linux")
	c.Path = "/opt/bin\nExecStartPre=/bin/rm -rf /\n"
	p, err := BuildPlan(c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(p.Files[0].Content, "ExecStartPre") {
		t.Fatalf("a newline in PATH injected a unit directive:\n%s", p.Files[0].Content)
	}
}

func TestUnitPathDropsVolatileAndRelativeEntries(t *testing.T) {
	c := testConfig("linux")
	c.Path = "/tmp/.mount_appXYZ:/keep/bin:relative/bin:/var/tmp/x:/dev/shm/y:/run/user/1000/z:."
	p, err := BuildPlan(c)
	if err != nil {
		t.Fatal(err)
	}
	line := unitPathOf(t, p.Files[0].Content)
	for _, bad := range []string{".mount_appXYZ", "relative/bin", "/var/tmp/x", "/dev/shm/y", "/run/user"} {
		if strings.Contains(line, bad) {
			t.Errorf("PATH kept a volatile entry %q: %s", bad, line)
		}
	}
	if !strings.Contains(line, "/keep/bin") {
		t.Errorf("PATH dropped a legitimate entry: %s", line)
	}
	for _, e := range strings.Split(strings.TrimPrefix(line, "Environment=PATH="), ":") {
		if !strings.HasPrefix(e, "/") {
			t.Errorf("PATH contains a non-absolute entry %q: %s", e, line)
		}
	}
}

func writeUnit(t *testing.T, content string) (Config, string) {
	t.Helper()
	home := t.TempDir()
	c := testConfig("linux")
	c.Home = home
	c.Path = "/opt/tool/bin"
	unit := filepath.Join(home, ".config", "systemd", "user", "herrscher.service")
	if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unit, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return c, unit
}

func TestUpdateAddsThePathToAUnitThatPredatesIt(t *testing.T) {
	c, unit := writeUnit(t, "[Unit]\nDescription=x\n\n[Service]\nType=simple\n"+
		"ExecStart=/home/me/.local/bin/herrscher serve --health-addr 127.0.0.1:9999\n"+
		"Restart=always\n\n[Install]\nWantedBy=default.target\n")
	changed, err := syncUnitPath(c)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("a unit with no PATH must be reported as changed")
	}
	b, err := os.ReadFile(unit)
	must(t, err)
	got := string(b)
	if !strings.Contains(got, "Environment=PATH=") || !strings.Contains(got, "/opt/tool/bin") {
		t.Errorf("unit carries no PATH:\n%s", got)
	}
	service := got[strings.Index(got, "[Service]"):strings.Index(got, "[Install]")]
	if !strings.Contains(service, "Environment=PATH=") {
		t.Errorf("PATH landed outside [Service]:\n%s", got)
	}
	if !strings.Contains(got, "--health-addr 127.0.0.1:9999") {
		t.Errorf("upgrade clobbered the installed flags:\n%s", got)
	}
}

func TestUpdateReplacesAStalePathAndIsIdempotent(t *testing.T) {
	c, unit := writeUnit(t, "[Unit]\nDescription=x\n\n[Service]\nType=simple\n"+
		"Environment=PATH=/usr/bin\nExecStart=/x serve\n\n[Install]\nWantedBy=default.target\n")
	changed, err := syncUnitPath(c)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v, want a rewrite", changed, err)
	}
	b, err := os.ReadFile(unit)
	must(t, err)
	if strings.Count(string(b), "Environment=PATH=") != 1 {
		t.Errorf("want exactly one PATH directive:\n%s", b)
	}
	if !strings.Contains(string(b), "/opt/tool/bin") {
		t.Errorf("stale PATH not replaced:\n%s", b)
	}
	if changed, err = syncUnitPath(c); err != nil || changed {
		t.Fatalf("changed=%v err=%v, want a no-op on the second run", changed, err)
	}
}

func TestSyncUnitPathToleratesNoUnit(t *testing.T) {
	c := testConfig("linux")
	c.Home = t.TempDir()
	if changed, err := syncUnitPath(c); err != nil || changed {
		t.Fatalf("changed=%v err=%v, want a silent no-op", changed, err)
	}
	c.GOOS = "darwin"
	if changed, err := syncUnitPath(c); err != nil || changed {
		t.Fatalf("non-systemd: changed=%v err=%v, want a no-op", changed, err)
	}
}

func TestMacPlan(t *testing.T) {
	p, err := BuildPlan(testConfig("darwin"))
	if err != nil {
		t.Fatal(err)
	}
	plist := p.Files[0]
	if !strings.HasSuffix(plist.Path, "/Library/LaunchAgents/com.vskstudio.herrscher.plist") {
		t.Fatalf("plist path = %s", plist.Path)
	}
	for _, want := range []string{
		"<key>Label</key><string>com.vskstudio.herrscher</string>",
		"<key>RunAtLoad</key><true/>",
		"<string>/home/me/.local/bin/herrscher</string>",
		"<string>serve</string>",
		"<string>--env-file</string>",
		"<string>/home/me/.config/herrscher/herrscher.env</string>",
	} {
		if !strings.Contains(plist.Content, want) {
			t.Errorf("plist missing %q\n---\n%s", want, plist.Content)
		}
	}
	if strings.Contains(plist.Content, "/bin/sh") {
		t.Errorf("plist should exec herrscher directly, not via a shell:\n%s", plist.Content)
	}
	assertCmd(t, p, "launchctl load -w")
}

func TestWindowsPlan(t *testing.T) {
	c := testConfig("windows")
	c.BinPath = `C:\Users\me\herrscher.exe`
	c.EnvFile = `C:\Users\me\.config\herrscher\herrscher.env`
	c.Home = `C:\Users\me`
	p, err := BuildPlan(c)
	if err != nil {
		t.Fatal(err)
	}
	launcher := p.Files[0]
	if !strings.HasSuffix(launcher.Path, "herrscher-serve.cmd") {
		t.Fatalf("launcher path = %s", launcher.Path)
	}
	if !strings.Contains(launcher.Content, `C:\Users\me\herrscher.exe serve --env-file C:\Users\me\.config\herrscher\herrscher.env`) {
		t.Errorf("launcher missing herrscher invocation:\n%s", launcher.Content)
	}
	if strings.Contains(launcher.Content, "for /f") {
		t.Errorf("launcher should not parse the env file in batch anymore:\n%s", launcher.Content)
	}
	assertCmd(t, p, "schtasks /create /tn herrscher /tr")
}

func TestStatusToleratesInactive(t *testing.T) {
	for _, os := range []string{"linux", "darwin", "windows"} {
		cmd, err := StatusCommand(testConfig(os))
		if err != nil {
			t.Fatalf("status %s: %v", os, err)
		}
		if !cmd.IgnoreErr {
			t.Errorf("status command for %s should be IgnoreErr", os)
		}
	}
}

func TestUnsupportedOS(t *testing.T) {
	if _, err := BuildPlan(testConfig("plan9")); err == nil {
		t.Fatal("expected error for unsupported OS")
	}
}

func TestQuoters(t *testing.T) {
	if got := systemdQuote("/usr/bin/herrscher"); got != "/usr/bin/herrscher" {
		t.Errorf("systemdQuote plain = %q", got)
	}
	if got := systemdQuote(`/opt/My Apps/herrscher`); got != `"/opt/My Apps/herrscher"` {
		t.Errorf("systemdQuote spaces = %q", got)
	}
	if got := systemdQuote(`a"b\c`); got != `"a\"b\\c"` {
		t.Errorf("systemdQuote escape = %q", got)
	}

	if got := cmdQuote(`C:\herrscher.exe`); got != `C:\herrscher.exe` {
		t.Errorf("cmdQuote plain = %q", got)
	}
	if got := cmdQuote(`C:\Program Files\herrscher.exe`); got != `"C:\Program Files\herrscher.exe"` {
		t.Errorf("cmdQuote spaces = %q", got)
	}

	if got := joinQuoted(systemdQuote, "/b/herrscher", []string{"serve", "x y"}); got != `/b/herrscher serve "x y"` {
		t.Errorf("joinQuoted = %q", got)
	}
}

func TestArgsWithSpacesAreQuoted(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		c := testConfig(goos)
		c.ExtraArgs = []string{"--status-channel", "chan with space"}
		p, err := BuildPlan(c)
		if err != nil {
			t.Fatalf("plan %s: %v", goos, err)
		}
		if strings.Contains(p.Files[0].Content, "--status-channel chan with space") {
			t.Errorf("%s leaves a space-bearing arg unquoted:\n%s", goos, p.Files[0].Content)
		}
	}
}

func TestDefaultCmdBakedIntoUnit(t *testing.T) {
	const cmd = "claude --model claude-opus-4-8 --effort low"
	for _, goos := range []string{"linux", "darwin", "windows"} {
		c := testConfig(goos)
		c.ExtraArgs = []string{"--cmd", cmd}
		p, err := BuildPlan(c)
		if err != nil {
			t.Fatalf("plan %s: %v", goos, err)
		}
		content := p.Files[0].Content
		if !strings.Contains(content, "claude-opus-4-8") {
			t.Errorf("%s: ExecStart missing the baked model:\n%s", goos, content)
		}
		if strings.Contains(content, "--cmd "+cmd) {
			t.Errorf("%s: default cmd not quoted as a single token:\n%s", goos, content)
		}
	}
}

func TestConfigScaffoldInPlan(t *testing.T) {
	const cmd = "claude --model claude-opus-4-8 --effort low"
	for _, goos := range []string{"linux", "darwin", "windows"} {
		c := testConfig(goos)
		c.ConfigPath = "/home/me/.config/herrscher/config.json"
		c.DefaultCmd = cmd
		p, err := BuildPlan(c)
		if err != nil {
			t.Fatalf("plan %s: %v", goos, err)
		}
		assertConfigScaffold(t, goos, configFile(p, c.ConfigPath), cmd)
	}
}

func configFile(p Plan, path string) *FileWrite {
	for i := range p.Files {
		if p.Files[i].Path == path {
			return &p.Files[i]
		}
	}
	return nil
}

func assertConfigScaffold(t *testing.T, goos string, cfg *FileWrite, cmd string) {
	t.Helper()
	if cfg == nil {
		t.Fatalf("%s: no config.json in plan", goos)
	}
	if !cfg.Template {
		t.Errorf("%s: config.json must be Template (never clobber edits)", goos)
	}
	if !strings.Contains(cfg.Content, cmd) {
		t.Errorf("%s: config.json missing pre-filled cmd:\n%s", goos, cfg.Content)
	}
	if strings.Contains(cfg.Content, "GW_TOKEN=") {
		t.Errorf("%s: config.json must not assign secrets", goos)
	}
}

func TestNoConfigScaffoldWhenUnset(t *testing.T) {
	p, err := BuildPlan(testConfig("linux"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range p.Files {
		if strings.HasSuffix(f.Path, "config.json") {
			t.Fatalf("unexpected config.json scaffold: %s", f.Path)
		}
	}
}

func TestInstalledBinPathReadsUnit(t *testing.T) {
	home := t.TempDir()
	c := testConfig("linux")
	c.Home = home
	c.BinPath = "/opt/herrscher/herrscher"
	unitDir := home + "/.config/systemd/user"
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := BuildPlan(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unitDir+"/herrscher.service", []byte(p.Files[0].Content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := InstalledBinPath(c)
	if !ok || got != "/opt/herrscher/herrscher" {
		t.Fatalf("InstalledBinPath = %q, %v; want /opt/herrscher/herrscher, true", got, ok)
	}
	c2 := testConfig("linux")
	c2.Home = t.TempDir()
	if _, ok := InstalledBinPath(c2); ok {
		t.Fatal("expected no bin path when unit is absent")
	}
}

func TestFirstToken(t *testing.T) {
	cases := map[string]string{
		"/usr/bin/herrscher serve --x y": "/usr/bin/herrscher",
		`"/has space/herrscher" serve`:   "/has space/herrscher",
		"/lone/herrscher":                "/lone/herrscher",
		"":                               "",
	}
	for in, want := range cases {
		if got := firstToken(in); got != want {
			t.Errorf("firstToken(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUninstallAndStatus(t *testing.T) {
	for _, os := range []string{"linux", "darwin", "windows"} {
		if _, err := BuildUninstall(testConfig(os)); err != nil {
			t.Errorf("uninstall %s: %v", os, err)
		}
		if _, err := StatusCommand(testConfig(os)); err != nil {
			t.Errorf("status %s: %v", os, err)
		}
	}
}

func assertCmd(t *testing.T, p Plan, want string) {
	t.Helper()
	for _, c := range p.Commands {
		if strings.Contains(strings.Join(c.Argv, " "), want) {
			return
		}
	}
	t.Errorf("no command matching %q in plan", want)
}

func TestSkipStartOmitsImmediateStart(t *testing.T) {
	lc := testConfig("linux")
	lc.SkipStart = true
	lp, err := BuildPlan(lc)
	if err != nil {
		t.Fatal(err)
	}
	var ljoinedBuilder strings.Builder
	for _, c := range lp.Commands {
		ljoinedBuilder.WriteString(strings.Join(c.Argv, " ") + "\n")
	}
	ljoined := ljoinedBuilder.String()
	if strings.Contains(ljoined, "enable --now") {
		t.Errorf("SkipStart linux plan must not start now:\n%s", ljoined)
	}
	if !strings.Contains(ljoined, "enable "+linuxUnitName) {
		t.Errorf("SkipStart linux plan should still enable at boot:\n%s", ljoined)
	}

	mc := testConfig("darwin")
	mc.SkipStart = true
	mp, err := BuildPlan(mc)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range mp.Commands {
		if len(c.Argv) >= 2 && c.Argv[0] == "launchctl" && c.Argv[1] == "load" {
			t.Errorf("SkipStart mac plan must not load (start) the agent: %v", c.Argv)
		}
	}
	mp2, err := BuildPlan(testConfig("darwin"))
	must(t, err)
	assertCmd(t, mp2, "launchctl load -w")
}

func TestEnvFileHasRequired(t *testing.T) {
	vars := testEnvVars()
	dir := t.TempDir()
	missing := filepath.Join(dir, "absent.env")
	if envFileHasRequired(missing, vars) {
		t.Error("absent env file should report not ready")
	}
	tmpl := filepath.Join(dir, "tmpl.env")
	if err := os.WriteFile(tmpl, []byte(renderEnvTemplate(vars)), 0o600); err != nil {
		t.Fatal(err)
	}
	if envFileHasRequired(tmpl, vars) {
		t.Error("empty template should report not ready")
	}
	filled := filepath.Join(dir, "filled.env")
	if err := os.WriteFile(filled, []byte("# c\nGW_TOKEN=  abc.def  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !envFileHasRequired(filled, vars) {
		t.Error("a filled required var should be detected")
	}
	commented := filepath.Join(dir, "commented.env")
	if err := os.WriteFile(commented, []byte("#GW_TOKEN=abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if envFileHasRequired(commented, vars) {
		t.Error("commented required line should not count")
	}
	if !envFileHasRequired(missing, []EnvVar{{Key: "OPTIONAL"}}) {
		t.Error("no required vars should always report ready")
	}
}

func TestInstallSkipsStartWhenNoToken(t *testing.T) {
	dir := t.TempDir()
	c := testConfig("linux")
	c.EnvFile = filepath.Join(dir, "herrscher.env")
	if envFileHasRequired(c.EnvFile, c.EnvVars) {
		t.Fatal("precondition: env file should not be ready")
	}
	c.SkipStart = true
	p, err := BuildPlan(c)
	must(t, err)
	for _, cmd := range p.Commands {
		if strings.Contains(strings.Join(cmd.Argv, " "), "--now") {
			t.Errorf("fresh install must not start the service now: %v", cmd.Argv)
		}
	}
}

func TestEnvTemplateCarriesNoValues(t *testing.T) {
	vars := testEnvVars()
	tmpl := renderEnvTemplate(vars)
	for _, v := range vars {
		if !strings.Contains(tmpl, v.Key+"=\n") && !strings.HasSuffix(tmpl, v.Key+"=") {
			t.Errorf("env template should declare %s with an empty value:\n%s", v.Key, tmpl)
		}
	}
	for _, line := range strings.Split(tmpl, "\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if _, val, _ := strings.Cut(line, "="); strings.TrimSpace(val) != "" {
			t.Errorf("env template line carries a value (would be a committed secret): %q\n%s", line, tmpl)
		}
	}
}

func TestNoSecretEmbeddedInAnyArtifact(t *testing.T) {
	const sentinel = "super-secret-token-value"
	for _, os := range []string{"linux", "darwin", "windows"} {
		c := testConfig(os)
		p, err := BuildPlan(c)
		if err != nil {
			t.Fatalf("plan %s: %v", os, err)
		}
		for _, f := range p.Files {
			if strings.Contains(f.Content, sentinel) {
				t.Errorf("%s artifact %s embeds a secret", os, f.Path)
			}
			if strings.Contains(f.Content, "TOKEN=x") || strings.Contains(f.Content, "Bot ") {
				t.Errorf("%s artifact %s looks like it embeds a token:\n%s", os, f.Path, f.Content)
			}
		}
	}
}

func TestLinuxLingerOmittedWithoutUser(t *testing.T) {
	c := testConfig("linux")
	c.User = ""
	p, err := BuildPlan(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range p.Commands {
		if len(cmd.Argv) > 0 && cmd.Argv[0] == "loginctl" {
			t.Errorf("linger command should be omitted when User is empty, got %v", cmd.Argv)
		}
	}
}

func TestMacRunStringIsXMLEscaped(t *testing.T) {
	c := testConfig("darwin")
	c.ExtraArgs = []string{"--note", "a<b>c&d"}
	p, err := BuildPlan(c)
	if err != nil {
		t.Fatal(err)
	}
	plist := p.Files[0].Content
	if strings.Contains(plist, "a<b>c&d") {
		t.Errorf("raw XML metacharacters leaked into the plist:\n%s", plist)
	}
	if !strings.Contains(plist, "a&lt;b&gt;c&amp;d") {
		t.Errorf("run string not XML-escaped:\n%s", plist)
	}
}

func TestWindowsBinPathQuotedWithSpaces(t *testing.T) {
	c := testConfig("windows")
	c.BinPath = `C:\Program Files\herrscher\herrscher.exe`
	p, err := BuildPlan(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Files[0].Content, `"C:\Program Files\herrscher\herrscher.exe" serve`) {
		t.Errorf("launcher must quote a binary path with spaces:\n%s", p.Files[0].Content)
	}
}

func TestBuildUninstallRemovesArtifacts(t *testing.T) {
	cases := map[string]string{
		"linux":   "herrscher.service",
		"darwin":  "com.vskstudio.herrscher.plist",
		"windows": "herrscher",
	}
	for os, marker := range cases {
		p, err := BuildUninstall(testConfig(os))
		if err != nil {
			t.Fatalf("uninstall %s: %v", os, err)
		}
		var joinedBuilder strings.Builder
		for _, cmd := range p.Commands {
			joinedBuilder.WriteString(strings.Join(cmd.Argv, " ") + "\n")
		}
		joined := joinedBuilder.String()
		if !strings.Contains(joined, marker) {
			t.Errorf("%s uninstall never references %q:\n%s", os, marker, joined)
		}
		if !strings.Contains(joined, "rm ") && !strings.Contains(joined, "/delete") {
			t.Errorf("%s uninstall has no removal step:\n%s", os, joined)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	c, err := DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.BinPath == "" || c.Home == "" {
		t.Fatalf("DefaultConfig left BinPath/Home empty: %+v", c)
	}
	if !strings.HasPrefix(c.EnvFile, c.Home) {
		t.Errorf("env file %q should live under home %q", c.EnvFile, c.Home)
	}
	if !strings.HasSuffix(c.EnvFile, filepath.Join("herrscher", "herrscher.env")) {
		t.Errorf("unexpected env file path %q", c.EnvFile)
	}
	if c.HealthAddr == "" {
		t.Error("DefaultConfig should set a default health address")
	}
}

func TestRunCommandRespectsIgnoreErr(t *testing.T) {
	bad := "definitely-not-a-real-binary-xyz"
	if runtime.GOOS == "windows" {
		t.Skip("argv exec semantics differ on windows")
	}
	if err := runCommand(context.Background(), Command{Argv: []string{bad}, IgnoreErr: true}); err != nil {
		t.Errorf("IgnoreErr command should swallow failure, got %v", err)
	}
	if err := runCommand(context.Background(), Command{Argv: []string{bad}}); err == nil {
		t.Error("strict command should surface a failure")
	}
	if err := runCommand(context.Background(), Command{}); err != nil {
		t.Errorf("empty command should be a no-op, got %v", err)
	}
}

func TestWriteFileAppliesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file modes not meaningful on windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "herrscher.env")
	if err := writeFile(FileWrite{Path: path, Content: renderEnvTemplate(testEnvVars()), Mode: 0o600, Template: true}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("env file mode = %o, want 600", info.Mode().Perm())
	}
}

func TestRestartCommandsPerOS(t *testing.T) {
	for _, tc := range []struct {
		goos string
		want string
	}{
		{"linux", "systemctl --user restart herrscher.service"},
		{"darwin", "launchctl kickstart -k"},
		{"windows", "schtasks /run /tn herrscher"},
	} {
		cmds, err := RestartCommands(testConfig(tc.goos))
		if err != nil {
			t.Fatalf("%s: %v", tc.goos, err)
		}
		joined := strings.Join(cmds[len(cmds)-1].Argv, " ")
		if !strings.Contains(joined, tc.want) {
			t.Fatalf("%s: want %q in %q", tc.goos, tc.want, joined)
		}
	}
	if _, err := RestartCommands(testConfig("plan9")); err == nil {
		t.Fatal("expected error for unsupported OS")
	}
}

func TestValidateSource(t *testing.T) {
	if err := validateSource(""); err == nil {
		t.Fatal("empty source must fail")
	}
	bare := t.TempDir()
	if err := validateSource(bare); err == nil {
		t.Fatal("dir without go.mod must fail")
	}
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateSource(src); err == nil {
		t.Fatal("dir whose go.mod is not the herrscher module must fail")
	}
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module github.com/Herrscherd/herrscher\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateSource(src); err != nil {
		t.Fatalf("valid checkout rejected: %v", err)
	}
}

func TestBuildRejectsNonSource(t *testing.T) {
	if err := Build(context.Background(), t.TempDir(), "/tmp/out"); err == nil {
		t.Fatal("Build must reject a non-checkout before invoking go")
	}
}

func TestSmokeRejectsBrokenBinary(t *testing.T) {
	if err := Smoke(context.Background(), ""); err == nil {
		t.Fatal("Smoke must reject an empty binary path")
	}
	if err := Smoke(context.Background(), "/usr/bin/false"); err == nil {
		t.Fatal("Smoke must reject a binary that exits non-zero")
	}
	if err := Smoke(context.Background(), "/bin/true"); err != nil {
		t.Skipf("smoke baseline unavailable: %v", err)
	}
}
