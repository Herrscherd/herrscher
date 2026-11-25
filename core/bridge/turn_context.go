package bridge

import (
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/contextwindow"
	"github.com/Herrscherd/herrscher/core/skills"
)

const blockSeparator = "\n\n"

type injectedBlock struct {
	part contracts.ContextPart
	text string
}

type turnContext struct {
	blocks []injectedBlock
}

func composeTurnContext(memCtx string, eng *skills.Engine, aff affordances) turnContext {
	return turnContext{blocks: []injectedBlock{
		{part: contracts.ContextMemory, text: memCtx},
		{part: contracts.ContextSkills, text: skillsBlock(eng)},
		{part: contracts.ContextDelegation, text: delegationBlock(aff.roster)},
		{part: contracts.ContextCapabilities, text: capabilitiesBlock(aff.caps)},
		{part: contracts.ContextIdentity, text: identityBlock(aff.user)},
	}}
}

func (c turnContext) String() string {
	texts := make([]string, 0, len(c.blocks))
	for _, block := range c.blocks {
		if block.text != "" {
			texts = append(texts, block.text)
		}
	}
	return strings.Join(texts, blockSeparator)
}

func (c turnContext) sizes() []contracts.InjectedPart {
	parts := make([]contracts.InjectedPart, 0, len(c.blocks))
	for _, block := range c.blocks {
		parts = append(parts, contracts.InjectedPart{Part: block.part, Tokens: contextwindow.EstimateTokens(block.text)})
	}
	return parts
}

func skillsBlock(eng *skills.Engine) string {
	if eng == nil {
		return ""
	}
	parts := make([]string, 0, skillPartsCap)
	if menu := eng.Menu(); menu != "" {
		parts = append(parts, menu)
	}
	if exp := eng.Expansions(); exp != "" {
		parts = append(parts, exp)
	}
	return strings.Join(parts, blockSeparator)
}
