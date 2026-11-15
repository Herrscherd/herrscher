package manager

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSessionListUsesGatewayChannelRef(t *testing.T) {
	h, d, _, _, _, st := newTestHandler(t, "category")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionCreateRun(context.Background(), args("name", "demo")); err != nil {
		t.Fatal(err)
	}
	out, err := cli.Plain(h.sessionListRun(context.Background(), contracts.Input{}))
	if err != nil {
		t.Fatal(err)
	}
	want := d.ChannelRef("new-demo")
	if !strings.Contains(out, want) {
		t.Fatalf("list output %q must render the gateway channel ref %q", out, want)
	}
}

func TestSessionListJSON(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.Sessions = []state.Session{
		{Name: "alpha", Agent: "roblox", Parent: ""},
		{Name: "beta", Agent: "roblox", Parent: "alpha"},
	}
	out, err := cli.Plain(h.sessionListRun(context.Background(), contracts.Input{JSON: true}))
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, out)
	}
	if len(got) != 2 || got[1]["parent"] != "alpha" {
		t.Fatalf("unexpected: %v", got)
	}
	if got[0]["id"] != "alpha" || got[1]["id"] != "beta" {
		t.Fatalf("id must equal name: %v", got)
	}
	if _, present := got[0]["parent"]; present {
		t.Fatalf("root session must omit parent, got: %v", got[0])
	}
}

func TestSessionListAggregatesUsage(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.Sessions = []state.Session{{Name: "alpha", Agent: "roblox"}}
	p := state.TranscriptPath(h.PartDir(), "alpha")
	for _, e := range []state.TranscriptEntry{
		{Ts: "2026-07-26T00:00:00Z", Role: "user", Text: "build a tower"},
		{Ts: "2026-07-26T00:00:05Z", Role: "assistant", Text: "done", Cost: 0.02, TokensIn: 30, TokensOut: 55, CacheRead: 12, CacheCreate: 3, DurMs: 4200},
		{Ts: "2026-07-26T00:01:00Z", Role: "user", Text: "now add a door"},
		{Ts: "2026-07-26T00:01:08Z", Role: "assistant", Text: "ok", Cost: 0.03, TokensIn: 40, TokensOut: 60, CacheRead: 5, CacheCreate: 1, DurMs: 8000},
	} {
		if err := state.AppendTranscript(p, e); err != nil {
			t.Fatal(err)
		}
	}
	out, err := cli.Plain(h.sessionListRun(context.Background(), contracts.Input{JSON: true}))
	if err != nil {
		t.Fatal(err)
	}
	var rows []sessionJSON
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, out)
	}
	if len(rows) != 1 || rows[0].Usage == nil {
		t.Fatalf("want usage present: %+v", rows)
	}
	u := rows[0].Usage
	if u.Turns != 2 || u.TokensOut != 115 || u.TokensIn != 70 || u.CacheRead != 17 || u.CacheCreate != 4 {
		t.Fatalf("aggregate wrong: %+v", u)
	}
	if u.Cost < 0.049 || u.Cost > 0.051 {
		t.Fatalf("cost aggregate wrong: %v", u.Cost)
	}
	if u.StartedAt != "2026-07-26T00:00:00Z" {
		t.Fatalf("started_at wrong: %q", u.StartedAt)
	}
	if rows[0].Task != "now add a door" {
		t.Fatalf("task should be last user text, got: %q", rows[0].Task)
	}
}

func TestSessionListEmitsCoordination(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.Sessions = []state.Session{
		{Name: "lead1", Agent: "roblox"},
		{Name: "solo1", Agent: "roblox"},
	}
	h.SetCoordinationReader(fakeCoordReader{views: map[string]CoordView{
		"lead1": {Role: "lead", Lead: "lead1", Reported: 2, Expected: 3, Complete: false},
	}})
	out, err := cli.Plain(h.sessionListRun(context.Background(), contracts.Input{JSON: true}))
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, out)
	}
	byName := make(map[string]map[string]any)
	for _, r := range rows {
		name, ok := r["name"].(string)
		if !ok {
			t.Fatalf("row without a name: %v", r)
		}
		byName[name] = r
	}
	coord, ok := byName["lead1"]["coordination"].(map[string]any)
	if !ok {
		t.Fatalf("lead1 should carry coordination, got %v", byName["lead1"])
	}
	if coord["role"] != "lead" || coord["reported"] != 2.0 || coord["expected"] != 3.0 {
		t.Fatalf("bad coordination: %v", coord)
	}
	if _, present := byName["solo1"]["coordination"]; present {
		t.Fatalf("solo1 must omit coordination: %v", byName["solo1"])
	}
}

func TestSessionListOmitsCoordinationWhenUnwired(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.Sessions = []state.Session{{Name: "s1", Agent: "roblox"}}
	out, err := cli.Plain(h.sessionListRun(context.Background(), contracts.Input{JSON: true}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "coordination") {
		t.Fatalf("unwired handler must not emit coordination: %s", out)
	}
}

func TestSessionWhoJSON(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	st.Sessions = []state.Session{{Name: "alpha", Agent: "roblox", Parent: "lead"}}
	out, err := cli.Plain(h.sessionWhoRun(context.Background(), contracts.Input{
		Args: map[string]string{"name": "alpha"}, JSON: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v (%q)", err, out)
	}
	if got["name"] != "alpha" || got["parent"] != "lead" {
		t.Fatalf("unexpected: %v", got)
	}
}

func TestSessionListEmptyAndPopulated(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	if out, err := cli.Plain(h.sessionListRun(context.Background(), args())); err != nil || !strings.Contains(out, "No active sessions") {
		t.Fatalf("empty list: out=%q err=%v", out, err)
	}
	if err := st.AddSession(state.Session{Name: "demo", ChannelID: "c1", Type: "text"}); err != nil {
		t.Fatal(err)
	}
	out, err := cli.Plain(h.sessionListRun(context.Background(), args()))
	if err != nil || !strings.Contains(out, "demo") {
		t.Fatalf("populated list should mention demo: out=%q err=%v", out, err)
	}
}

func TestSessionListNeutralForTerminalHome(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "terminal")
	if err := st.SetHome(state.HomeRef{ID: "term-home", Type: "terminal"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "alpha", ChannelID: "terminal/alpha-1", Type: "text"}); err != nil {
		t.Fatal(err)
	}
	out, err := cli.Plain(h.sessionListRun(context.Background(), args()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "<#") {
		t.Fatalf("terminal home list must not leak channel-mention syntax: %q", out)
	}
	if !strings.Contains(out, "terminal/alpha-1") {
		t.Fatalf("list should show the bare channel id: %q", out)
	}
}

func TestSessionWhoListsParticipants(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	if err := st.AddSession(state.Session{Name: "demo", ChannelID: "c1", Type: "text"}); err != nil {
		t.Fatal(err)
	}
	jp := state.ParticipantsPath(h.PartDir(), "demo")
	if _, err := state.AppendParticipant(jp, "h1"); err != nil {
		t.Fatal(err)
	}
	if _, err := state.AppendParticipant(jp, "h2"); err != nil {
		t.Fatal(err)
	}

	out, err := cli.Plain(h.sessionWhoRun(context.Background(), args("name", "demo")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "h1") || !strings.Contains(out, "h2") {
		t.Fatalf("who should list both participants: %q", out)
	}
}

func TestSessionWhoEmpty(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	if err := st.AddSession(state.Session{Name: "demo", ChannelID: "c1", Type: "text"}); err != nil {
		t.Fatal(err)
	}
	out, err := cli.Plain(h.sessionWhoRun(context.Background(), args("name", "demo")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Nobody has written in this session yet.") {
		t.Fatalf("empty who should say nobody wrote yet: %q", out)
	}
}
