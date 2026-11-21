package host

import (
	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
)

const approvalRefusalUnknownKey = "approval.refusal.unknown"

func approvalRefusalKey(refusal contracts.ApprovalRefusal) string {
	switch refusal {
	case contracts.RefusalInvalid:
		return "approval.refusal.invalid"
	case contracts.RefusalTooLarge:
		return "approval.refusal.too_large"
	case contracts.RefusalTooMany:
		return "approval.refusal.too_many"
	case contracts.RefusalNoAsker:
		return "approval.refusal.no_asker"
	case contracts.RefusalCancelled:
		return "approval.refusal.cancelled"
	case contracts.RefusalDeadline:
		return "approval.refusal.deadline"
	case contracts.RefusalStreamClosed:
		return "approval.refusal.stream_closed"
	case contracts.RefusalSendFailed:
		return "approval.refusal.send_failed"
	case contracts.RefusalUnclassified:
		return "approval.refusal.unclassified"
	}
	return approvalRefusalUnknownKey
}

func approvalRefusalText(messages *hostMessages, lang i18n.Language, refusal contracts.ApprovalRefusal) string {
	return messages.text(lang, approvalRefusalKey(refusal), nil)
}
