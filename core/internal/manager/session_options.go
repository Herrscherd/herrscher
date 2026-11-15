package manager

import (
	"slices"
	"strconv"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type budgetCaps struct {
	cost, cohortCost     float64
	tokens, cohortTokens uint64
}

type sessionOptions struct {
	parent, memProject, memAgent string
	projectPinned                bool
	extractor, journal           string
	consolidateEvery             int
	gateways                     []string
	caps                         budgetCaps
}

func checkParentCycle(name, parent string, all []state.Session) error {
	if parent == "" || !slices.Contains(state.TreeChain(all, parent), name) {
		return nil
	}
	return &InvalidValueError{Field: "parent", Value: parent, Hint: "it would make " + name + " its own ancestor"}
}

func (h *Handler) parseSessionOptions(input contracts.Input, terminalOnly bool) (sessionOptions, error) {
	var options sessionOptions
	options.parent, _ = input.Lookup("parent")
	options.memProject, _ = input.Lookup("memory_project")
	if options.memProject != "" && !projectRe.MatchString(options.memProject) {
		return sessionOptions{}, &InvalidValueError{Field: "memory_project", Value: options.memProject, Hint: "use a single name (no /, spaces, or ..)"}
	}
	options.memAgent, _ = input.Lookup("memory_agent")
	options.projectPinned = input.Bool("project_pinned")
	options.extractor, _ = input.Lookup("extractor")
	options.journal, _ = input.Lookup("journal")
	every, err := optionalNonNegative(input, "consolidate_every")
	if err != nil {
		return sessionOptions{}, err
	}
	options.consolidateEvery = every
	gwList, _ := input.Lookup("gateways")
	options.gateways = ParseGateways(gwList, terminalOnly, h.defaultGateways)
	caps, err := parseBudgetCaps(input)
	if err != nil {
		return sessionOptions{}, err
	}
	options.caps = caps
	return options, nil
}

func parseBudgetCaps(input contracts.Input) (budgetCaps, error) {
	var caps budgetCaps
	var err error
	if caps.cost, err = optionalFloat(input, "cost_cap"); err != nil {
		return budgetCaps{}, err
	}
	tokens, err := optionalNonNegative(input, "token_cap")
	if err != nil {
		return budgetCaps{}, err
	}
	caps.tokens = nonNegativeCount(tokens)
	if caps.cohortCost, err = optionalFloat(input, "cohort_cost_cap"); err != nil {
		return budgetCaps{}, err
	}
	cohortTokens, err := optionalNonNegative(input, "cohort_token_cap")
	if err != nil {
		return budgetCaps{}, err
	}
	caps.cohortTokens = nonNegativeCount(cohortTokens)
	return caps, nil
}

func optionalFloat(input contracts.Input, key string) (float64, error) {
	value, ok := input.Lookup(key)
	if !ok || value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, &InvalidValueError{Field: key, Value: value}
	}
	return parsed, nil
}

func optionalNonNegative(input contracts.Input, key string) (int, error) {
	value, ok := input.Lookup(key)
	if !ok || value == "" {
		return 0, nil
	}
	count, err := strconv.Atoi(value)
	if err != nil || count < 0 {
		return 0, &InvalidValueError{Field: key, Value: value, Hint: nonNegativeHint}
	}
	return count, nil
}

const langParam = "lang"

func parseSessionLanguage(input contracts.Input) (string, error) {
	raw, _ := input.Lookup(langParam)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	lang, err := i18n.ParseLanguage(raw)
	if err != nil {
		return "", &InvalidValueError{Field: langParam, Value: raw, Hint: "use en or fr"}
	}
	return lang.String(), nil
}
