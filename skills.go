package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Herrscherd/herrscher/core/skills"
)

// shippedSkills are the playbooks that travel with the binary. Shipping them in
// the repo was not enough: the backend loads skills from the workspace and from
// ~/.claude/skills, neither of which is this source tree, so an agent told to
// follow "pr-job" reported the skill did not exist and improvised.
//
//go:embed skills
var shippedSkills embed.FS

// installShippedSkills materializes them where every backend looks for personal
// skills, once. Best effort on purpose: a daemon that cannot write there must
// still start — it loses a playbook, not its ability to answer.
func installShippedSkills() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	src, err := fs.Sub(shippedSkills, "skills")
	if err != nil {
		return
	}
	made, err := skills.Install(src, filepath.Join(home, ".claude", "skills"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "herrscher: install shipped skills: "+err.Error())
	}
	for _, name := range made {
		fmt.Fprintln(os.Stderr, "herrscher: installed skill "+name)
	}
}
