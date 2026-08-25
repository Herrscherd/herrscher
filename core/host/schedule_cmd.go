package host

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/schedule"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

// addScheduleCommands registers the six `schedule` verbs. The registry being
// neutral argv, a chat gateway binds them as they are, and the core never
// learns which gateway that is.
func addScheduleCommands(reg *cli.Registry, st *state.State, agents agentLookup, slot *schedulerSlot) error {
	// Every guard falls here, at creation, and not at firing: a schedule that
	// cannot run must be refused to the face of whoever writes it, rather than
	// fail silently every minute for weeks.
	if err := reg.Add(contracts.New("schedule", "add").
		Help("wake a session on a cadence, with no human in the loop").
		Param("name", "schedule name (unique)", true).
		Param("session", "target an existing session by name (exclusive with --agent)", false).
		Param("agent", "target a durable agent; its session is opened once and reused (exclusive with --session)", false).
		Param("project", "workspace sub-directory for the agent's session", false).
		Param("every", "fixed cadence, e.g. 30m or 24h (exclusive with --cron)", false).
		Param("cron", "five-field cron in the daemon's local time, e.g. '0 9 * * 1-5' (exclusive with --every)", false).
		Param("task", "the task handed to the session on each firing", true).
		Param("grace", "how late a window missed while the daemon was down may still fire (default 1h)", false).
		Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
			sc := schedule.Schedule{
				Name:      strings.TrimSpace(in.Get("name")),
				Session:   strings.TrimSpace(in.Get("session")),
				Agent:     strings.TrimSpace(in.Get("agent")),
				Project:   strings.TrimSpace(in.Get("project")),
				Task:      in.Get("task"),
				Every:     strings.TrimSpace(in.Get("every")),
				Cron:      strings.TrimSpace(in.Get("cron")),
				Grace:     strings.TrimSpace(in.Get("grace")),
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
			}
			if err := schedule.Validate(sc); err != nil {
				return "", err
			}
			if sc.Agent != "" {
				if _, ok := agents.Get(sc.Agent); !ok {
					return "", fmt.Errorf("unknown agent %q", sc.Agent)
				}
			}
			for _, existing := range st.SnapshotSchedules() {
				if existing.Name == sc.Name {
					// Two schedules of one name would fight over the same owned
					// session, and `rm` would no longer know which to aim at.
					return "", fmt.Errorf("a schedule named %q already exists", sc.Name)
				}
			}
			if err := st.PutSchedule(sc); err != nil {
				return "", err
			}
			out := "scheduled " + sc.Name + " (" + cadenceOf(sc) + ")"
			// A named session is not required to exist, because one can be opened
			// after the schedule. But a typo would otherwise buy a schedule that
			// silently skips every window forever, so it is said now, once, rather
			// than left to be discovered by its silence.
			if sc.Session != "" && !sessionKnown(st, sc.Session) {
				out += "; note: no session named " + strconv.Quote(sc.Session) +
					" right now, so its windows are skipped until there is one"
			}
			return out, nil
		})); err != nil {
		return err
	}

	if err := reg.Add(contracts.New("schedule", "list").
		Help("list the schedules, with their target, cadence, state and next window").
		Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
			rows := st.SnapshotSchedules()
			if len(rows) == 0 {
				return "no schedules yet", nil
			}
			now := time.Now()
			var b strings.Builder
			for _, sc := range rows {
				status := "live"
				if sc.Paused {
					status = "paused"
				}
				last := sc.LastRun
				if last == "" {
					last = "never"
				}
				// The next window is computed from now rather than from the anchor,
				// because that is the question the operator is asking: not "which
				// window follows the last run" but "when does this next wake up".
				// A window already reached is the exception: saying now plus a
				// period would name a time the schedule will not wait for.
				next := "unknown"
				switch {
				case !sc.Paused && schedule.Due(sc, now):
					next = "due"
				default:
					if at, err := schedule.Next(sc, now); err == nil {
						next = at.Format(time.RFC3339)
					}
				}
				fmt.Fprintf(&b, "%s\t%s\t%s\t%s\tlast %s\tnext %s\n",
					sc.Name, targetOf(sc), cadenceOf(sc), status, last, next)
			}
			return strings.TrimRight(b.String(), "\n"), nil
		})); err != nil {
		return err
	}

	if err := reg.Add(contracts.New("schedule", "rm").
		Help("remove a schedule; the session it opened is left alone").
		Param("name", "schedule name", true).
		Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
			name := strings.TrimSpace(in.Get("name"))
			ok, err := st.RemoveSchedule(name)
			if err != nil {
				return "", err
			}
			if !ok {
				return "", fmt.Errorf("no schedule named %q", name)
			}
			// Says what it does not do: removing a schedule does not destroy the
			// session it had opened, and a worktree never goes on a gesture that
			// did not name it.
			return "removed " + name + " (its session, if any, is still there; close it with `session close`)", nil
		})); err != nil {
		return err
	}

	for _, v := range []struct {
		verb   string
		paused bool
		help   string
	}{
		{"pause", true, "stop firing a schedule, keeping it and its history"},
		{"resume", false, "let a paused schedule fire again, counting from now"},
	} {
		if err := reg.Add(contracts.New("schedule", v.verb).
			Help(v.help).
			Param("name", "schedule name", true).
			Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
				name := strings.TrimSpace(in.Get("name"))
				ok, err := st.SetSchedulePaused(name, v.paused)
				if err != nil {
					return "", err
				}
				if !ok {
					return "", fmt.Errorf("no schedule named %q", name)
				}
				if !v.paused {
					// Resuming restarts the cadence from now. Without this, a
					// schedule paused for a week would find its window long past
					// and fire the instant it came back, which is not what
					// anybody means by resume.
					if err := st.StampScheduleRun(name, time.Now().UTC().Format(time.RFC3339)); err != nil {
						return "", err
					}
				}
				return v.verb + "d " + name, nil
			})); err != nil {
			return err
		}
	}

	// The try by hand. It makes the whole thing checkable without waiting for a
	// window, and that is what lets an operator read a task's reply before
	// letting it run on its own.
	if err := reg.Add(contracts.New("schedule", "run").
		Help("fire a schedule right now, out of band; the cadence is not moved").
		Param("name", "schedule name", true).
		Do(func(cmdCtx context.Context, in contracts.Input) (string, error) {
			name := strings.TrimSpace(in.Get("name"))
			if slot == nil || slot.sched == nil {
				return "", fmt.Errorf("no running daemon to fire %q: `schedule run` needs `herrscher serve`", name)
			}
			if err := slot.sched.fireNow(cmdCtx, name); err != nil {
				return "", err
			}
			return "fired " + name, nil
		})); err != nil {
		return err
	}
	return nil
}

func sessionKnown(st *state.State, name string) bool {
	for _, sess := range st.SnapshotSessions() {
		if sess.Name == name && !sess.Archived {
			return true
		}
	}
	return false
}

func targetOf(sc schedule.Schedule) string {
	if sc.Session != "" {
		return "session:" + sc.Session
	}
	return "agent:" + sc.Agent
}

func cadenceOf(sc schedule.Schedule) string {
	if sc.Every != "" {
		return "every " + sc.Every
	}
	return "cron " + sc.Cron
}
