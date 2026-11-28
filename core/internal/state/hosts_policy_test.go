package state

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFullPolicyReadsTheTwoSettingsAndDefaultsToRefuse(t *testing.T) {
	cases := map[string]FullPolicy{"": FullRefuse, "refuse": FullRefuse, "wait": FullWait}
	for text, want := range cases {
		got, err := ParseFullPolicy(text)
		if err != nil || got != want {
			t.Fatalf("%q: want %q, got %q (%v)", text, want, got, err)
		}
	}
	if _, err := ParseFullPolicy("queue"); !errors.Is(err, ErrUnknownFullPolicy) {
		t.Fatalf("an unknown policy must be refused, got %v", err)
	}
}

func TestKnownHostsFileIsPerHostAndBesideTheState(t *testing.T) {
	dir := t.TempDir()
	st := NewState(filepath.Join(dir, "state.json"))
	build := st.KnownHostsFile("build1")
	if filepath.Dir(filepath.Dir(build)) != dir {
		t.Fatalf("the pinned file must sit under the state directory, got %q", build)
	}
	if strings.Contains(build, "build1") {
		t.Fatalf("the host name must not reach the file name, got %q", build)
	}
	if st.KnownHostsFile("build1") != build {
		t.Fatal("the same host must always name the same file")
	}
	if st.KnownHostsFile("build2") == build {
		t.Fatal("two hosts must never share a pinned file")
	}
}

func TestAHostRecordWrittenBeforePinningStillLoads(t *testing.T) {
	var host Host
	legacy := `{"name":"build1","ssh":"me@build1","workspace":"/srv/work","bin":"/home/me/.herrscher/bin/herrscher","version":"abc1234"}`
	if err := json.Unmarshal([]byte(legacy), &host); err != nil {
		t.Fatal(err)
	}
	if host.HostKey != "" || host.KnownHosts != "" || host.BinSHA256 != "" || host.KeyChanged != "" {
		t.Fatalf("a record from before pinning must carry no pin, got %+v", host)
	}
	if len(host.Projects) != 0 || host.MaxSessions != 0 || host.WhenFull != "" {
		t.Fatalf("a record from before the limits must keep the unlimited behaviour, got %+v", host)
	}
}

func TestSessionsOnCountsOnlyLiveSessionsOfTheHost(t *testing.T) {
	st := NewState(filepath.Join(t.TempDir(), "state.json"))
	for _, sess := range []Session{
		{Name: "a", Host: "build1"},
		{Name: "b", Host: "build1", Archived: true},
		{Name: "c", Host: "build2"},
		{Name: "d", Host: ""},
	} {
		if err := st.AddSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	if got := st.SessionsOn("build1"); got != 1 {
		t.Fatalf("only the live session of build1 holds a slot, got %d", got)
	}
}
