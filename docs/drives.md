# Drives

A session sees its worktree and its memory. A drive adds a folder of Google
Drive, Dropbox, OneDrive, an S3 bucket or Cloudflare R2, with an access set for
each agent: none, read or write. The agent reaches it through its
[self tools](self-tools.md), and every write is put to you first.

## What it needs

herrscher drives [rclone](https://rclone.org). Install it once
(`sudo pacman -S rclone`, `brew install rclone`, or the script of rclone.org)
and check it with `herrscher health`, whose `drives` line gives the rclone
version and the number of drives. `HERRSCHER_RCLONE` names another command,
split on spaces.

herrscher keeps its own rclone configuration, `drives/rclone.conf` beside
`state.json`, readable by you alone, and never touches the one in
`~/.config/rclone`. Every rclone it starts gets `--config` on that file and an
environment with every `RCLONE_` variable removed, so your own rclone setup
neither leaks in nor gets written.

Only rclone writes that file: `rclone config create` when you sign in,
`rclone config delete` when you remove a drive, and rclone itself when it
refreshes a token. herrscher never edits it. Every rclone run that may refresh a
token holds a shared lock, `drives/rclone.conf.lock`, and a sign in or a
removal holds it alone, so neither runs while a refresh may rewrite the file.
A sign in or a removal that waits closes a gate, `drives/rclone.conf.gate`,
that every new run passes first, so a steady flow of reads cannot keep it
waiting. A sign in in progress makes the OAuth drive verbs wait until it ends.
An S3 or R2 drive never touches the file, so it takes no lock and never
waits.

The OAuth tokens live in `drives/rclone.conf`. An S3 or R2 drive never enters
it: its settings are kept with the drive in `drives/drives.json`, its secret in
`drives/secrets.json` (0600), and both reach rclone as `RCLONE_CONFIG_<NAME>_*`
variables in the environment of each run, which only your account can read.
The secret is written before the drive and forgotten after it, so a failure in
between leaves at most a secret no drive names, which is ignored and replaced by
the next add of that name, never a drive without its secret. No
token or key is ever in a worktree, in the arguments of a process, in an
approval question or on a gateway.

## Connecting a drive

`herrscher drive add` runs in your terminal, not in the daemon, so the sign in
and the secret stay between you and rclone.

```bash
herrscher drive add gdrive --kind gdrive --folders Neublox,Karamon --agents read
herrscher drive add box --kind dropbox
herrscher drive add work --kind onedrive --folders Shared/Reports
herrscher drive add assets --kind r2 --account <cloudflare account id> --access-key <key id> --folders assets-bucket/game
herrscher drive add backups --kind s3 --access-key <key id> --region eu-west-3 --folders my-bucket/herrscher
```

- `--kind` is `gdrive`, `dropbox`, `onedrive`, `s3` or `r2`.
- `gdrive`, `dropbox` and `onedrive` sign in through the browser: herrscher
  runs `rclone config create` attached to your terminal, rclone opens the sign
  in page and writes the token into herrscher's configuration. Google Drive
  gets the full `drive` scope, and OneDrive asks which drive to use.
- `s3` and `r2` use an access key. `--access-key` is the key id. The secret is
  typed in the terminal without echo, or read from the variable that
  `--secret-env` names; it is never a flag value, so it stays out of the shell
  history and the process list. `r2` needs `--account`, the Cloudflare account
  id, and derives its endpoint. `s3` takes `--region`, `--endpoint` for a
  store that is not AWS, and `--provider` (`AWS` by default, any provider
  rclone knows otherwise). An empty value, or one with a space or a control
  character, is refused.
- `--folders` lists the folders the agents see, comma separated, relative to
  the root of the remote (for `s3` and `r2`, the bucket comes first). Without
  it, the whole remote is exposed.
- `--agents` is the access of every agent the drive does not name, and of a
  session that runs no agent: `none` by default, `read` or `write`.

A drive name is 1 to 32 lowercase letters, digits, `-` or `_`. It is also the
name of the rclone remote.

## Who sees what

The access is a matter of sessions. You, in your terminal, and a human of the
gateways, within what their [role](approvals.md#a-daemon-that-knows-who-is-calling)
lets them run, act on every drive.

`drive grant <drive> --agent <name> --access none|read|write` sets the access
of one agent, and `--agent '*'` the default. Only you grant it: a session is
never let run `drive grant`, not even with your approval, not even as a
steward, since it would grant itself rights. `drive list` shows each drive,
its kind, its folders and the access of each agent, never the settings of an
S3 or R2 drive. A session sees only the drives its agent can at least read,
with its own access and nothing about the others.

Every path is checked before rclone runs, and for a session before you are
asked anything. It is relative, slash separated, UTF-8, with no empty, `.` or
`..` segment, no backslash, no control character, and no invisible or format
character (bidirectional overrides, zero width characters, spaces other than
the plain one, variation selectors, private use characters), so two paths that
look the same are the same path. It must sit inside one of the exposed folders: `Neublox/a.md`
is inside `Neublox`, `NeubloxOther/a.md` is not. herrscher builds the rclone
location itself, `<drive>:<path>`, and passes it after `--`.

## The verbs

| Verb | What it does | A session |
|---|---|---|
| `drive list` | the drives and their access | runs it |
| `drive ls <drive> [path]` | one level of a folder, 1000 entries at most; the root of a drive with folders lists those folders | needs read |
| `drive read <drive> <path>` | the file as text, 1 MiB at most, refused when it is not UTF-8 or is a folder | needs read |
| `drive read <drive> <path> --to <file>` | copies the file, 64 MiB at most, into the session's worktree | needs read |
| `drive write <drive> <path> --from <file>` | sends a file of the session's worktree, 64 MiB at most | needs write, and asks you |
| `drive log [--drive d] [--agent a] [--session s] [--limit n]` | the journal, 50 entries by default | sees its own entries |
| `drive grant <drive> --agent <name> --access <level>` | sets an access | never |
| `drive remove <drive>` | disconnects the drive and deletes its token or key | asks you |

The self tools give `drive list`, `drive ls`, `drive read` and `drive log` a
tool each (`drive_list`, `drive_ls`, `drive_read`, `drive_log`). `drive write`
goes through `herrscher_run`.

The drive and the path are named once, either as words
(`drive write gdrive Neublox/a.md --from out.md`) or as `--drive` and `--path`.
Both forms together, or a word more, are refused before anything else.

A session's `--to` and `--from` are paths inside its worktree, and herrscher
opens them through an `os.Root` of the worktree:

- `..`, an absolute path and a path through a symbolic link are refused;
- a control, invisible or format character is refused in `--to` and `--from`
  as in a drive path, for you too, so the question never shows a disguised
  file name; a segment holding `~` and a digit, the shape of a Windows short
  name such as `GIT~1`, is refused for a session;
- a path under `.git`, at any depth, or under a file herrscher materializes for
  the agent (`.claude`, `.codex`, `.cursor`, `.grok`, `.herrscher`, `.mcp.json`,
  `AGENTS.md`) is refused, letter case aside, so a read cannot replace the
  approval hook or repoint the repository;
- `--from` must be a regular file, opened without following a link and without
  waiting, so a FIFO or a device is refused at once;
- `--to` streams into a staging file beside the target, readable by you alone
  (0600, folders 0700), renamed over the target once complete: no copy is held
  in the daemon's memory, and a failed copy leaves nothing behind. A staging
  file a stopped daemon left, `.herrscher-drive-*`, is removed by the next
  copy into that folder once it is an hour old.

From your terminal, `herrscher drive` turns a relative `--to` or `--from` into
an absolute path before it reaches the daemon. Any other caller, a human of the
gateways or a caller with no identity, cannot use `--to` or `--from` at all, so
no chat message writes a file on the daemon's machine.

## Each write is asked

A session that holds `write` on a drive still asks for each write. The question
reaches its gateways like any other verb a session may not run alone, with the
four answers of [Approvals](approvals.md): `allow`, `allow --session`,
`allow --always` and `deny`. It reads
`Herrscher(drive write gdrive Neublox/report.md --from out.md)`, whatever form
the agent used: herrscher asks about the drive, the path and the source it is
about to use, in that order, never about the arguments as typed.

- `allow --always` writes the rule for that exact call: the same file, from the
  same source, passes from then on. It approves the pair, not the content, so
  a later write of other bytes from the same source passes too.
- A wider rule is written by hand on that form, such as
  `herrscher approve rule --session s1 'allow Herrscher(drive write gdrive Neublox/*)'`,
  which covers every file under `Neublox` of `gdrive` and nothing else.
- The access, the path and the source are checked before the question. A
  session whose agent holds less than `write`, a path outside the folders, or a
  protected source is refused at once and you are not asked.
- A `steward` session is asked too: `drive write` and `drive remove` are among
  the verbs a steward still puts to you, and `drive grant` is never run for it. A `bypass`
  session asks as well, since the approval mode of a session does not decide its
  self tool calls.

## The journal

Every listing of the remote, read and write, done, refused or failed, is a
line of the journal: the time, the session and its agent, the drive, the path,
the operation, the bytes and the outcome, with the reason of a refusal or a
failure. Your own operations are journaled with no session. The root of a
drive with folders lists those folders without reading the remote, and is not
journaled.

Nothing runs unrecorded. An operation is journaled before its result is given:
a read or a listing whose entry cannot be written returns an error instead of
what it found, and a copy removes what it staged. A write has already reached
the drive by then, so it returns an error saying that its entry is missing.

The journal lives in `drives/journal/`, with files of its own for each session
and for you, so a session can only ever rotate its own history away. Writes
that reached the drive, done or failed, go to a file of their own
(`<session>.writes.jsonl`), and nothing else fills it. Listings, reads,
refusals and the writes that never left the machine (a missing or oversized
source) go to `<session>.jsonl`. Each file is rotated at 4 MiB and keeps eight
previous files (`.1` to `.8`). `drive log` reads them newest first and stops
once it holds the entries it shows; a filter on a session reads that session's
files alone. A line it cannot read is counted and said under the listing.

Two caps keep a session from flooding it:

- the entries that carry no data, refusals and writes that never left the
  machine, are recorded at most 60 per minute and per session, and the next one
  recorded says how many were left out before it;
- a session runs at most 600 listings, reads and writes per minute; the next
  one is refused before it runs, with an error that says so, and that refusal
  is journaled. You are never capped.

Every listing and read that runs is recorded with its path, whatever the pace.

## Limits

- A session on another host lists and reads inline, but cannot use `--to` or
  `--from`: its worktree is on another machine, and the daemon touches only
  files of its own.
- The journal records what went through herrscher. A shell command an agent
  was allowed to run can still start rclone on herrscher's configuration, since
  it runs with your rights; the [trust model of approvals](approvals.md#who-may-change-the-approvals)
  holds here too.
- A token that expires is refreshed by rclone, which rewrites
  `drives/rclone.conf` itself. A refresh refused by the provider makes the
  verbs fail with rclone's message: run `herrscher drive remove` and
  `herrscher drive add` again.
