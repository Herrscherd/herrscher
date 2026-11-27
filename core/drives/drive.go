package drives

import (
	"slices"
	"strings"
)

// AllAgents is the agent name that sets the default access of a drive.
const AllAgents = "*"

// Drive is one drive of the catalogue: its rclone remote, the folders it
// exposes and the access each agent holds. Default is the access of an agent
// the drive does not name, and of a session that runs no agent.
type Drive struct {
	Name    string            `json:"name"`
	Kind    Kind              `json:"kind"`
	Folders []string          `json:"folders,omitempty"`
	Default Access            `json:"default"`
	Agents  map[string]Access `json:"agents,omitempty"`
}

// AccessOf is the access the agent holds on the drive.
func (d Drive) AccessOf(agent string) Access {
	if held, ok := d.Agents[agent]; ok && agent != "" {
		return held
	}
	return d.Default
}

// Grant sets the access of one agent, or the default when agent is AllAgents.
func (d *Drive) Grant(agent string, access Access) {
	if agent == AllAgents {
		d.Default = access
		return
	}
	if d.Agents == nil {
		d.Agents = make(map[string]Access)
	}
	d.Agents[agent] = access
}

// ExposedFolders parses the folders the drive exposes. An empty list exposes the whole remote.
func (d Drive) ExposedFolders() ([]Path, error) {
	folders := make([]Path, 0, len(d.Folders))
	for _, raw := range d.Folders {
		folder, err := ParsePath(raw)
		if err != nil {
			return nil, err
		}
		if !folder.IsRoot() {
			folders = append(folders, folder)
		}
	}
	return folders, nil
}

// Exposes reports whether path sits inside one of the exposed folders.
func (d Drive) Exposes(path Path) (bool, error) {
	folders, err := d.ExposedFolders()
	if err != nil {
		return false, err
	}
	if len(folders) == 0 {
		return true, nil
	}
	return slices.ContainsFunc(folders, path.Within), nil
}

// Target is the rclone location of path, refused outside the exposed folders.
func (d Drive) Target(path Path) (string, error) {
	exposed, err := d.Exposes(path)
	if err != nil {
		return "", err
	}
	if !exposed {
		return "", ErrOutsideFolders
	}
	return d.Name + ":" + path.String(), nil
}

// FolderList renders the exposed folders for a listing.
func (d Drive) FolderList() string {
	if len(d.Folders) == 0 {
		return "/"
	}
	return strings.Join(d.Folders, ", ")
}
