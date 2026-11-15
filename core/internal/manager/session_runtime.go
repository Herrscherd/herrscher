package manager

import (
	"fmt"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type runtimeChoice struct {
	cmd      string
	explicit bool
	backend  string
	agent    string
	vendor   string
	model    string
}

func (h *Handler) checkSessionAccount(input contracts.Input, runtime runtimeChoice) (string, string, error) {
	explicitAccount, _ := input.Lookup("account")
	hostName := h.resolveHost(input, runtime.agent)
	account := chooseSessionAccount(explicitAccount, h.agentAccount(runtime.agent))
	err := h.checkAccountUsable(hostName, account)
	if err == nil {
		err = h.checkAccount(runtime, account)
	}
	if err != nil {
		if account != explicitAccount {
			err = localize(h.currentLanguage(), fmt.Sprintf("the account of agent %q: %v", runtime.agent, err), clitext.HostErrAccountOfAgent, clitext.Params{clitext.ParamAgent: runtime.agent, clitext.ParamErr: err.Error()}, err)
		}
		return "", "", err
	}
	return hostName, account, nil
}

// PreflightAccount reports the account refusal that a session create with these arguments would meet, without creating anything.
func (h *Handler) PreflightAccount(input contracts.Input) error {
	runtime, err := h.chooseRuntime(input)
	if err != nil {
		return err
	}
	_, _, err = h.checkSessionAccount(input, runtime)
	return err
}

func (h *Handler) chooseRuntime(input contracts.Input) (runtimeChoice, error) {
	var runtime runtimeChoice
	runtime.cmd = h.defaultCmd
	if c, ok := input.Lookup("cmd"); ok && c != "" {
		runtime.cmd = c
		runtime.explicit = true
	}
	runtime.backend, _ = input.Lookup("backend")
	if runtime.backend == "" {
		runtime.backend = "stream"
	}
	runtime.agent, _ = input.Lookup("agent")
	runtime.vendor, _ = input.Lookup("vendor")
	model, _ := input.Lookup("model")
	agentModel, agentBackend := h.agentDefaults(runtime.agent)
	fallbackVendor := runtime.vendor
	if fallbackVendor == "" {
		fallbackVendor = agentBackend
	}
	runtime.model = h.resolveModel(model, fallbackVendor, agentModel, runtime.explicit)
	if err := h.checkModel(runtime.vendor, runtime.model); err != nil {
		return runtimeChoice{}, err
	}
	if err := h.checkSpawnSource(runtime.explicit, runtime.model); err != nil {
		return runtimeChoice{}, err
	}
	return runtime, nil
}

func (h *Handler) vendorOf(runtime runtimeChoice) string {
	if runtime.vendor != "" {
		return runtime.vendor
	}
	if agentSpec, found := h.agents.Get(runtime.agent); found {
		return agentSpec.Backend
	}
	return ""
}

func chooseSessionAccount(explicit, agentAccount string) string {
	if explicit != "" {
		return explicit
	}
	return agentAccount
}

func (h *Handler) agentAccount(agentName string) string {
	if agentName == "" {
		return ""
	}
	if a, ok := h.agents.Get(agentName); ok {
		return a.Account
	}
	return ""
}

func (h *Handler) resolveHost(input contracts.Input, agentName string) string {
	hostName, _ := input.Lookup("host")
	hostName = strings.TrimSpace(hostName)
	if hostName == "" && agentName != "" {
		if a, ok := h.agents.Get(agentName); ok {
			hostName = a.Host
		}
	}
	if hostName == state.LocalHost {
		return ""
	}
	return hostName
}

func (h *Handler) checkHostReady(hostName string) error {
	if hostName == "" || h.hs == nil {
		return nil
	}
	return h.hs.Ready(hostName)
}
