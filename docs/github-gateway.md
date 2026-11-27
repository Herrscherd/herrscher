# GitHub as a gateway: feasibility study

This study covers part D2 of issue #210: a GitHub gateway on the
`contracts.Gateway` port, where `Post` and `Reply` write issue comments and
`React` adds a reaction. It is a study only. Nothing here is implemented. The
contract is read at herrscher-contracts v0.50.0, the version in `go.mod`, and
the host code at master `c358a168`.

## Verdict

A GitHub gateway is feasible on the current port for output, but it cannot be
enabled yet. Three prerequisites block it, and two of them are host behaviour
that the port does not control:

1. **Automatic attachment (blocking).** A session with no stored gateway list
   and a channel (a legacy session) is bound to every non-terminal gateway
   registered on the host. A registered GitHub gateway would therefore receive
   the final reply of each legacy session, and publish it without condition.
   The GitHub kind must be excluded from this automatic binding before any
   GitHub gateway is registered.
2. **Channel resolution (blocking).** The host passes the session's channel to
   every gateway. A GitHub conversation id has to be the issue, not the session
   channel, so the host must resolve the channel per gateway before the GitHub
   adapter can post at all.
3. **Free-form reactions and chunk size (not blocking today).** `React` takes a
   free emoji string, and the host splits replies at a fixed size that is not a
   property of the gateway. Both are described below.

Recommendation: do not build the write gateway now. Deliver the input side
first (part D1, read only, with an author allow-list), and open a separate
issue for a write gateway once the two blocking prerequisites are settled.

## How a session reaches a gateway

The mechanism, as read in the host code:

- `effectiveKinds` (`core/host/serve.go`, lines 449 to 457) chooses the gateway
  kinds for a session. A session with a stored gateway list uses that list.
- A session without a list but with a channel is legacy: `IsLegacy`
  (`core/internal/state/state.go`, lines 151 to 153) is true when the session
  has no gateway list and a non-empty channel id.
- For a legacy session, `nonTerminalKinds` (`serve.go`, lines 469 to 479)
  returns every registered gateway whose kind is not the terminal kind.
- `gatewayRenderer.handle` (`core/host/renderer.go`, lines 24 to 31) posts each
  final `EventReply` to every gateway of the session, with `Post`, in chunks.
  Nothing asks for an opt-in.

So an explicit configuration does not protect a GitHub gateway: a legacy
session reaches it anyway. Explicit binding is only a second layer, and it is
not in place yet.

## Mapping onto the port

| Port element | GitHub call | Status |
|---|---|---|
| `Manifest` | none | Kind `github`. Declare `Reactions: true`, `Replies: false`, `SelectMenus: false`. |
| `Post(conv, text)` | `POST /repos/{owner}/{repo}/issues/{number}/comments` | Feasible once the channel is resolved per gateway. The returned MessageID is the comment id. |
| `Reply(conv, replyTo, text)` | same as `Post`, quoting the replied-to body | Feasible but lossy: issue comments have no threads. Review threads have their own port (`ReplyToThread`). |
| `React(conv, msg, emoji)` | `POST .../issues/comments/{id}/reactions` | GitHub accepts a fixed set of names. The adapter must refuse any other value. The set is not sourced in this study. |
| `Unreact` (`contracts.ChannelReader`) | `DELETE` of the reaction | Same fixed set. Part of the reader, not of `Gateway`. |
| `Menu(conv, replyTo, prompt, opts)` | a comment listing the choices | Feasible as text. Declare `SelectMenus: false`. The answer must come back as a comment, which needs the input side (D1). |
| `MenuRouter.RouteMenu` | none | Optional, implemented by the reader. Not implemented for GitHub. |
| file sending | none | Issue comments cannot attach files through the REST API. Declare no file sender. |
| input (`contracts.ChannelReader`) | `Read` on issue comments | Part of the reader port, not of `Gateway`. This is the input side of D1. |

The conversation id must name the repository and the issue number, for example
`owner/repo#123`. The port treats `Conversation.ID` as opaque, so the adapter
parses it and refuses anything else with a typed error. This only works once
the host passes the issue as the channel of the conversation (blocking
prerequisite 2).

## How the host calls the gateway

- **Replies.** Every final reply goes through `Post` (see the mechanism above).
  `Gateway.React` is never called by the host: the only `React` the host
  calls is `contracts.TurnReactor`, which strips memory markers from a reply
  (`core/bridge/hub.go`, line 361). So the free-form emoji risk is theoretical
  today.
- **Approvals.** `core/host/turnloop_approval.go` first tries
  `contracts.MenuRouter.RouteMenu` on the reader (line 58). Only when that
  fails, or when the reader is not a router, does it fall back to
  `contracts.Degrade(gateway).Menu`. The approval announcements (deadline,
  risk, subject, lines 82 to 89) go only to the gateway when it implements
  `RoutedEventSink` or `EventSink`, never through `Menu`.
- **Chunks.** `renderer.go` splits the reply with `chunk`. The split counts
  runes, cuts at the last newline in the second half of the window, and
  otherwise cuts at the limit. The limit is the constant `gatewayMaxLen`, which
  is 2000. It is not a property of the gateway.

## Gaps in the contract and the host

- **Automatic attachment.** See the mechanism above. Blocking prerequisite 1.
- **Channel.** The host gives every gateway the session channel, and
  `renderChannel` (`core/host/turnloop_emit.go`, lines 104 to 109) returns it
  before the gateway default channel. Blocking prerequisite 2.
- **Reaction set.** `React` takes `emoji string`. The port does not say which
  values a gateway accepts. The GitHub adapter must refuse unknown values with
  a sentinel error, or the port must define the set.
- **Chunk size.** `gatewayMaxLen` is a host constant. A GitHub comment has its
  own maximum, which this study does not source (see Not verified), so a reply
  may need a different split for GitHub.
- **Quota.** The gateway port has no notion of a request budget. Writes count
  against the same REST budget as the reads of the forge port, so they must
  share the `prwatch.Limit` of that forge. The port does not say so.
- **Input and detection of change.** The reader port (`contracts.ChannelReader`)
  has `Read` but no change marker: a GitHub issue has no date of creation or
  update in `contracts.Issue`, and `Issues` has no revision. So a poller cannot
  tell that an issue changed, without reading it again. The `IssueQuery` filter
  by label exists (`IssueQuery.Labels`). There is no filter by mention, and no
  filter by date. A `herrscher-contracts` change (v0.52.0) is reported in
  progress to add these filters; this study has not verified that change.

## Risks

- **Public disclosure.** On a public repository, anything the agent writes is
  published to the world: source excerpts, file paths, approval prompts, and
  any secret the agent prints. Automatic output must never reach a GitHub
  conversation.
- **Self-trigger.** Comments written by the gateway come back through the input
  path as new comments. The input must ignore the App's own login and every
  `ForgeActor` marked `Bot`.
- **Prompt injection through output.** A reply or an approval prompt quotes
  untrusted text from the issue. Posting it back is harmless to the host, but it
  lets other readers treat the agent's words as an instruction.
- **Secondary rate limits.** GitHub throttles bursts of content creation
  separately from the primary budget. Writes must be serialized per
  conversation, with a delay between them.
- **Token.** Writes need `issues: write` on the App. The token stays in the
  host, as for every forge call.

## Proposed order before a write gateway

1. Exclude the GitHub kind from the automatic binding of legacy sessions
   (`nonTerminalKinds`), so that no session reaches it without an explicit,
   per-repository configuration. This is a prerequisite: explicit configuration
   alone is not enough while legacy binding exists.
2. Resolve the channel per gateway in the host, so that a GitHub conversation
   receives the issue number, not the session channel.
3. Output reaches GitHub only through an explicit host verb, decided by the
   approval policy like any other write. The host does not post automatically
   to a GitHub conversation.
4. The GitHub adapter declares `Reactions: true`, accepts only the names GitHub
   defines, and refuses the others with a sentinel error.
5. The chunk limit becomes a property of the gateway, read by the renderer.
6. Writes share the forge `Limit`, are serialized per conversation, and skip
   comments written by the App itself.

## Not verified

No code was run for this study. Every fact about the host and the contract
comes from reading the code at the versions named above. Two values are not
sourced here and must be checked against the GitHub documentation before any
adapter relies on them: the list of accepted reaction names, and the maximum
length of an issue comment.
