package agent

import (
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/testsupport/stagingtest"
)

func TestSetReplySweepsAStaleStagingFileOfTheRule(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.Create(CreateSpec{Name: "rin"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	home := filepath.Join(s.root, "rin")
	stale := filepath.Join(home, "."+replyFile+".4242560")
	fresh := filepath.Join(home, "."+replyFile+".4242561")
	stagingtest.Plant(t, stale, time.Hour)
	stagingtest.Plant(t, fresh, time.Second)

	if err := s.SetReply("rin", contracts.ReplyInChannels); err != nil {
		t.Fatalf("SetReply: %v", err)
	}

	stagingtest.Absent(t, stale)
	stagingtest.Present(t, fresh)
}

func TestSetReplyWithChannelsSweepsTheStagingFilesOfBothFiles(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.Create(CreateSpec{Name: "rin"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	home := filepath.Join(s.root, "rin")
	staleChannels := filepath.Join(home, "."+channelsFile+".4242562")
	staleReply := filepath.Join(home, "."+replyFile+".4242563")
	fresh := filepath.Join(home, "."+channelsFile+".4242564")
	stagingtest.Plant(t, staleChannels, time.Hour)
	stagingtest.Plant(t, staleReply, time.Hour)
	stagingtest.Plant(t, fresh, time.Second)

	if err := s.SetReplyWithChannels("rin", contracts.ReplyInChannels, nil); err != nil {
		t.Fatalf("SetReplyWithChannels: %v", err)
	}

	stagingtest.Absent(t, staleChannels)
	stagingtest.Absent(t, staleReply)
	stagingtest.Present(t, fresh)
}
