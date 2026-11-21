package host

import (
	"bytes"
	"context"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/approvalsock"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/language"
)

type refusalText struct {
	code   contracts.ApprovalRefusal
	en, fr string
}

var knownRefusalTexts = []refusalText{
	{contracts.RefusalInvalid,
		"herrscher: this approval was refused because its request or its answer was malformed",
		"herrscher : cette approbation a été refusée, sa demande ou sa réponse était mal formée"},
	{contracts.RefusalTooLarge,
		"herrscher: this approval was refused because a message exceeded its size limit",
		"herrscher : cette approbation a été refusée, un message dépassait sa taille limite"},
	{contracts.RefusalTooMany,
		"herrscher: this approval was refused because the transport already has its maximum of approvals waiting",
		"herrscher : cette approbation a été refusée, le transport a déjà le maximum d'approbations en attente"},
	{contracts.RefusalNoAsker,
		"herrscher: this approval was refused because no one was available to answer it",
		"herrscher : cette approbation a été refusée, personne n'était disponible pour y répondre"},
	{contracts.RefusalCancelled,
		"herrscher: this approval was refused because it was cancelled before anyone answered",
		"herrscher : cette approbation a été refusée, elle a été annulée avant qu'une réponse arrive"},
	{contracts.RefusalDeadline,
		"herrscher: this approval was refused because its time ran out before anyone answered",
		"herrscher : cette approbation a été refusée, son délai est écoulé avant toute réponse"},
	{contracts.RefusalStreamClosed,
		"herrscher: this approval was refused because the connection closed before anyone answered",
		"herrscher : cette approbation a été refusée, la connexion s'est fermée avant toute réponse"},
	{contracts.RefusalSendFailed,
		"herrscher: this approval was refused because its answer could not be sent back",
		"herrscher : cette approbation a été refusée, sa réponse n'a pas pu être renvoyée"},
	{contracts.RefusalUnclassified,
		"herrscher: this approval was refused by the transport for a cause it cannot name",
		"herrscher : cette approbation a été refusée par le transport, pour une cause qu'il ne sait pas nommer"},
}

const (
	unknownRefusalEnglish = "herrscher: this approval was refused by the transport for a reason this version does not know"
	unknownRefusalFrench  = "herrscher : cette approbation a été refusée par le transport, pour une raison que cette version ne connaît pas"
)

func TestAKnownRefusalHasItsTextInEnglishAndFrench(t *testing.T) {
	messages := newHostMessages()
	for _, tc := range knownRefusalTexts {
		if got := approvalRefusalText(messages, i18n.English, tc.code); got != tc.en {
			t.Errorf("%s in english = %q, want %q", tc.code, got, tc.en)
		}
		if got := approvalRefusalText(messages, i18n.French, tc.code); got != tc.fr {
			t.Errorf("%s in french = %q, want %q", tc.code, got, tc.fr)
		}
	}
}

func TestAnUnknownRefusalGivesTheGenericTextInEachLanguage(t *testing.T) {
	messages := newHostMessages()
	martian := contracts.ApprovalRefusal("martian")
	if got := approvalRefusalText(messages, i18n.English, martian); got != unknownRefusalEnglish {
		t.Errorf("unknown in english = %q, want %q", got, unknownRefusalEnglish)
	}
	if got := approvalRefusalText(messages, i18n.French, martian); got != unknownRefusalFrench {
		t.Errorf("unknown in french = %q, want %q", got, unknownRefusalFrench)
	}
}

func TestAHookRefusedByTheTransportWritesItsTextInFrench(t *testing.T) {
	t.Setenv(language.EnvVar, "fr")
	t.Setenv(SessionVar, "")
	t.Setenv(TokenVar, "")
	path := listenApprovals(t, func(_ context.Context, req contracts.ApprovalRequest) contracts.ApprovalAnswer {
		answer, err := contracts.RefusedAnswer(req.ID, contracts.RefusalTooMany)
		if err != nil {
			t.Errorf("RefusedAnswer: %v", err)
		}
		return answer
	})
	t.Setenv(approvalsock.EnvApprovalSocket, path)
	assertHookRefusal(t, "fr", knownRefusalTexts[2].fr)
}

func TestAHookRefusedByTheTransportWritesItsTextInEnglish(t *testing.T) {
	pinEnglish(t)
	t.Setenv(SessionVar, "")
	t.Setenv(TokenVar, "")
	path := listenApprovals(t, func(_ context.Context, req contracts.ApprovalRequest) contracts.ApprovalAnswer {
		answer, err := contracts.RefusedAnswer(req.ID, contracts.RefusalDeadline)
		if err != nil {
			t.Errorf("RefusedAnswer: %v", err)
		}
		return answer
	})
	t.Setenv(approvalsock.EnvApprovalSocket, path)
	assertHookRefusal(t, "en", knownRefusalTexts[5].en)
}

func TestAHookRefusedWithAnUnknownCodeWritesTheGenericText(t *testing.T) {
	pinEnglish(t)
	t.Setenv(SessionVar, "")
	t.Setenv(TokenVar, "")
	path := listenApprovals(t, func(_ context.Context, req contracts.ApprovalRequest) contracts.ApprovalAnswer {
		return contracts.ApprovalAnswer{ID: req.ID, Decision: contracts.DecisionDeny, Refusal: "martian"}
	})
	t.Setenv(approvalsock.EnvApprovalSocket, path)
	assertHookRefusal(t, "en", unknownRefusalEnglish)
}

func TestADenialWithoutACodeKeepsItsReason(t *testing.T) {
	pinEnglish(t)
	t.Setenv(SessionVar, "")
	t.Setenv(TokenVar, "")
	path := listenApprovals(t, func(_ context.Context, req contracts.ApprovalRequest) contracts.ApprovalAnswer {
		return contracts.ApprovalAnswer{ID: req.ID, Decision: contracts.DecisionDeny, Reason: "use make clean"}
	})
	t.Setenv(approvalsock.EnvApprovalSocket, path)
	if errOut := assertHookDenial(t, "en", "use make clean"); errOut != "" {
		t.Fatalf("stderr %q, want none for a denial without a code", errOut)
	}
}

func assertHookRefusal(t *testing.T, lang, want string) {
	t.Helper()
	errOut := assertHookDenial(t, lang, want)
	if !strings.Contains(errOut, want) {
		t.Fatalf("stderr %q, want the note %q (%s)", errOut, want, lang)
	}
}

func assertHookDenial(t *testing.T, lang, want string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	mustRunHook(context.Background(), t, strings.NewReader(bashPayload), &out, &errOut, testTarget, forbidDaemon(t))
	decision, reason := hookDecision(t, out.String())
	if decision != "deny" {
		t.Fatalf("decision %q, want deny (%s)", decision, lang)
	}
	if reason != want {
		t.Fatalf("reason %q, want %q (%s)", reason, want, lang)
	}
	return errOut.String()
}
