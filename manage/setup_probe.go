package manage

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Herrscherd/herrscher/core/clitext"
)

const (
	probeTimeout     = 20 * time.Second
	discordAPI       = "https://discord.com/api/v10"
	telegramAPI      = "https://api.telegram.org"
	redactedToken    = "[token]"
	anthropicAPI     = "https://api.anthropic.com"
	anthropicVersion = "2023-06-01"
	probeBodyLimit   = 1 << 16
	codexLoggedIn    = "Logged in"
	codexUsing       = "using "
	codexKeySuffix   = " - "
)

var errProbeStatus = clitext.SentinelError("unexpected status from the check")

type probeKind int

const (
	probeDiscord probeKind = iota
	probeTelegram
	probeClaude
	probeCodex
	probeCursor
	probeVault
)

type probeState int

const (
	probeIdle probeState = iota
	probeRunning
	probePassed
	probeFailed
)

type probeResult struct {
	params clitext.Params
	key    clitext.Key
	state  probeState
}

func passed(key clitext.Key, params clitext.Params) probeResult {
	return probeResult{state: probePassed, key: key, params: params}
}

func failed(key clitext.Key, params clitext.Params) probeResult {
	return probeResult{state: probeFailed, key: key, params: params}
}

func probeError(err error) probeResult {
	return failed(clitext.SetupProbeFailed, clitext.Params{clitext.ParamErr: err.Error()})
}

type setupCommand struct {
	dir   string
	stdin string
	name  string
	args  []string
	env   []string
}

const processWaitDelay = 5 * time.Second

type commandRunner func(ctx context.Context, cmd setupCommand) ([]byte, error)

func execRunner(ctx context.Context, cmd setupCommand) ([]byte, error) {
	proc := exec.CommandContext(ctx, cmd.name, cmd.args...)
	proc.Dir = cmd.dir
	proc.WaitDelay = processWaitDelay
	if len(cmd.env) > 0 {
		proc.Env = append(os.Environ(), cmd.env...)
	}
	if cmd.stdin != "" {
		proc.Stdin = strings.NewReader(cmd.stdin)
	}
	out, err := proc.CombinedOutput()
	if errors.Is(err, exec.ErrWaitDelay) && proc.ProcessState != nil && proc.ProcessState.Success() {
		return out, nil
	}
	return out, err
}

type setupProber struct {
	run          commandRunner
	lookPath     func(string) (string, error)
	client       *http.Client
	discordURL   string
	telegramURL  string
	anthropicURL string
}

func liveProber() setupProber {
	return setupProber{
		run:          execRunner,
		lookPath:     exec.LookPath,
		client:       &http.Client{Timeout: probeTimeout},
		discordURL:   discordAPI,
		telegramURL:  telegramAPI,
		anthropicURL: anthropicAPI,
	}
}

type discordUser struct {
	Username string `json:"username"`
}

func (prober setupProber) discord(ctx context.Context, token string) probeResult {
	if strings.TrimSpace(token) == "" {
		return failed(clitext.SetupBotEmpty, discordParams())
	}
	var user discordUser
	status, err := prober.getJSON(ctx, prober.discordURL+"/users/@me", http.Header{"Authorization": {"Bot " + token}}, &user)
	switch {
	case err != nil:
		return probeError(err)
	case status == http.StatusUnauthorized:
		return failed(clitext.SetupBotRefused, discordParams())
	case status != http.StatusOK:
		return probeError(errProbeStatus)
	}
	return passed(clitext.SetupBotOK, clitext.Params{clitext.ParamName: user.Username})
}

type telegramIdentity struct {
	Result struct {
		Username string `json:"username"`
	} `json:"result"`
}

func (prober setupProber) telegram(ctx context.Context, token string) probeResult {
	token = strings.TrimSpace(token)
	if token == "" {
		return failed(clitext.SetupBotEmpty, telegramParams())
	}
	var identity telegramIdentity
	status, err := prober.getJSON(ctx, prober.telegramURL+"/bot"+token+"/getMe", http.Header{}, &identity)
	switch {
	case err != nil:
		return failed(clitext.SetupProbeFailed, clitext.Params{clitext.ParamErr: strings.ReplaceAll(err.Error(), token, redactedToken)})
	case status == http.StatusUnauthorized, status == http.StatusNotFound:
		return failed(clitext.SetupBotRefused, telegramParams())
	case status != http.StatusOK:
		return probeError(errProbeStatus)
	}
	return passed(clitext.SetupBotOK, clitext.Params{clitext.ParamName: "@" + identity.Result.Username})
}

func (prober setupProber) anthropicKey(ctx context.Context, key string) probeResult {
	if strings.TrimSpace(key) == "" {
		return failed(clitext.SetupKeyEmpty, nil)
	}
	header := http.Header{"X-Api-Key": {key}, "Anthropic-Version": {anthropicVersion}}
	status, err := prober.getJSON(ctx, prober.anthropicURL+"/v1/models", header, nil)
	switch {
	case err != nil:
		return probeError(err)
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return failed(clitext.SetupKeyRefused, nil)
	case status != http.StatusOK:
		return probeError(errProbeStatus)
	}
	return passed(clitext.SetupKeyAccepted, nil)
}

func (prober setupProber) getJSON(ctx context.Context, url string, header http.Header, into any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return 0, err
	}
	req.Header = header
	resp, err := prober.client.Do(req)
	if err != nil {
		return 0, err
	}
	var decodeErr error
	if resp.StatusCode == http.StatusOK && into != nil {
		decodeErr = json.NewDecoder(io.LimitReader(resp.Body, probeBodyLimit)).Decode(into)
	}
	if err := errors.Join(decodeErr, resp.Body.Close()); err != nil {
		return 0, err
	}
	return resp.StatusCode, nil
}

type claudeStatus struct {
	AuthMethod       string `json:"authMethod"`
	SubscriptionType string `json:"subscriptionType"`
	LoggedIn         bool   `json:"loggedIn"`
}

func (prober setupProber) claude(ctx context.Context, afterLogin bool) probeResult {
	out, err := prober.run(ctx, setupCommand{name: kindClaude, args: []string{"auth", "status", "--json"}})
	if errors.Is(err, exec.ErrNotFound) {
		return failed(clitext.SetupCLIMissing, clitext.Params{clitext.ParamName: kindClaude})
	}
	var status claudeStatus
	if jsonErr := json.Unmarshal(out, &status); jsonErr != nil {
		if err != nil {
			return probeError(err)
		}
		return probeError(jsonErr)
	}
	if !status.LoggedIn {
		return loggedOut(afterLogin)
	}
	plan := status.SubscriptionType
	if plan == "" {
		plan = status.AuthMethod
	}
	return passed(clitext.SetupLoggedIn, clitext.Params{clitext.ParamName: plan})
}

func (prober setupProber) codex(ctx context.Context, afterLogin bool) probeResult {
	out, err := prober.run(ctx, setupCommand{name: kindCodex, args: []string{verbLogin, "status"}})
	if errors.Is(err, exec.ErrNotFound) {
		return failed(clitext.SetupCLIMissing, clitext.Params{clitext.ParamName: kindCodex})
	}
	status := firstLine(out)
	if err != nil || !strings.HasPrefix(status, codexLoggedIn) {
		return loggedOut(afterLogin)
	}
	account := strings.TrimSpace(strings.TrimPrefix(status, codexLoggedIn))
	account = strings.TrimPrefix(account, codexUsing)
	if cut, _, found := strings.Cut(account, codexKeySuffix); found {
		account = cut
	}
	return passed(clitext.SetupLoggedIn, clitext.Params{clitext.ParamName: account})
}

func (prober setupProber) codexKeyLogin(ctx context.Context, key string) probeResult {
	if strings.TrimSpace(key) == "" {
		return failed(clitext.SetupKeyEmpty, nil)
	}
	_, err := prober.run(ctx, setupCommand{name: kindCodex, stdin: key + "\n", args: []string{verbLogin, "--with-api-key"}})
	if errors.Is(err, exec.ErrNotFound) {
		return failed(clitext.SetupCLIMissing, clitext.Params{clitext.ParamName: kindCodex})
	}
	return prober.codex(ctx, true)
}

func loggedOut(afterLogin bool) probeResult {
	if afterLogin {
		return failed(clitext.SetupLoginUnfinished, nil)
	}
	return failed(clitext.SetupLoggedOut, nil)
}

func firstLine(out []byte) string {
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

const cursorCLI = "cursor-agent"

func (prober setupProber) cursor(key string) probeResult {
	if _, err := prober.lookPath(cursorCLI); err != nil {
		return failed(clitext.SetupCLIMissing, clitext.Params{clitext.ParamName: cursorCLI})
	}
	if strings.TrimSpace(key) == "" {
		return failed(clitext.SetupKeyEmpty, nil)
	}
	return passed(clitext.SetupCursorOK, nil)
}

func vaultCheck(path string) probeResult {
	path = strings.TrimSpace(path)
	if path == "" {
		return failed(clitext.SetupVaultEmpty, nil)
	}
	if !filepath.IsAbs(path) {
		return failed(clitext.SetupVaultBad, clitext.Params{clitext.ParamErr: errVaultRelative.Error()})
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		if parent, parentErr := os.Stat(filepath.Dir(path)); parentErr == nil && parent.IsDir() {
			return passed(clitext.SetupVaultCreate, nil)
		}
		return failed(clitext.SetupVaultBad, clitext.Params{clitext.ParamErr: err.Error()})
	}
	if err != nil {
		return failed(clitext.SetupVaultBad, clitext.Params{clitext.ParamErr: err.Error()})
	}
	if !info.IsDir() {
		return failed(clitext.SetupVaultBad, clitext.Params{clitext.ParamErr: errVaultNotDir.Error()})
	}
	probe, err := os.CreateTemp(path, ".herrscher-setup-*")
	if err != nil {
		return failed(clitext.SetupVaultBad, clitext.Params{clitext.ParamErr: err.Error()})
	}
	name := probe.Name()
	closeErr := probe.Close()
	if err := errors.Join(closeErr, os.Remove(name)); err != nil {
		return failed(clitext.SetupVaultBad, clitext.Params{clitext.ParamErr: err.Error()})
	}
	return passed(clitext.SetupVaultOK, nil)
}

var (
	errVaultNotDir   = clitext.SentinelError("not a directory")
	errVaultRelative = clitext.SentinelError("not an absolute path")
)
