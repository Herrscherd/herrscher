package main

import (
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

const (
	defaultModel    = "claude-haiku-5-5"
	defaultBinary   = "herrscher"
	defaultSamples  = 10
	defaultMaxTurns = 6
	defaultSeed     = 1
	minMaxTurns     = 3
	float64Bits     = 64
	formatMarkdown  = "md"
	formatJSON      = "json"
	settingsAll     = "all"
)

const (
	nameHerrscher    = "herrscher"
	nameModel        = "model"
	nameBudgetTokens = "budget-tokens"
	nameBudgetUSD    = "budget-usd"
	nameCredentials  = "credentials"
	nameAPIKeyEnv    = "api-key-env"
	nameDataset      = "dataset"
	nameSamples      = "samples"
	nameSeed         = "seed"
	nameSettings     = "settings"
	nameMaxTurns     = "max-turns"
	nameFormat       = "format"
	nameLang         = "lang"
)

var errUsageRequested = errors.New("membench: usage requested")

type messageError struct {
	key    msgKey
	params params
}

func (e messageError) Error() string {
	return string(e.key)
}

type config struct {
	binary      string
	model       string
	budget      budget
	credentials string
	apiKey      string
	dataset     string
	samples     int
	seed        uint64
	settings    []setting
	maxTurns    int
	format      string
	lang        i18n.Language
}

type flagSpec struct {
	name string
	key  msgKey
}

func flagCatalogue() []flagSpec {
	return []flagSpec{
		{nameHerrscher, msgFlagHerrscher},
		{nameModel, msgFlagModel},
		{nameBudgetTokens, msgFlagBudgetTokens},
		{nameBudgetUSD, msgFlagBudgetUSD},
		{nameCredentials, msgFlagCredentials},
		{nameAPIKeyEnv, msgFlagAPIKeyEnv},
		{nameDataset, msgFlagDataset},
		{nameSamples, msgFlagSamples},
		{nameSeed, msgFlagSeed},
		{nameSettings, msgFlagSettings},
		{nameMaxTurns, msgFlagMaxTurns},
		{nameFormat, msgFlagFormat},
		{nameLang, msgFlagLang},
	}
}

func parseConfig(args []string, stderr io.Writer, msgs catalogue, defaultLang i18n.Language, getenv func(string) string) (config, error) {
	var raw rawFlags
	flags := flag.NewFlagSet("membench", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	raw.bind(flags)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			if usageErr := writeUsage(stderr, msgs, defaultLang); usageErr != nil {
				return config{}, usageErr
			}
			return config{}, errUsageRequested
		}
		return config{}, messageError{key: msgErrFlagValue, params: params{paramName: "flags", paramDetail: err.Error()}}
	}
	return raw.resolve(defaultLang, getenv)
}

type rawFlags struct {
	binary, model, credentials, apiKeyEnv, dataset, settings, format, lang string
	budgetTokens                                                           int64
	budgetUSD                                                              float64
	samples, maxTurns                                                      int
	seed                                                                   uint64
}

func (r *rawFlags) bind(flags *flag.FlagSet) {
	flags.StringVar(&r.binary, nameHerrscher, defaultBinary, "")
	flags.StringVar(&r.model, nameModel, defaultModel, "")
	flags.Int64Var(&r.budgetTokens, nameBudgetTokens, 0, "")
	flags.Float64Var(&r.budgetUSD, nameBudgetUSD, 0, "")
	flags.StringVar(&r.credentials, nameCredentials, "", "")
	flags.StringVar(&r.apiKeyEnv, nameAPIKeyEnv, "", "")
	flags.StringVar(&r.dataset, nameDataset, "", "")
	flags.IntVar(&r.samples, nameSamples, defaultSamples, "")
	flags.Uint64Var(&r.seed, nameSeed, defaultSeed, "")
	flags.StringVar(&r.settings, nameSettings, settingsAll, "")
	flags.IntVar(&r.maxTurns, nameMaxTurns, defaultMaxTurns, "")
	flags.StringVar(&r.format, nameFormat, formatMarkdown, "")
	flags.StringVar(&r.lang, nameLang, "", "")
}

func (r *rawFlags) resolve(defaultLang i18n.Language, getenv func(string) string) (config, error) {
	lang := defaultLang
	if r.lang != "" {
		parsed, err := i18n.ParseLanguage(r.lang)
		if err != nil {
			return config{}, flagError(nameLang, r.lang)
		}
		lang = parsed
	}
	cfg := config{
		model:       r.model,
		budget:      budget{tokens: r.budgetTokens, usd: r.budgetUSD},
		credentials: r.credentials,
		dataset:     r.dataset,
		samples:     r.samples,
		seed:        r.seed,
		maxTurns:    r.maxTurns,
		format:      r.format,
		lang:        lang,
	}
	if err := r.validate(&cfg); err != nil {
		return config{}, err
	}
	if err := cfg.readAPIKey(r.apiKeyEnv, getenv); err != nil {
		return config{}, err
	}
	binary, err := exec.LookPath(r.binary)
	if err != nil {
		return config{}, messageError{key: msgErrHerrscher, params: params{paramPath: r.binary, paramErr: err.Error()}}
	}
	cfg.binary = binary
	return cfg, nil
}

func (c *config) readAPIKey(name string, getenv func(string) string) error {
	if name == "" {
		return nil
	}
	value := getenv(name)
	if value == "" {
		return flagError(nameAPIKeyEnv, name)
	}
	c.apiKey = value
	return nil
}

func (r *rawFlags) validate(cfg *config) error {
	if cfg.budget.tokens < 0 {
		return flagError(nameBudgetTokens, strconv.FormatInt(cfg.budget.tokens, 10))
	}
	if cfg.budget.usd < 0 {
		return flagError(nameBudgetUSD, strconv.FormatFloat(cfg.budget.usd, 'f', -1, float64Bits))
	}
	if cfg.budget.tokens == 0 && cfg.budget.usd == 0 {
		return messageError{key: msgErrBudgetMissing}
	}
	if cfg.maxTurns < minMaxTurns {
		return flagError(nameMaxTurns, strconv.Itoa(cfg.maxTurns))
	}
	if cfg.samples < 0 {
		return flagError(nameSamples, strconv.Itoa(cfg.samples))
	}
	if cfg.format != formatMarkdown && cfg.format != formatJSON {
		return flagError(nameFormat, cfg.format)
	}
	if cfg.credentials != "" {
		if _, err := os.Stat(cfg.credentials); err != nil {
			return messageError{key: msgErrFlagValue, params: params{paramName: nameCredentials, paramDetail: err.Error()}}
		}
	}
	settings, err := selectSettings(r.settings)
	if err != nil {
		return err
	}
	cfg.settings = settings
	return nil
}

func flagError(name, detail string) error {
	return messageError{key: msgErrFlagValue, params: params{paramName: name, paramDetail: detail}}
}

func writeUsage(out io.Writer, msgs catalogue, lang i18n.Language) error {
	translator := msgs.For(lang)
	var text strings.Builder
	text.WriteString(translator.Text(msgUsage, nil) + "\n")
	for _, spec := range flagCatalogue() {
		text.WriteString("  -" + spec.name + "\n    " + translator.Text(spec.key, nil) + "\n")
	}
	if err := translator.Err(); err != nil {
		return err
	}
	_, err := io.WriteString(out, text.String())
	return err
}
