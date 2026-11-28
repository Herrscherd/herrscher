package bridge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/testsupport/stagingtest"
)

func TestProjectionSweepsAStaleStagingFileBesideASkill(t *testing.T) {
	cwd := t.TempDir()
	skillDir := filepath.Join(learnedRoot(cwd), "retry-http")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(skillDir, ".SKILL.md.4242540")
	fresh := filepath.Join(skillDir, ".SKILL.md.4242541")
	stagingtest.Plant(t, stale, time.Hour)
	stagingtest.Plant(t, fresh, time.Second)
	orch := &learnedOrch{nodes: []contracts.Node{learnedNode("agents/a/skills/retry-http", "wait out a 429", "Read Retry-After.")}}

	ProjectLearnedSkills(context.Background(), orch, cwd)

	stagingtest.Absent(t, stale)
	stagingtest.Present(t, fresh)
}
