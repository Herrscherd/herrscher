package vendortest

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type turnOutcome uint8

const (
	outcomeAnswered turnOutcome = iota + 1
	outcomeHung
	outcomeCrashed
)

func (r *replay) stream(streams Streams) int {
	lines := bufio.NewScanner(streams.In)
	lines.Buffer(nil, maxLineBytes)
	hung := false
	for lines.Scan() {
		if hung {
			continue
		}
		outcome, err := r.answer(streams, lines.Bytes())
		if err != nil {
			return exitOf(streams.Err, err)
		}
		switch outcome {
		case outcomeCrashed:
			return exitOf(streams.Err, errCrashed)
		case outcomeHung:
			hung = true
		case outcomeAnswered:
		}
	}
	return exitOf(streams.Err, lines.Err())
}

var errCrashed = errors.New("crashed as scripted")

type userLine struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

func (r *replay) answer(streams Streams, line []byte) (turnOutcome, error) {
	var user userLine
	if err := json.Unmarshal(line, &user); err != nil {
		return 0, fmt.Errorf("cannot decode a user line: %w", err)
	}
	prompt := user.Message.Content
	if err := r.record("prompt " + strconv.Quote(prompt)); err != nil {
		return 0, err
	}
	turn, err := r.beginTurn()
	if err != nil {
		return 0, err
	}
	return r.play(streams, turn, prompt)
}

type turnState struct {
	reply    []string
	decided  *verdict
	reported int
}

func (r *replay) play(streams Streams, turn Turn, prompt string) (turnOutcome, error) {
	var state turnState
	for _, current := range turn.steps {
		outcome, finished, err := r.runStep(streams, current, prompt, &state)
		if err != nil {
			return 0, err
		}
		if finished {
			return outcome, nil
		}
	}
	return outcomeAnswered, writeJSON(streams.Out, r.result(strings.Join(state.reply, "\n\n"), prompt, state.reported))
}

func (r *replay) runStep(streams Streams, current step, prompt string, state *turnState) (turnOutcome, bool, error) {
	switch current.kind {
	case stepHang:
		return outcomeHung, true, nil
	case stepCrash:
		return outcomeCrashed, true, nil
	case stepFail:
		return outcomeAnswered, true, writeJSON(streams.Out, r.failure(current.text, prompt))
	case stepAPIError:
		return r.apiError(streams.Out, current.text, prompt)
	case stepEcho:
		return say(streams.Out, state, echoPrefix+lastLine(prompt))
	case stepText:
		return say(streams.Out, state, current.text)
	case stepTool:
		outcome, err := r.callTool(streams, current)
		if err != nil {
			return 0, false, err
		}
		state.decided = &outcome
		return outcomeAnswered, false, nil
	case stepExpect:
		return r.judge(streams.Out, state.decided, current.text, prompt)
	case stepUsage:
		state.reported = current.tokens
		return outcomeAnswered, false, writeJSON(streams.Out, assistantUsage(current.tokens))
	}
	return outcomeAnswered, false, nil
}

func (r *replay) apiError(out io.Writer, text, prompt string) (turnOutcome, bool, error) {
	if err := writeJSON(out, apiErrorText(text)); err != nil {
		return 0, true, err
	}
	return outcomeAnswered, true, writeJSON(out, r.failure(text, prompt))
}

func say(out io.Writer, state *turnState, text string) (turnOutcome, bool, error) {
	state.reply = append(state.reply, text)
	return outcomeAnswered, false, writeJSON(out, assistantText(text))
}

func (r *replay) judge(out io.Writer, decided *verdict, want, prompt string) (turnOutcome, bool, error) {
	err := expectation(decided, want)
	if err == nil {
		return outcomeAnswered, false, nil
	}
	if logErr := r.record("failed: " + err.Error()); logErr != nil {
		return 0, true, logErr
	}
	return outcomeAnswered, true, writeJSON(out, r.failure(err.Error(), prompt))
}

func (r *replay) callTool(streams Streams, tool step) (verdict, error) {
	r.toolCalls++
	call := r.toolCalls
	if err := writeJSON(streams.Out, assistantTool(call, tool)); err != nil {
		return verdict{}, err
	}
	outcome, err := gateTool(tool, streams.Err)
	if err != nil {
		return verdict{}, err
	}
	if err := r.settle(streams.Out, call, outcome); err != nil {
		return verdict{}, err
	}
	return outcome, nil
}

func (r *replay) settle(out io.Writer, call int, outcome verdict) error {
	callID := toolUseID(call)
	if err := r.record(outcomeLine(callID, outcome)); err != nil {
		return err
	}
	return writeJSON(out, toolResultOf(callID, outcome))
}

func outcomeLine(callID string, outcome verdict) string {
	if outcome.allowed {
		return "result " + callID + " " + wordAllowed
	}
	return "result " + callID + " " + wordDenied + ": " + outcome.reason
}

func isHealthProbe(args []string) bool {
	return slices.Contains(args, "--tools") && slices.Contains(args, "--no-session-persistence")
}

func (r *replay) oneShot(out io.Writer, args []string) error {
	prompt := oneShotPrompt(args)
	if err := r.record("prompt " + strconv.Quote(prompt)); err != nil {
		return err
	}
	if isHealthProbe(args) {
		return writeJSON(out, r.result(echoPrefix+lastLine(prompt), prompt, 0))
	}
	turn, err := r.beginTurn()
	if err != nil {
		return err
	}
	return writeJSON(out, r.result(oneShotReply(turn, prompt), prompt, 0))
}

func oneShotReply(turn Turn, prompt string) string {
	var texts []string
	for _, current := range turn.steps {
		if current.kind == stepText {
			texts = append(texts, current.text)
		}
	}
	if len(texts) == 0 {
		return echoPrefix + lastLine(prompt)
	}
	return strings.Join(texts, "\n\n")
}

func (r *replay) beginTurn() (Turn, error) {
	turn, err := r.nextTurn()
	if err != nil {
		return Turn{}, err
	}
	if err := r.spawnDescendants(turn); err != nil {
		return Turn{}, err
	}
	return turn, nil
}

var errNoLog = errors.New("spawn needs $VENDORTEST_LOG, which names a file in the work directory of the scenario")

func (r *replay) spawnDescendants(turn Turn) error {
	for _, pidFile := range turn.spawns {
		if err := r.spawn(pidFile); err != nil {
			return err
		}
	}
	return nil
}

func (r *replay) spawn(pidFile string) error {
	if r.files.Log == "" {
		return errNoLog
	}
	child := exec.CommandContext(context.Background(), "sleep", descendantSleepArg)
	if err := child.Start(); err != nil {
		return fmt.Errorf("cannot start the descendant: %w", err)
	}
	r.descendants = append(r.descendants, child)
	pidPath := filepath.Join(filepath.Dir(r.files.Log), filepath.Base(pidFile))
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(child.Process.Pid)), privateFile); err != nil {
		return errors.Join(fmt.Errorf("cannot record the descendant: %w", err), child.Process.Kill())
	}
	return nil
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
