package bridge

import (
	"context"
	"encoding/json"
	"sync"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

type approvalChannel struct {
	send     func(contracts.Event) error
	messages *bridgeMessages
	mu       sync.Mutex
	waiting  map[string]chan contracts.ApprovalAnswer
}

func newApprovalChannel(send func(contracts.Event) error) *approvalChannel {
	return &approvalChannel{send: send, messages: newBridgeMessages(), waiting: make(map[string]chan contracts.ApprovalAnswer)}
}

func (c *approvalChannel) attach(ctx context.Context, lang i18n.Language) context.Context {
	if c == nil {
		return ctx
	}
	return contracts.WithApprovalAsker(ctx, c.asker(lang))
}

func (c *approvalChannel) asker(lang i18n.Language) contracts.ApprovalAsker {
	return func(ctx context.Context, req contracts.ApprovalRequest) contracts.ApprovalAnswer {
		return c.ask(ctx, lang, req)
	}
}

func (c *approvalChannel) ask(ctx context.Context, lang i18n.Language, req contracts.ApprovalRequest) contracts.ApprovalAnswer {
	payload, err := json.Marshal(req)
	if err != nil {
		return c.refusal(lang, req, err)
	}
	answers := c.register(req.ID)
	defer c.unregister(req.ID)
	if err := c.send(contracts.Event{T: eventwire.EventApprovalAsk, Value: string(payload)}); err != nil {
		return c.refusal(lang, req, err)
	}
	select {
	case answer := <-answers:
		return answer
	case <-ctx.Done():
		return c.refusal(lang, req, ctx.Err())
	}
}

func (c *approvalChannel) register(requestID string) chan contracts.ApprovalAnswer {
	answers := make(chan contracts.ApprovalAnswer, 1)
	c.mu.Lock()
	c.waiting[requestID] = answers
	c.mu.Unlock()
	return answers
}

func (c *approvalChannel) unregister(requestID string) {
	c.mu.Lock()
	delete(c.waiting, requestID)
	c.mu.Unlock()
}

func (c *approvalChannel) deliver(event contracts.Event) bool {
	if event.T != eventwire.EventApprovalAnswer {
		return false
	}
	var answer contracts.ApprovalAnswer
	if err := json.Unmarshal([]byte(event.Value), &answer); err != nil {
		logger.Warn("approval answer from the daemon is unreadable", "err", err)
		return true
	}
	c.mu.Lock()
	answers, ok := c.waiting[answer.ID]
	delete(c.waiting, answer.ID)
	c.mu.Unlock()
	if ok {
		answers <- answer
	}
	return true
}

func (c *approvalChannel) refusal(lang i18n.Language, req contracts.ApprovalRequest, cause error) contracts.ApprovalAnswer {
	return contracts.ApprovalAnswer{
		ID:       req.ID,
		Decision: contracts.DecisionDeny,
		Reason:   c.messages.render(lang, "approval.unavailable", map[string]any{detailParam: cause.Error()}),
	}
}
