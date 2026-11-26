# Lint exclusions

Every path exclusion in `.golangci.yml` is listed here with the reason it stays. A
lint is excluded only when the fix would make the code worse or when the flagged
pattern is the design, and never by a comment in the source. An exclusion that no
longer has a reason is removed from `.golangci.yml`, and its row here with it.

Rows marked **to confirm** keep the reason of the rule as far as its path and text show
it. The original decision is not written in the repository, so its owner should confirm
it or narrow the rule.

## Tests and tools

| Path | Linters | Text | Reason |
|---|---|---|---|
| `_test.go` | forbidigo, err113, gosec, goconst, mnd, varnamelen | | Tests use literal fixtures, dynamic errors and short names on purpose; these checks guard production code. |
| `_test.go` | gocritic | `tooManyResultsChecker` | Table helpers return several results to assert on them together. |
| `^(cmd\|tools)/` | gosec | G304, G703 | Build and developer tools read and write the paths their operator passes on the command line. |
| `tools/membench/runner.go` | gosec | G204 | The benchmark runner starts the commands its operator names in order to measure them. |
| `bridge_provision_test.go` | unparam | `result 0 (error) is always nil` | The provisioning test fixture keeps the signature of the function it stands in for. **To confirm.** |

## Agent, store and forge

| Path | Linters | Text | Reason |
|---|---|---|---|
| `core/internal/agent/(agent\|store).go` | gosec | G304, G204, G703 | The agent package reads and writes files under the agent home, and starts the agent backend commands from the operator's configuration. |
| `core/internal/agent/agent.go` | gosec | G101 | A configuration key name that contains a credential word, not a credential. **To confirm.** |
| `core/(identity\|scope\|service\|keyproxy\|internal/(forge\|runner\|supervisor\|worktree\|checkpoint))/` | gosec | G204 | These packages start the configured forge, runner and git binaries with arguments built by the code, never through a shell. **To confirm** the list of packages. |
| `core/forgecreds/gh.go` | gosec | G204 | Starts the GitHub CLI with fixed sub-commands. **To confirm.** |
| `core/internal/supervisor/supervisor.go` | containedctx | context field | The supervisor keeps the context of the processes it owns for their lifetime. **To confirm.** |
| `core/internal/manager/session.go` | misspell | `marrage` | The term matches data already written by the sessions API. **To confirm.** |

## MCP tool weight (`core/internal/mcpweight`)

| Path | Linters | Text | Reason |
|---|---|---|---|
| `core/internal/mcpweight/rpc.go` | gosec | G204 | Starts each MCP server the agent's `mcp.json` declares, with the command and arguments it declares, after `{{WORKTREE}}` substitution. No shell is involved. The operator trusts that file, as `--deep` documents. |
| `core/internal/mcpweight/config.go` | gosec | G304 | Reads the agent's `mcp.json` at the path the agent home defines. |
| `core/internal/mcpweight/fake_test.go` | revive | deep-exit | The test server fake exits its own process to end the child it runs as. It is compiled into test binaries only. |

The `G101` exclusion that matched the `MCPWeight` identifiers was removed: the identifiers
are now `ToolWeight`, and the rule no longer matches them.

## Host, service and runtime

| Path | Linters | Text | Reason |
|---|---|---|---|
| `core/host/session_shell.go` | gosec | G204 | The shell session starts the user's shell on purpose. **To confirm.** |
| `core/host/(hosts_port\|serve).go` | gosec | G204 | Starts the processes of the host's own serving commands. **To confirm.** |
| `core/host/(approve_cmd\|lock\|remote_materialize).go` | gosec | G304 | Reads and writes host state files at paths the host defines. **To confirm.** |
| `core/host/backendenv.go` | gosec | G101 | Names of environment variables that carry credentials, not values. **To confirm.** |
| `core/host/backendenv.go` | gochecknoglobals | `capturedGateway` | A test seam set once at startup. **To confirm.** |
| `core/host/announcekey.go` | gochecknoglobals | `capturedAnnounce` | A test seam set once at startup. **To confirm.** |
| `core/host/(coordinator\|coordinator_test).go` | misspell | `conflit`, `existant`, `partiel` | Words of the French locale the coordinator matches. **To confirm.** |
| `core/clitext/keys_setup.go`, `manage/setup_catalog.go` | gosec | G101 | Catalog keys and names that contain a credential word, not credentials. **To confirm.** |
| `core/service/service.go` | gosec | G703 | The service installer writes its unit files at the paths the platform defines. **To confirm.** |
| `core/envx/hide.go` | gochecknoglobals | `hidden` | A process-wide list of variables hidden from the output. **To confirm.** |
| `core/internal/redact/redact.go` | gochecknoglobals | `rules` | The redaction rules table, built once. **To confirm.** |
| `core/bridge/log.go` | gochecknoglobals | `logger` | The bridge logger, set once at startup. **To confirm.** |
| `core/bridge/attachments.go` | gochecknoglobals | `pinnedTransports` | The transports pinned at startup. **To confirm.** |
| `envfile.go` | gosec | G703 | Writes the environment file at the path the caller passes. **To confirm.** |
| `manage/(manage\|seam\|version\|setup_probe).go` | gosec | G204 | Starts the install and version probes of the managed binaries. **To confirm.** |
| `manage/(apply\|init\|lifecycle\|manage\|setup).go` | gosec | G304 | Reads the managed configuration files. **To confirm.** |
| `manage/(apply\|init\|manage\|seam).go` | gosec | G306, G703 | Writes managed files with the modes the installer sets. **To confirm.** |
| `manage/version.go` | nilerr | `returns nil` | A missing version is reported as unknown, not as an error. **To confirm.** |
| `manage/setup_(model\|apply).go` | containedctx | context field | The setup wizard keeps the context of its run. **To confirm.** |

## Terminal plugin (`plugins/terminal`)

| Path | Linters | Text | Reason |
|---|---|---|---|
| `plugins/terminal/register.go` | gochecknoinits | | The plugin registers itself at load, as the plugin system requires. |
| `plugins/terminal/terminal.go`, `plugins/terminal/tui/tui.go` | containedctx | context field | The terminal and its UI keep the context of their session. **To confirm.** |
| `plugins/terminal/tui/image_decode.go` | revive | blank-imports | The image decoders register themselves by import. |
| `plugins/terminal/tui/(clipboard\|open\|editor\|sidebar_git\|statusline_script\|diff_git).go` | gosec | G204 | Start the clipboard, editor, browser and git helpers of the user's environment. **To confirm.** |
| `plugins/terminal/tui/keylog.go` | gosec | G703 | The key log writes to the path the user configures. **To confirm.** |
| `plugins/terminal/tui/` | misspell | `commandes`, `marrage` | Words of the French and legacy strings the UI matches. **To confirm.** |
| `plugins/terminal/tui/` | exhaustive | missing `tea.Key` cases | The UI handles the keys it uses and passes the others on. **To confirm.** |
| `plugins/terminal/tui/image_sixel.go`, `palette.go`, `open.go` | nonamedreturns | named return values | Named results document the values of these helpers. **To confirm.** |
