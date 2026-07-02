# Review — Memory C (domain routing)

**Date:** 2026-07-02
**Scope:** the chantier-C diff across `herrscher-contracts` (branch `feat/kind-domain`)
and `herrscher-obsidian-memory` (branch `feat/domain-routing`) — ~130 lines.
**Method:** direct multi-axis review (CI, architecture, performance, code
quality, security, bug review, useless comments, doc drift), false-positives
filtered. The repos are Go, not the TS project the continuous-improvement skill
targets, so the skill's method was kept and its TS mechanics dropped.

## Gate

- `go vet ./...` clean, `go test ./...` green in both repos (contracts 30, obsidian 31).
- No CI workflow exists in either repo — nothing to satisfy beyond the local gate.

## Applied (safe fixes — doc only, committed)

- `herrscher-contracts` README: `KindDomain` added to the node-kind spine
  description. (commit `148f1d8`)
- `herrscher-obsidian-memory` README: `Domain` documented (+ `Agent`, which was
  already missing) and `InitSpec.Domain` behaviour noted. (commit `5c0dc6f`)

## Findings by axis

| Axe | Résultat |
|-----|----------|
| CI | No workflow present. Proposal: add a minimal Go CI (build+vet+test) — see below. |
| Architecture | Sound. `KindDomain` additive; `in-domain` link (navigation) + `Meta["domain"]` tag (filtering) is deliberate dual representation, not redundancy. `domaines/` namespace consistent with `projets/`. |
| Performance | No regression. Added branches are O(1); `matchesQuery` gains one map insert. `Search` remains an O(files) full walk — pre-existing, tracked as chantier A. |
| Code quality | `projNode` promoted to a variable to carry conditional `Meta` — clean and necessary. |
| Security | No new surface. `Domain` reaches a path (validated by `validKey`, rejects `..`/`/`/empty segments at write) and frontmatter (sanitised by `frontValue`, strips `\r`/`\n`). Same guarantees as `Org`/`Project`. |
| Useless comments | None to remove. The 3 added comments explain intent (the "transverse" concept, relation semantics) and match house density. |
| Bug review | One design finding (below). |
| Doc drift | Fixed (see Applied). |

## Proposals (not applied — need decision)

### P1 — `domaines/<slug>` lists only its first project (design)

`Init` creates the domain node via `ensure` (writes only if absent). A second
project sharing the domain skips the existing node, so its `contains` back-link
is never added — `Recall(domain)` reaches only the first project. The project
still links up (`in-domain`), so navigation up works; only domain→projects is
incomplete.

**Not a regression:** `KindOrganization` has the identical limitation in the
pre-existing code (an org lists only its first project). The C code inherits the
pattern.

**Fix:** replace the `ensure`-carried `contains` link with
`m.Links(ctx, domainKey, projKey, "contains")` after ensuring the domain node —
`Links` is idempotent and appends even when the node exists. Apply the same to
`Org` for consistency. Severity: low (single-project domains, the common case,
work today). Adds ~4 lines + a multi-project test.

### P2 — no CI (feature)

Neither repo has a GitHub Actions workflow. A minimal
`build + go vet + go test` on push/PR would make "respect des CI" a real gate.
Severity: low, but cheap and high-value for a multi-repo platform.

## Nothing found on

Injection, resource leaks (flock/`os.Root` unchanged), concurrency (same mutex),
error handling (errors propagate as before), API breakage (all additive).
