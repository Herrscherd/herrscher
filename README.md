# Herrscher

**A self-hosted harness for running fleets of AI coding agents.**

A session is a conversation, an agent, and an isolated git worktree. It is supervised, so it survives a restart. It has a memory, so it knows what happened last week. It can hand work to another session, on another model vendor, without you in the middle. You talk to it from Discord, from the terminal UI that ships in the binary, or from a gateway you write yourself.

## Install and run

```bash
curl -fsSL https://raw.githubusercontent.com/Herrscherd/herrscher/master/scripts/install.sh | sh
```

The script downloads the latest release for Linux or macOS (amd64 or arm64),
checks it against `checksums.txt` and installs `herrscher` in `~/.local/bin`
(`HERRSCHER_INSTALL_DIR` changes the directory, `HERRSCHER_VERSION` pins a tag).
The checksum comes from the same release as the archive, so it catches a
corrupted download, not a release replaced by someone with write access to the
repository.
Windows archives, and every other platform, are on the
[releases page](https://github.com/Herrscherd/herrscher/releases). With Go 1.27:

```bash
go install github.com/Herrscherd/herrscher@latest
```

On Arch Linux, `makepkg -si` next to the `PKGBUILD` builds the `herrscher`
package from the `master` branch on GitHub. It replaces the older `herrscherd`
package, whose binary was `/usr/bin/herrscherd`: a service installed from it still
starts that path, so run `herrscher service install` again after the upgrade.

A prebuilt binary carries the default stack. Changing the compiled modules
(`herrscher plugin`, `herrscher update`, the modules screen of `setup`) rebuilds
from source and needs Go 1.27 and a checkout:

```bash
git clone https://github.com/Herrscherd/herrscher.git
cd herrscher
go build -o herrscher .    # the only binary; plugins are compiled in
```

## First turn

Then:

```bash
herrscher setup            # pick modules, sign in, configure everything
herrscher                  # serves, with the terminal UI in the foreground
```

`setup` opens an interactive screen: you check the modules you want (gateways, backends, memory, skills), then each one is configured in turn. The terminal path needs no Discord token; only the Discord gateway asks for one.

A first task needs no daemon to start: `herrscher "read the thread and propose a split"` opens a session on that task and shows its turn in the terminal UI.

## The five commands to know

- `herrscher setup`: choose the modules and sign-ins, and write the configuration.
- `herrscher "<task>"`: open a session on a task and watch its turn.
- `herrscher session create|list|close|set-budget`: create, list, close and budget sessions.
- `herrscher serve --health-addr :8787`: run the daemon in the foreground.
- `herrscher service install|status|restart|update`: keep the daemon running as a boot-started service.

## Turn telemetry (OpenTelemetry)

The daemon can send each turn of a session to an OpenTelemetry collector. It is off by default: with no OTLP endpoint configured, nothing is initialised, no connection is opened and the turn path pays no cost.

Enable it by pointing the standard variables at a collector, for example:

```sh
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 herrscher serve
```

The configuration file has one telemetry key, `telemetry.enabled`, and it takes priority over the environment:

```json
{
  "telemetry": {
    "enabled": false
  }
}
```

`false` turns the export off even when an endpoint is set: no exporter is built, nothing connects, and an invalid endpoint is not reported. A missing key or `true` changes nothing, so the export still needs an endpoint. Use the key when the daemon runs as a service, since a service unit usually carries only `PATH` and cannot switch the export off through the environment.

Otherwise the export reads the standard OpenTelemetry variables:

- `OTEL_EXPORTER_OTLP_ENDPOINT` or `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` enables the export (the traces variable wins when both are set). The endpoint rules are in the Endpoint paragraph below.
- `OTEL_EXPORTER_OTLP_PROTOCOL` or `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL` selects `http/protobuf` (default) or `grpc`.
- `OTEL_EXPORTER_OTLP_INSECURE` (or `OTEL_EXPORTER_OTLP_TRACES_INSECURE`, which wins) makes a bare `host:port` `grpc` endpoint plaintext.
- `OTEL_SERVICE_NAME` overrides the service name, which defaults to `herrscher`.
- `OTEL_SDK_DISABLED=true` turns the export off even when an endpoint is set.
- Headers, timeouts and batch sizes use the other standard `OTEL_EXPORTER_OTLP_*` and `OTEL_BSP_*` variables.

The export follows the GenAI semantic conventions pinned to **semconv v1.41.0** (`go.opentelemetry.io/otel/semconv/v1.41.0`). Each settled turn becomes one `invoke_agent <session>` span, and each tool call of that turn becomes an `execute_tool <tool>` child span.

Exported: the operation name, the provider (`anthropic`, `openai` or `x_ai` for the built-in backends, the vendor name otherwise), the session as `gen_ai.agent.name`, the request model as it was when the turn started, the input and output token counts, the cache read and cache creation counts, the tool name and call id, and the start and end timestamps of the turn and of each call. Every free-text field is cut to the timeline bound (`timeline.MaxFieldBytes`, 4096 bytes).

Input tokens: `gen_ai.usage.input_tokens` is the sum of the uncached input, the cache reads and the cache creations the backend reported, so it counts every input token. The two cache attributes give the cached part of that sum.

Outcomes and error status:

- A completed turn has no error status.
- A failed turn (the backend returned an error) has error status with `error.type` set to `turn_failed`. The error text is never exported, so the status carries the fixed type only.
- An interrupted turn (the operator cut it while the backend was answering) has unset status, because the operator chose to stop it, and the result attribute `herrscher.turn.outcome` set to `interrupted`. `herrscher.turn.outcome` is a daemon attribute: semconv v1.41.0 defines no value for an interrupted turn.
- An abandoned turn has error status with `error.type` set to `turn_abandoned`. A turn is abandoned when its send to the bridge fails, when the language announcement fails, or when the turn is cut while it waits for the backend. A pick turn that fails in the same places is abandoned too, with the same type.
- A failed tool call has error status with `error.type` set to `tool_failed`. A tool call still open when its turn ends has `error.type` set to `tool_unfinished`. Both are set on the `execute_tool` span, and the turn keeps its own status.
- The error message of a failed call is never exported: the error status carries the type only.

Not exported: prompts, replies, tool details, tool arguments and error messages, in any case. The semantic conventions define opt-in content capture, which this export does not implement.

Endpoint: the endpoint is read from the same environment the daemon reads, and it is passed to the OTLP client explicitly, so the client never reads a different value from the process environment.

- `http/protobuf`: the endpoint is an `http` or `https` URL with a host, such as `http://[::1]:4318`. The scheme sets the transport: `https` is TLS, `http` is plaintext. A base endpoint (`OTEL_EXPORTER_OTLP_ENDPOINT`) gets the `/v1/traces` path appended; `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` is used exactly as written.
- `grpc`: the endpoint is either a URL with an `http` or `https` scheme, which the scheme makes plaintext or TLS, or a bare `host:port`, with IPv6 literals in brackets. A bare `host:port` is TLS unless `OTEL_EXPORTER_OTLP_INSECURE` (or `OTEL_EXPORTER_OTLP_TRACES_INSECURE`) is `true`, which makes it plaintext.

An invalid value is refused at start-up: the daemon logs a localized warning and runs without telemetry.

Known limits:

- An input dropped by an interrupt before the driver hands it to the backend, or refused by the budget gate, produces no span, because no agent invocation happened.
- Token counts that are zero, negative or not finite are omitted, not exported as zero.

## Documentation

Start with the [documentation index](docs/README.md). The main guides are:

- [Install and run](docs/install.md): building, the first session, starting work, sockets.
- [Sessions](docs/sessions.md): what a session gives you, from memory to delegation.
- [Remote sessions](docs/remote.md): sessions on another machine, and their trust model.
- [Approvals](docs/approvals.md): tool calls under policy, and who may change the policy.
- [Observability](docs/observability.md): following a session, health, usage, model routing.
- [Terminal UI](docs/terminal-ui.md): the window, its keys, and the keys not bound yet.
- [Plugins](docs/plugins.md): the compiled-in plugins, managing them, languages.
- [Architecture](docs/architecture.md) and [Testing](docs/testing.md).
- Reference: [service](docs/service.md), [skills](docs/skills.md), [budgets](docs/budgets.md), [environment variables](docs/env.md).
