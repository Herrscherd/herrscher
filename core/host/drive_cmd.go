package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/drives"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/authz"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	nsDrive          = "drive"
	drivesDirName    = "drives"
	rcloneEnvSuffix  = "RCLONE"
	paramDrive       = "drive"
	paramDrivePath   = "path"
	paramDriveTo     = "to"
	paramDriveFrom   = "from"
	paramDriveAgent  = "agent"
	paramDriveAccess = "access"
	paramDriveLimit  = "limit"
	verbDriveLs      = "drive ls"
	verbDriveRead    = "drive read"
	verbDriveWrite   = "drive write"
	driveLogDefault  = 50
	driveLogMaximum  = 1000
)

var (
	errDriveNameMissing    = errors.New("name a drive (see `drive list`)")
	errDriveMixedForms     = errors.New("name the drive and the path once each, either as words or as --drive and --path, and nothing more")
	errDriveAgentMissing   = errors.New("name an agent, or * for every agent the drive does not name")
	errDriveLocalForbidden = errors.New("only a session or the operator's own terminal reads into or writes from a local file")
)

// DrivesDir is the directory that holds the drives of the daemon whose state lives at statePath.
func DrivesDir(statePath string) string {
	return filepath.Join(filepath.Dir(statePath), drivesDirName)
}

// DriveRclone is the rclone runner of a drive catalogue, HERRSCHER_RCLONE overriding the command.
func DriveRclone(catalog drives.Catalog) drives.Rclone {
	return drives.NewRclone(envx.Get(rcloneEnvSuffix), catalog.RcloneConfig())
}

func driveService(partDir string) drives.Service {
	catalog := drives.Open(filepath.Join(partDir, drivesDirName))
	return drives.NewService(catalog, DriveRclone(catalog)).Protecting(agent.ControlPaths())
}

type driveVerbs struct {
	service  drives.Service
	store    *state.State
	messages *clitext.Messages
	lang     languageSource
}

func addAccessCommands(reg *cli.Registry, cliMessages *clitext.Messages, lang languageSource, store *state.State, partDir string) error {
	if err := addRoleCommands(reg, cliMessages, lang, store); err != nil {
		return err
	}
	return addVerbs(reg, newDriveVerbs(cliMessages, lang, store, partDir).commands()...)
}

func newDriveVerbs(cliMessages *clitext.Messages, lang languageSource, store *state.State, partDir string) driveVerbs {
	return driveVerbs{service: driveService(partDir), store: store, messages: cliMessages, lang: lang}
}

func (d driveVerbs) commands() []contracts.Cmd {
	return []contracts.Cmd{
		contracts.New(nsDrive, "list").Help(clitext.CmdHelpDriveList).Reads().Do(d.list),
		contracts.New(nsDrive, "ls").Help(clitext.CmdHelpDriveLs).
			ValueParam(paramDrive, clitext.CmdParamDriveName, false).
			ValueParam(paramDrivePath, clitext.CmdParamDrivePath, false).
			Reads().Do(d.ls),
		contracts.New(nsDrive, "read").Help(clitext.CmdHelpDriveRead).
			ValueParam(paramDrive, clitext.CmdParamDriveName, false).
			ValueParam(paramDrivePath, clitext.CmdParamDrivePath, false).
			ValueParam(paramDriveTo, clitext.CmdParamDriveTo, false).
			Reads().Do(d.read),
		contracts.New(nsDrive, "write").Help(clitext.CmdHelpDriveWrite).
			ValueParam(paramDrive, clitext.CmdParamDriveName, false).
			ValueParam(paramDrivePath, clitext.CmdParamDrivePath, false).
			Param(paramDriveFrom, clitext.CmdParamDriveFrom, true).
			Do(d.write),
		contracts.New(nsDrive, "log").Help(clitext.CmdHelpDriveLog).
			ValueParam(paramDrive, clitext.CmdParamDriveName, false).
			ValueParam(paramDriveAgent, clitext.CmdParamDriveAgent, false).
			ValueParam(paramSession, clitext.CmdParamDriveSession, false).
			ValueParam(paramDriveLimit, clitext.CmdParamDriveLimit, false).
			Reads().Do(d.log),
		contracts.New(nsDrive, "grant").Help(clitext.CmdHelpDriveGrant).
			ValueParam(paramDrive, clitext.CmdParamDriveName, false).
			Param(paramDriveAgent, clitext.CmdParamDriveAgent, true).
			Param(paramDriveAccess, clitext.CmdParamDriveAccess, true).
			Do(d.grant),
		contracts.New(nsDrive, "remove").Help(clitext.CmdHelpDriveRemove).
			ValueParam(paramDrive, clitext.CmdParamDriveName, false).
			Do(d.remove),
	}
}

type driveTarget struct {
	drive string
	path  string
}

func driveTargetOf(input contracts.Input, takesPath bool) (driveTarget, error) {
	rest := input.Rest
	drive, flagged := input.Lookup(paramDrive)
	if !flagged && len(rest) > 0 {
		drive, rest = rest[0], rest[1:]
	} else if flagged && len(rest) > 0 && !takesPath {
		return driveTarget{}, errDriveMixedForms
	}
	path, pathFlagged := input.Lookup(paramDrivePath)
	if takesPath && !pathFlagged && len(rest) > 0 {
		path, rest = rest[0], rest[1:]
	}
	if len(rest) > 0 || (!takesPath && pathFlagged) {
		return driveTarget{}, errDriveMixedForms
	}
	drive = strings.TrimSpace(drive)
	if drive == "" {
		return driveTarget{}, errDriveNameMissing
	}
	return driveTarget{drive: drive, path: path}, nil
}

func driveVerbTakesPath(verb string) bool {
	return verb == verbDriveLs || verb == verbDriveRead || verb == verbDriveWrite
}

func canonicalDriveSubject(path []string, input contracts.Input) (string, bool) {
	verb := strings.Join(path, " ")
	if len(path) == 0 || path[0] != nsDrive || verb == "drive list" || verb == "drive log" {
		return "", false
	}
	target, err := driveTargetOf(input, driveVerbTakesPath(verb))
	if err != nil {
		return "", false
	}
	tokens := append([]string(nil), path...)
	tokens = append(tokens, target.drive)
	if driveVerbTakesPath(verb) {
		tokens = append(tokens, target.path)
	}
	for _, flag := range []string{paramDriveTo, paramDriveFrom, paramDriveAgent, paramDriveAccess} {
		if value, given := input.Lookup(flag); given {
			tokens = append(tokens, "--"+flag, value)
		}
	}
	return verbSubject(tokens), true
}

func (d driveVerbs) caller(ctx context.Context) drives.Caller {
	return driveCallerOf(d.store, principalOf(ctx))
}

func driveCallerOf(store *state.State, principal authz.Principal) drives.Caller {
	if principal.Session == "" {
		return drives.Caller{}
	}
	caller := drives.Caller{Session: principal.Session}
	if store == nil {
		return caller
	}
	if sess, ok := store.FindSession(principal.Session); ok {
		caller.Agent = sess.Agent
		if sess.Host == "" || sess.Host == state.LocalHost {
			caller.Worktree = sess.Worktree
		}
	}
	return caller
}

func (d driveVerbs) localCaller(ctx context.Context) (drives.Caller, error) {
	principal := principalOf(ctx)
	if principal.Session == "" && principal.ID != authz.LocalPrincipal {
		return drives.Caller{}, errDriveLocalForbidden
	}
	return d.caller(ctx), nil
}

func (d driveVerbs) text(key clitext.Key, params clitext.Params) string {
	return replyText(d.messages, d.lang()).Text(key, params)
}

func (d driveVerbs) list(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	visible, err := d.service.Visible(d.caller(ctx))
	if err != nil {
		return contracts.Result{}, err
	}
	if input.JSON {
		return driveJSON(visible)
	}
	if len(visible) == 0 {
		return contracts.DataResult(d.text(clitext.HostDriveNone, nil)), nil
	}
	lines := make([]string, 0, len(visible))
	for _, drive := range visible {
		lines = append(lines, driveLine(drive))
	}
	return contracts.DataResult(strings.Join(lines, "\n")), nil
}

func driveLine(drive drives.Drive) string {
	var access strings.Builder
	access.WriteString(drive.Default.String())
	for _, agent := range slices.Sorted(maps.Keys(drive.Agents)) {
		access.WriteString(", " + agent + ": " + drive.Agents[agent].String())
	}
	return fmt.Sprintf("%s  %s  %s  %s", drive.Name, drive.Kind, drive.FolderList(), access.String())
}

func (d driveVerbs) ls(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	target, err := driveTargetOf(input, true)
	if err != nil {
		return contracts.Result{}, err
	}
	items, truncated, err := d.service.List(ctx, d.caller(ctx), target.drive, target.path)
	if err != nil {
		return contracts.Result{}, err
	}
	if input.JSON {
		return driveJSON(items)
	}
	if len(items) == 0 {
		return contracts.DataResult(d.text(clitext.HostDriveEmptyFolder, clitext.Params{clitext.ParamDrive: target.drive, clitext.ParamPath: target.path})), nil
	}
	lines := make([]string, 0, len(items)+1)
	for _, item := range items {
		lines = append(lines, itemLine(item))
	}
	if truncated {
		lines = append(lines, d.text(clitext.HostDriveTruncated, clitext.Params{clitext.ParamLimit: drives.ListEntries}))
	}
	return contracts.DataResult(strings.Join(lines, "\n")), nil
}

func itemLine(item drives.Item) string {
	if item.IsDir {
		return item.Name + "/"
	}
	return fmt.Sprintf("%s  %d  %s", item.Name, item.Size, item.ModTime.UTC().Format(time.RFC3339))
}

func (d driveVerbs) read(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	target, err := driveTargetOf(input, true)
	if err != nil {
		return contracts.Result{}, err
	}
	dest := input.Get(paramDriveTo)
	if dest == "" {
		content, readErr := d.service.Read(ctx, d.caller(ctx), target.drive, target.path)
		return cli.Data(content, readErr)
	}
	caller, err := d.localCaller(ctx)
	if err != nil {
		return contracts.Result{}, err
	}
	size, err := d.service.ReadTo(ctx, caller, target.drive, target.path, dest)
	if err != nil {
		return contracts.Result{}, err
	}
	return contracts.DataResult(d.text(clitext.HostDriveCopied, clitext.Params{clitext.ParamBytes: size, clitext.ParamDrive: target.drive, clitext.ParamPath: target.path, clitext.ParamTo: dest})), nil
}

func (d driveVerbs) write(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	target, err := driveTargetOf(input, true)
	if err != nil {
		return contracts.Result{}, err
	}
	caller, err := d.localCaller(ctx)
	if err != nil {
		return contracts.Result{}, err
	}
	size, err := d.service.Write(ctx, caller, target.drive, target.path, input.Get(paramDriveFrom))
	if err != nil {
		return contracts.Result{}, err
	}
	return contracts.DataResult(d.text(clitext.HostDriveWrote, clitext.Params{clitext.ParamBytes: size, clitext.ParamDrive: target.drive, clitext.ParamPath: target.path})), nil
}

func (d driveVerbs) log(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	limit := driveLogDefault
	if raw := strings.TrimSpace(input.Get(paramDriveLimit)); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return contracts.Result{}, &driveLimitError{Limit: raw}
		}
		limit = min(parsed, driveLogMaximum)
	}
	filter := drives.Filter{Drive: firstOf(input.Get(paramDrive), input.Rest), Agent: strings.TrimSpace(input.Get(paramDriveAgent)), Session: strings.TrimSpace(input.Get(paramSession))}
	if caller := d.caller(ctx); caller.Session != "" {
		filter.Session = caller.Session
	}
	log, err := d.service.Log(filter, limit)
	if err != nil {
		return contracts.Result{}, err
	}
	if input.JSON {
		return driveJSON(log)
	}
	lines := make([]string, 0, len(log.Entries)+1)
	for index := range log.Entries {
		entry := &log.Entries[index]
		line := journalLine(entry)
		if entry.Suppressed > 0 {
			line += "  " + d.text(clitext.HostDriveLogSuppressed, clitext.Params{clitext.ParamCount: entry.Suppressed})
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		lines = append(lines, d.text(clitext.HostDriveLogEmpty, nil))
	}
	if log.Unreadable > 0 {
		lines = append(lines, d.text(clitext.HostDriveLogUnreadable, clitext.Params{clitext.ParamCount: log.Unreadable}))
	}
	return contracts.DataResult(strings.Join(lines, "\n")), nil
}

func journalLine(entry *drives.Entry) string {
	who := entry.Session
	if who == "" {
		who = "operator"
	}
	if entry.Agent != "" {
		who += " (" + entry.Agent + ")"
	}
	line := fmt.Sprintf("%s  %s  %s %s:%s  %s", entry.Time.UTC().Format(time.RFC3339), who, entry.Op, entry.Drive, entry.Path, entry.Outcome)
	if entry.Bytes > 0 {
		line += fmt.Sprintf("  %d bytes", entry.Bytes)
	}
	if entry.Reason != "" {
		line += "  " + entry.Reason
	}
	return line
}

func (d driveVerbs) grant(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	target, err := driveTargetOf(input, false)
	if err != nil {
		return contracts.Result{}, err
	}
	access, err := drives.ParseAccess(strings.TrimSpace(input.Get(paramDriveAccess)))
	if err != nil {
		return contracts.Result{}, err
	}
	named := strings.TrimSpace(input.Get(paramDriveAgent))
	if named == "" {
		return contracts.Result{}, errDriveAgentMissing
	}
	if err := d.service.Catalog().Update(ctx, target.drive, func(drive *drives.Drive) error {
		*drive = drive.WithAccess(named, access)
		return nil
	}); err != nil {
		return contracts.Result{}, err
	}
	return contracts.DataResult(d.text(clitext.HostDriveGranted, clitext.Params{clitext.ParamDrive: target.drive, clitext.ParamAgent: named, clitext.ParamAccess: access.String()})), nil
}

func (d driveVerbs) remove(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	target, err := driveTargetOf(input, false)
	if err != nil {
		return contracts.Result{}, err
	}
	if _, err := d.service.Catalog().Find(target.drive); err != nil {
		return contracts.Result{}, err
	}
	if err := d.service.Rclone().DeleteRemote(ctx, target.drive); err != nil {
		return contracts.Result{}, err
	}
	if err := d.service.Catalog().Remove(ctx, target.drive); err != nil {
		return contracts.Result{}, err
	}
	return contracts.DataResult(d.text(clitext.HostDriveRemoved, clitext.Params{clitext.ParamDrive: target.drive})), nil
}

func driveJSON(value any) (contracts.Result, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return contracts.Result{}, err
	}
	return contracts.DataResult(string(raw)), nil
}

type driveLimitError struct {
	Limit string
}

func (e *driveLimitError) Error() string {
	return fmt.Sprintf("drive log: --limit takes a positive number, not %q", e.Limit)
}

func (h *hub) guardDrive(ctx context.Context, session, verb string, input contracts.Input) error {
	target, err := driveTargetOf(input, true)
	if err != nil {
		return err
	}
	request := drives.Request{Drive: target.drive, Path: target.path, Op: drives.OpRead, Local: input.Get(paramDriveTo)}
	switch verb {
	case verbDriveLs:
		request.Op = drives.OpList
	case verbDriveWrite:
		request.Op, request.Local = drives.OpWrite, input.Get(paramDriveFrom)
	}
	caller := driveCallerOf(h.st, authz.SessionPrincipal(session))
	_, _, err = driveService(h.partDir).Admit(ctx, caller, request)
	return err
}
