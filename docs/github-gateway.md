# GitHub as a gateway: feasibility study

This study covers part D2 of issue #210: a GitHub gateway on the
`contracts.Gateway` port, where `Post` and `Reply` write issue comments and
`React` adds a reaction. It is a study only. Nothing here is implemented, and
the contract is read at herrscher-contracts v0.50.0, the version in `go.mod`.

## Verdict

A GitHub gateway is feasible on the current port for output: `Post`, `Reply`
and `Menu` map onto issue comments, and `React` maps onto a reaction for a
fixed set of names. Three things are missing or dangerous and must be settled
before any write path is built:

1. the host publishes every final agent reply through `Post`, with no opt-in,
   so a gateway attached to a public issue would publish the whole session
   on that issue without any human verb;
2. the port has no typed set of reactions, so `React` takes a free emoji
   string that GitHub will refuse for most values;
3. the host splits every reply into chunks of 2000 characters, a constant that
   is not per gateway, while a GitHub comment holds 65536.

Recommendation: do not build the write gateway now. Deliver the input side
first (part D1, read only, with an author allow-list). Open a separate issue
for a write gateway only once the opt-in described below exists.

## Mapping onto the port

| Port method | GitHub call | Status |
|---|---|---|
| `Manifest` | none | Kind `github`. Declare `Reactions: true`, `Replies: false`, `SelectMenus: false`. |
| `Post(conv, text)` | `POST /repos/{o}/{r}/issues/{n}/comments` | Feasible. The returned MessageID is the comment id. |
| `Reply(conv, replyTo, text)` | same as `Post`, quoting the replied-to body | Feasible but lossy: issue comments have no threads. Review threads already have their own port (`ReplyToThread`). |
| `React(conv, msg, emoji)` | `POST .../issues/comments/{id}/reactions` | Only `+1`, `-1`, `laugh`, `confused`, `heart`, `hooray`, `rocket`, `eyes` are accepted. Any other value must be refused by the adapter. |
| `Menu(conv, replyTo, prompt, opts)` | a comment listing the choices | Feasible as text only. Declare `SelectMenus: false` so the host degrades it. The answer has to come back as a comment, which needs the input side (D1). |
| file sending | none | Issue comments cannot attach files through the REST API. Declare no file sender. |

The conversation id has to encode the repository and the issue number, for
example `owner/repo#123`. The port treats `Conversation.ID` as opaque, so the
adapter parses it and refuses anything else with a typed error.

## Gaps in the contract and the host

- **Automatic publication.** `core/host/renderer.go` posts every final
  `EventReply` through `Post`, and `core/host/turnloop_approval.go` posts
  approval menus through `Menu`. Nothing asks for an opt-in. For a GitHub
  conversation this is a public comment per chunk, written without a human
  verb. This is the largest gap, and it contradicts the rule of #210 that
  nothing is ever posted automatically on GitHub.
- **Reaction set.** `React` takes `emoji string`. The port does not say which
  values a gateway accepts, so every gateway must guess. GitHub accepts eight
  names. Either the port defines the set, or the GitHub adapter refuses
  unknown values with a sentinel error.
- **Chunk size.** `gatewayMaxLen` is a host constant of 2000 characters. A
  GitHub comment accepts 65536, so a long reply becomes many comments. The
  limit should come from the gateway, not from the host.
- **Quota.** The gateway port has no notion of a request budget. Writes count
  against the same REST budget as the reads of the forge port, so they must
  share the `prwatch.Limit` of that forge, and the port does not say so.
- **Input.** The port has no inbound method. `contracts.Inbound` is a struct
  that a plugin pushes into the manager, so the input of a GitHub conversation
  is outside the port. This is why D1 is a separate piece of work.

## Risks

- **Public disclosure.** On a public repository, anything the agent writes is
  published to the world: source excerpts, file paths, approval prompts, and
  any secret the agent prints. The gateway must refuse to be the target of
  automatic output at all.
- **Self-trigger.** Comments written by the gateway come back through the
  input path of D1 as new comments. The input must ignore the App's own login
  and every `ForgeActor` marked `Bot`.
- **Prompt injection through output.** An approval prompt or reply quotes
  untrusted text from the issue. Posting it back is harmless to the host, but
  it lets other readers treat the agent's words as an instruction.
- **Secondary rate limits.** GitHub throttles bursts of content creation
  separately from the primary budget. Writes must be serialized per
  conversation, with a delay between them.
- **Token.** Writes need `issues: write` on the App. The token stays in the
  host, as for every forge call.

## Proposed order if the write gateway is wanted later

1. A GitHub gateway is attached to a session only by explicit configuration,
   per repository and off by default, in the same format as the input
   configuration of D1.
2. The host does not post automatically to a GitHub conversation. Output
   reaches GitHub only through an explicit host verb, decided by the approval
   policy like any other write.
3. The GitHub adapter declares `Reactions: true`, accepts only the eight names,
   and refuses the others with a sentinel error.
4. The chunk limit becomes a property of the gateway, read by the renderer.
5. Writes share the forge `Limit`, are serialized per conversation, and skip
   comments written by the App itself.

## Not verified

No code was run for this study. Every fact comes from reading herrscher-contracts
v0.50.0 and the host code at master `c358a168`. The GitHub REST reaction list
and comment size are taken from the public GitHub REST documentation, not
tested against the live API.
