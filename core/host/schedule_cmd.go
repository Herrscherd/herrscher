package host

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/schedule"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func addScheduleCommands(reg *cli.Registry, hostState *state.State, agents agentLookup, slot *schedulerSlot) error {
	return addVerbs(reg,
		scheduleAddVerb(hostState, agents),
		scheduleListVerb(hostState),
		scheduleRmVerb(hostState),
		schedulePauseVerb(hostState, "pause", true, clitext.CmdHelpSchedulePause),
		schedulePauseVerb(hostState, "resume", false, clitext.CmdHelpScheduleResume),
		scheduleRunVerb(slot),
	)
}

func scheduleAddVerb(store *state.State, agents agentLookup) contracts.Cmd {
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
			return cli.Data(addSchedule(store, agents, in))
		})
}

func addSchedule(store *state.State, agents agentLookup, input contracts.Input) (string, error) {
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
	out := "scheduled " + sched.Name + " (" + cadenceOf(sched) + ")"
	if sched.Session != "" && !sessionKnown(store, sched.Session) {
		out += "; note: no session named " + strconv.Quote(sched.Session) +
			" right now, so its windows are skipped until there is one"
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

func scheduleListVerb(store *state.State) contracts.Cmd {
	return contracts.New(nsSchedule, "list").
		Help(clitext.CmdHelpScheduleList).
		Do(func(_ context.Context, _ contracts.Input) (contracts.Result, error) {
			return contracts.DataResult(listSchedules(store)), nil
		})
}

func listSchedules(st *state.State) string {
	rows := st.SnapshotSchedules()
	if len(rows) == 0 {
		return "no schedules yet"
	}
	now := time.Now()
	var body strings.Builder
	for index := range rows {
		row := rows[index]
		fmt.Fprintf(&body, "%s\t%s\t%s\t%s\tlast %s\tnext %s\n",
			row.Name, targetOf(row), cadenceOf(row), scheduleStatus(row), lastRunOf(row), nextWindowOf(row, now))
	}
	return strings.TrimRight(body.String(), "\n")
}

func scheduleStatus(sc schedule.Schedule) string {
	if sc.Paused {
		return "paused"
	}
	return "live"
}

func lastRunOf(sched schedule.Schedule) string {
	if sched.LastRun == "" {
		return "never"
	}
	return sched.LastRun
}

func nextWindowOf(sched schedule.Schedule, now time.Time) string {
	switch {
	case sched.Paused:
		return "-"
	case schedule.Due(sched, now):
		return "due"
	}
	when, err := schedule.NextWindow(sched)
	if err != nil {
		return "unknown"
	}
	return when.Format(time.RFC3339)
}

func scheduleRmVerb(store *state.State) contracts.Cmd {
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
			return contracts.DataResult("removed " + name + " (its session, if any, is still there; close it with `session close`)"), nil
		})
}

func schedulePauseVerb(store *state.State, verb string, paused bool, helpKey string) contracts.Cmd {
	return contracts.New(nsSchedule, verb).
		Help(helpKey).
		Param(paramName, clitext.CmdParamScheduleName, true).
		Do(func(_ context.Context, in contracts.Input) (contracts.Result, error) {
			return cli.Data(setSchedulePaused(store, verb, paused, strings.TrimSpace(in.Get(paramName))))
		})
}

func setSchedulePaused(store *state.State, verb string, paused bool, name string) (string, error) {
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
	return verb + "d " + name, nil
}

func scheduleRunVerb(slot *schedulerSlot) contracts.Cmd {
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
			return contracts.DataResult("fired " + name), nil
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

func cadenceOf(definition schedule.Schedule) string {
	if definition.Every != "" {
		return "every " + definition.Every
	}
	return "cron " + definition.Cron
}
