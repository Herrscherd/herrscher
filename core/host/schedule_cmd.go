package host

import (
	"context"
	"fmt"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/schedule"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func addScheduleCommands(reg *cli.Registry, lang i18n.Language, hostState *state.State, agents agentLookup, slot *schedulerSlot) error {
	return addVerbs(
		reg,
		scheduleAddVerb(lang, hostState, agents),
		scheduleListVerb(lang, hostState),
		scheduleRmVerb(lang, hostState),
		schedulePauseVerb(lang, hostState, "pause", true, clitext.CmdHelpSchedulePause),
		schedulePauseVerb(lang, hostState, "resume", false, clitext.CmdHelpScheduleResume),
		scheduleRunVerb(lang, slot),
	)
}

func scheduleAddVerb(lang i18n.Language, store *state.State, agents agentLookup) contracts.Cmd {
	return contracts.New(nsSchedule, "add").
		Help(clitext.CmdHelpScheduleAdd).
		Param(paramName, clitext.CmdParamScheduleAddName, true).
		Param(paramSession, clitext.CmdParamScheduleAddSession, false).
		Param(paramAgent, clitext.CmdParamScheduleAddAgent, false).
		Param("project", clitext.CmdParamScheduleAddProject, false).
		Param("every", clitext.CmdParamScheduleAddEvery, false).
		Param("cron", clitext.CmdParamScheduleAddCron, false).
		Param("task", clitext.CmdParamScheduleAddTask, true).
		Param("grace", clitext.CmdParamScheduleAddGrace, false).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(addSchedule(lang, store, agents, in))
		})
}

func addSchedule(lang i18n.Language, store *state.State, agents agentLookup, input contracts.Input) (string, error) {
	sched := schedule.Schedule{
		Name:      strings.TrimSpace(input.Get(paramName)),
		Session:   strings.TrimSpace(input.Get(paramSession)),
		Agent:     strings.TrimSpace(input.Get(paramAgent)),
		Project:   strings.TrimSpace(input.Get("project")),
		Task:      input.Get("task"),
		Every:     strings.TrimSpace(input.Get("every")),
		Cron:      strings.TrimSpace(input.Get("cron")),
		Grace:     strings.TrimSpace(input.Get("grace")),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := checkNewSchedule(store, agents, sched); err != nil {
		return "", err
	}
	if err := store.PutSchedule(sched); err != nil {
		return "", err
	}
	text := replyText(lang)
	out := text.Text(clitext.HostScheduleScheduled, clitext.Params{clitext.ParamName: sched.Name, clitext.ParamCadence: cadenceOf(lang, sched)})
	if sched.Session != "" && !sessionKnown(store, sched.Session) {
		out += text.Text(clitext.HostScheduleNoSession, clitext.Params{clitext.ParamName: sched.Session})
	}
	return out, nil
}

func checkNewSchedule(store *state.State, agents agentLookup, definition schedule.Schedule) error {
	if err := schedule.Validate(definition, time.Now()); err != nil {
		return err
	}
	if definition.Agent != "" {
		if _, ok := agents.Get(definition.Agent); !ok {
			return &UnknownError{Kind: kindAgent, Name: definition.Agent}
		}
	}
	schedules := store.SnapshotSchedules()
	for i := range schedules {
		if schedules[i].Name == definition.Name {
			return &scheduleExistsError{Name: definition.Name}
		}
	}
	return nil
}

func scheduleListVerb(lang i18n.Language, store *state.State) contracts.Cmd {
	return contracts.New(nsSchedule, "list").
		Help(clitext.CmdHelpScheduleList).
		Do(func(_ context.Context, _ contracts.Input) (contracts.Result, error) {
			return contracts.DataResult(listSchedules(lang, store)), nil
		})
}

func listSchedules(lang i18n.Language, st *state.State) string {
	rows := st.SnapshotSchedules()
	if len(rows) == 0 {
		return replyText(lang).Text(clitext.HostScheduleNone, nil)
	}
	now := time.Now()
	var body strings.Builder
	for index := range rows {
		row := rows[index]
		fmt.Fprintf(&body, "%s\t%s\t%s\t%s\t%s\t%s\n",
			row.Name, targetOf(row), cadenceOf(lang, row), scheduleStatus(lang, row), lastRunOf(lang, row), nextWindowOf(lang, row, now))
	}
	return strings.TrimRight(body.String(), "\n")
}

func scheduleStatus(lang i18n.Language, sched schedule.Schedule) string {
	var key clitext.Key = clitext.HostScheduleLive
	if sched.Paused {
		key = clitext.HostScheduleStatusPaused
	}
	return replyText(lang).Text(key, nil)
}

func lastRunOf(lang i18n.Language, sched schedule.Schedule) string {
	when := replyText(lang).Text(clitext.HostScheduleNever, nil)
	if sched.LastRun != "" {
		when = sched.LastRun
	}
	return replyText(lang).Text(clitext.HostScheduleLastRun, clitext.Params{clitext.ParamWhen: when})
}

func nextWindowOf(lang i18n.Language, sched schedule.Schedule, now time.Time) string {
	text := replyText(lang)
	switch {
	case sched.Paused:
		return text.Text(clitext.HostScheduleNextRun, clitext.Params{clitext.ParamWhen: "-"})
	case schedule.Due(sched, now):
		return text.Text(clitext.HostScheduleNextRun, clitext.Params{clitext.ParamWhen: text.Text(clitext.HostScheduleDue, nil)})
	}
	when, err := schedule.NextWindow(sched)
	if err != nil {
		return text.Text(clitext.HostScheduleNextRun, clitext.Params{clitext.ParamWhen: text.Text(clitext.HostScheduleUnknown, nil)})
	}
	return text.Text(clitext.HostScheduleNextRun, clitext.Params{clitext.ParamWhen: when.Format(time.RFC3339)})
}

func scheduleRmVerb(lang i18n.Language, store *state.State) contracts.Cmd {
	return contracts.New(nsSchedule, "rm").
		Help(clitext.CmdHelpScheduleRm).
		Param(paramName, clitext.CmdParamScheduleRmName, true).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			name := strings.TrimSpace(in.Get(paramName))
			ok, err := store.RemoveSchedule(name)
			if err != nil {
				return contracts.Result{}, err
			}
			if !ok {
				return contracts.Result{}, &NotFoundError{Kind: kindSchedule, Name: name}
			}
			return contracts.DataResult(replyText(lang).Text(clitext.HostScheduleRemoved, clitext.Params{clitext.ParamName: name})), nil
		})
}

func schedulePauseVerb(lang i18n.Language, store *state.State, verb string, paused bool, helpKey string) contracts.Cmd {
	return contracts.New(nsSchedule, verb).
		Help(helpKey).
		Param(paramName, clitext.CmdParamScheduleName, true).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(setSchedulePaused(lang, store, paused, strings.TrimSpace(in.Get(paramName))))
		})
}

func setSchedulePaused(lang i18n.Language, store *state.State, paused bool, name string) (string, error) {
	ok, err := store.SetSchedulePaused(name, paused)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", &NotFoundError{Kind: kindSchedule, Name: name}
	}
	if !paused {
		if err := store.StampScheduleRun(name, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return "", err
		}
	}
	var key clitext.Key = clitext.HostScheduleResumed
	if paused {
		key = clitext.HostSchedulePaused
	}
	return replyText(lang).Text(key, clitext.Params{clitext.ParamName: name}), nil
}

func scheduleRunVerb(lang i18n.Language, slot *schedulerSlot) contracts.Cmd {
	return contracts.New(nsSchedule, "run").
		Help(clitext.CmdHelpScheduleRun).
		Param(paramName, clitext.CmdParamScheduleRunName, true).
		Do(func(cmdCtx context.Context, in contracts.Input) (contracts.Result, error) {
			name := strings.TrimSpace(in.Get(paramName))
			if slot == nil || slot.sched == nil {
				return contracts.Result{}, &noDaemonToFireError{Name: name}
			}
			if err := slot.sched.fireNow(cmdCtx, name); err != nil {
				return contracts.Result{}, err
			}
			return contracts.DataResult(replyText(lang).Text(clitext.HostScheduleFired, clitext.Params{clitext.ParamName: name})), nil
		})
}

func sessionKnown(st *state.State, name string) bool {
	sessions := st.SnapshotSessions()
	for idx := range sessions {
		sess := sessions[idx]
		if sess.Name == name && !sess.Archived {
			return true
		}
	}
	return false
}

func targetOf(definition schedule.Schedule) string {
	if definition.Session != "" {
		return "session:" + definition.Session
	}
	return "agent:" + definition.Agent
}

func cadenceOf(lang i18n.Language, definition schedule.Schedule) string {
	text := replyText(lang)
	if definition.Every != "" {
		return text.Text(clitext.HostScheduleEvery, clitext.Params{clitext.ParamEvery: definition.Every})
	}
	return text.Text(clitext.HostScheduleCron, clitext.Params{clitext.ParamCron: definition.Cron})
}
