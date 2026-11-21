package host

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/approvalsock"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/lease"
	"github.com/Herrscherd/herrscher/core/language"
)

type refusalText struct {
	code   contracts.ApprovalRefusal
	en, fr string
}

var knownRefusalTexts = []refusalText{
	{
		contracts.RefusalInvalid,
		"herrscher: this approval was refused because its request or its answer was malformed",
		"herrscher : cette approbation a été refusée, sa demande ou sa réponse était mal formée",
	},
	{
		contracts.RefusalTooLarge,
		"herrscher: this approval was refused because a message exceeded its size limit",
		"herrscher : cette approbation a été refusée, un message dépassait sa taille limite",
	},
	{
		contracts.RefusalTooMany,
		"herrscher: this approval was refused because the transport already has its maximum of approvals waiting",
		"herrscher : cette approbation a été refusée, le transport a déjà le maximum d'approbations en attente",
	},
	{
		contracts.RefusalNoAsker,
		"herrscher: this approval was refused because no one was available to answer it",
		"herrscher : cette approbation a été refusée, personne n'était disponible pour y répondre",
	},
	{
		contracts.RefusalCancelled,
		"herrscher: this approval was refused because it was cancelled before anyone answered",
		"herrscher : cette approbation a été refusée, elle a été annulée avant qu'une réponse arrive",
	},
	{
		contracts.RefusalDeadline,
		"herrscher: this approval was refused because its time ran out before anyone answered",
		"herrscher : cette approbation a été refusée, son délai est écoulé avant toute réponse",
	},
	{
		contracts.RefusalStreamClosed,
		"herrscher: this approval was refused because the connection closed before anyone answered",
		"herrscher : cette approbation a été refusée, la connexion s'est fermée avant toute réponse",
	},
	{
		contracts.RefusalSendFailed,
		"herrscher: this approval was refused because its answer could not be sent back",
		"herrscher : cette approbation a été refusée, sa réponse n'a pas pu être renvoyée",
	},
	{
		contracts.RefusalUnclassified,
		"herrscher: this approval was refused by the transport for a cause it cannot name",
		"herrscher : cette approbation a été refusée par le transport, pour une cause qu'il ne sait pas nommer",
	},
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

type hookRun struct {
	out, errOut string
	err         error
}

func TestAHookRefusedThroughTheTransportWithoutAnAskerWritesItsText(t *testing.T) {
	pinEnglish(t)
	t.Setenv(SessionVar, "")
	t.Setenv(TokenVar, "")
	path := privateSocketPath(t)
	t.Setenv(approvalsock.EnvApprovalSocket, path)
	backend, remote := remoteSessionOverMTLS(t)
	ran := make(chan hookRun, 1)
	remote.setDriver(func(_ context.Context, asker contracts.ApprovalAsker) string {
		ran <- runHookOverSocket(path, asker)
		return ""
	})
	relay, ok := lease.Unwrap(backend).(remoteBackend).Backend.(approvalRelayingBackend)
	if !ok {
		t.Fatal("the remote session's backend must relay approvals through the transport proxy")
	}
	if _, err := relay.RespondWithApprovals(context.Background(), contracts.Prompt{Content: "clean up"}, nil); err != nil {
		t.Fatalf("RespondWithApprovals over mTLS: %v", err)
	}
	var run hookRun
	select {
	case run = <-ran:
	default:
		t.Fatal("the remote turn never ran its hook")
	}
	if run.err != nil {
		t.Fatalf("the hook could not deliver its decision: %v", run.err)
	}
	want := refusalTextFor(t, contracts.RefusalNoAsker).en
	decision, reason := hookDecision(t, run.out)
	if decision != "deny" || reason != want {
		t.Fatalf("decision %q reason %q, want deny with %q", decision, reason, want)
	}
	if !strings.Contains(run.errOut, want) {
		t.Fatalf("stderr %q, want the note %q", run.errOut, want)
	}
}

func refusalTextFor(t *testing.T, code contracts.ApprovalRefusal) refusalText {
	t.Helper()
	for _, known := range knownRefusalTexts {
		if known.code == code {
			return known
		}
	}
	t.Fatalf("no catalogue text for the refusal %q", code)
	return refusalText{}
}

func runHookOverSocket(path string, asker contracts.ApprovalAsker) (run hookRun) {
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() {
		served <- approvalsock.Serve(ctx, path, asker, approvalsock.WithErrorHandler(func(error) {}))
	}()
	defer func() {
		cancel()
		if err := <-served; err != nil && run.err == nil {
			run.err = err
		}
	}()
	if err := waitForSocket(path); err != nil {
		return hookRun{err: err}
	}
	var out, errOut bytes.Buffer
	noCommandSocket := func(context.Context, []string) (string, bool, error) { return "", false, nil }
	err := RunPermissionHook(context.Background(), NewMessages(), strings.NewReader(bashPayload), &out, &errOut, testTarget, hookAnswerWait(), noCommandSocket)
	return hookRun{out: out.String(), errOut: errOut.String(), err: err}
}

func waitForSocket(path string) error {
	for range 500 {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(time.Millisecond)
	}
	return fmt.Errorf("approval socket %s never appeared", path)
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

func TestAHookDenialWithoutAnyReasonWritesTheGenericText(t *testing.T) {
	deny := func(_ context.Context, req contracts.ApprovalRequest) contracts.ApprovalAnswer {
		return contracts.ApprovalAnswer{ID: req.ID, Decision: contracts.DecisionDeny}
	}
	t.Run("english", func(t *testing.T) {
		pinEnglish(t)
		t.Setenv(SessionVar, "")
		t.Setenv(TokenVar, "")
		t.Setenv(approvalsock.EnvApprovalSocket, listenApprovals(t, deny))
		assertHookRefusal(t, "en", "herrscher: this tool call was denied, and the approval gave no reason")
	})
	t.Run("french", func(t *testing.T) {
		t.Setenv(language.EnvVar, "fr")
		t.Setenv(SessionVar, "")
		t.Setenv(TokenVar, "")
		t.Setenv(approvalsock.EnvApprovalSocket, listenApprovals(t, deny))
		assertHookRefusal(t, "fr", "herrscher : cet appel d'outil a été refusé, et l'approbation n'a donné aucune raison")
	})
}
