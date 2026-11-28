package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const skillsBudgetFile = "skills-budget"

var (
	errNegativeBudget = errors.New("token caps must not be negative")
	errTrailingData   = errors.New("data after the budget object")
)

// SkillsBudget caps the skill text of an agent's sessions each turn, in estimated
// tokens, from <home>/skills-budget. A zero field leaves that block unbounded, and
// a missing file leaves both unbounded.
type SkillsBudget struct {
	MenuTokens      int `json:"menuTokens"`
	ExpansionTokens int `json:"expansionTokens"`
}

// SkillsBudgetError reports a skills-budget file that cannot be read or does not
// parse. It names the agent so the operator knows which file to fix.
type SkillsBudgetError struct {
	Name string
	Err  error
}

func (e *SkillsBudgetError) Error() string {
	return fmt.Sprintf("agent %s skills budget: %v", e.Name, e.Err)
}

func (e *SkillsBudgetError) Unwrap() error { return e.Err }

// SkillsBudget reads the skills budget of the named agent. It returns the zero
// budget when the agent has no skills-budget file, and a *SkillsBudgetError when
// the file exists but is unreadable, has unknown fields, trailing data or a
// negative cap.
func (s *Store) SkillsBudget(name string) (SkillsBudget, error) {
	if !validateName(name) {
		return SkillsBudget{}, &InvalidNameError{Name: name}
	}
	data, err := os.ReadFile(filepath.Join(s.root, name, skillsBudgetFile))
	if errors.Is(err, fs.ErrNotExist) {
		return SkillsBudget{}, nil
	}
	if err != nil {
		return SkillsBudget{}, &SkillsBudgetError{Name: name, Err: err}
	}
	budget, err := parseSkillsBudget(data)
	if err != nil {
		return SkillsBudget{}, &SkillsBudgetError{Name: name, Err: err}
	}
	return budget, nil
}

func parseSkillsBudget(data []byte) (SkillsBudget, error) {
	var budget SkillsBudget
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&budget); err != nil {
		return SkillsBudget{}, err
	}
	trailing := decoder.Decode(new(struct{}))
	if !errors.Is(trailing, io.EOF) {
		return SkillsBudget{}, errTrailingData
	}
	if budget.MenuTokens < 0 || budget.ExpansionTokens < 0 {
		return SkillsBudget{}, errNegativeBudget
	}
	return budget, nil
}
