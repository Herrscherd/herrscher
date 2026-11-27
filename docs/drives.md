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
neither leaks in nor gets written. The tokens and keys live in that file and
nowhere else: not in a worktree, not in the arguments of a verb, not in an
approval question, not on a gateway.

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
  rclone knows otherwise). A value with a space, a quote, a backslash, a
  bracket, `#` or `;` is refused rather than written.
- `--folders` lists the folders the agents see, comma separated, relative to
  the root of the remote (for `s3` and `r2`, the bucket comes first). Without
  it, the whole remote is exposed.
- `--agents` is the access of every agent the drive does not name, and of a
  session that runs no agent: `none` by default, `read` or `write`.

A drive name is 1 to 32 lowercase letters, digits, `-` or `_`. It is also the
name of the rclone remote.

## Who sees what

`drive grant <drive> --agent <name> --access none|read|write` sets the access
of one agent, and `--agent '*'` the default. `drive list` shows each drive,
its kind, its folders and the access of each agent. A session sees only the
drives its agent can at least read, with its own access and nothing about the
others.

Every path is checked before rclone runs. It is relative, slash separated,
UTF-8, with no empty, `.` or `..` segment, no backslash and no control
character, and it must sit inside one of the exposed folders: `Neublox/a.md`
is inside `Neublox`, `NeubloxOther/a.md` is not. herrscher builds the rclone
location itself, `<drive>:<path>`, and passes it after `--`.

## The verbs

| Verb | What it does | A session |
|---|---|---|
| `drive list` | the drives and their access | runs it |
| `drive ls <drive> [path]` | one level of a folder, 1000 entries at most; the root of a drive with folders lists those folders | needs read |
| `drive read <drive> <path>` | the file as text, 1 MiB at most, refused when it is not UTF-8 | needs read |
| `drive read <drive> <path> --to <file>` | copies the file, 64 MiB at most, into the session's worktree | needs read |
| `drive write <drive> <path> --from <file>` | sends a file of the session's worktree, 64 MiB at most | needs write, and asks you |
| `drive log [--drive d] [--agent a] [--session s] [--limit n]` | the journal, 50 entries by default | sees its own entries |
| `drive grant <drive> --agent <name> --access <level>` | sets an access | asks you |
| `drive remove <drive>` | disconnects the drive and deletes its token or key | asks you |

The self tools give `drive list`, `drive ls`, `drive read` and `drive log` a
tool each (`drive_list`, `drive_ls`, `drive_read`, `drive_log`). `drive write`
goes through `herrscher_run`.

A session's `--to` and `--from` are paths inside its worktree, and herrscher
opens them through an `os.Root` of the worktree: `..`, an absolute path and a
symbolic link that leads out are refused. From your terminal, `herrscher drive`
turns a relative `--to` or `--from` into an absolute path before it reaches the
daemon. A gateway user who is neither a session nor the operator's terminal
cannot use `--to` or `--from` at all, so no chat message writes a file on the
daemon's machine.

## Each write is asked

A session that holds `write` on a drive still asks for each write. The question
reaches its gateways like any other verb a session may not run alone, with the
four answers of [Approvals](approvals.md): `allow`, `allow --session`,
`allow --always` and `deny`. It reads
`Herrscher(drive write gdrive Neublox/report.md --from out.md)`, the exact
arguments the agent sent.

- `allow --always` writes the rule for that exact call: the same file, from the
  same source, passes from then on. It approves the pair, not the content, so
  a later write of other bytes from the same source passes too.
- A wider rule is written by hand, on the form the agent sends, such as
  `herrscher approve rule --session s1 'allow Herrscher(drive write gdrive Neublox/*)'`.
- The access check runs before the question. A session whose agent holds less
  than `write` is refused at once and you are not asked.
- A `steward` session is asked too: `drive write`, `drive grant` and
  `drive remove` are among the verbs a steward still puts to you. A `bypass`
  session asks as well, since the approval mode of a session does not decide its
  self tool calls.

## The journal

Every listing, read and write, done, refused or failed, is a line of
`drives/journal.jsonl`: the time, the session and its agent, the drive, the
path, the operation, the bytes and the outcome, with the reason of a refusal or
a failure. Your own operations are journaled with no session. The file is
rotated at 4 MiB, the previous one kept as `journal.jsonl.1`, and `drive log`
reads both.

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
