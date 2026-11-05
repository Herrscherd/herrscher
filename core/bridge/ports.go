package bridge

import (
	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/lease"
)

func compactorOf(backend contracts.Backend) (contracts.Compactor, bool) {
	compactor, ok := lease.Unwrap(backend).(contracts.Compactor)
	return compactor, ok
}

func resumeAwareOf(backend contracts.Backend) (contracts.ResumeAware, bool) {
	resumer, ok := lease.Unwrap(backend).(contracts.ResumeAware)
	return resumer, ok
}

func choiceInjectorOf(backend contracts.Backend) (contracts.ChoiceInjector, bool) {
	injector, ok := lease.Unwrap(backend).(contracts.ChoiceInjector)
	return injector, ok
}

func skillNativeOf(backend contracts.Backend) (contracts.SkillNative, bool) {
	native, ok := lease.Unwrap(backend).(contracts.SkillNative)
	return native, ok
}
