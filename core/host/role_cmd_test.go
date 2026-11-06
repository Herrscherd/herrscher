package host

import (
	"context"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func roleRegistry(t *testing.T) (*cli.Registry, *state.State) {
	t.Helper()
	st, err := state.LoadState(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	reg := &cli.Registry{}
	if err := addRoleCommands(reg, st); err != nil {
		t.Fatal(err)
	}
	return reg, st
}

func TestTheFirstGrantWarnsThatEveryoneElseBecomesAnObserver(t *testing.T) {
	reg, _ := roleRegistry(t)
	out, err := cli.Plain(reg.Dispatch(context.Background(), []string{"role", "grant", "--principal", "chat:1", "--role", "owner"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "observer") {
		t.Fatalf("the first grant said %q, which does not warn about the cliff", out)
	}
	out, err = cli.Plain(reg.Dispatch(context.Background(), []string{"role", "grant", "--principal", "chat:2", "--role", "operator"}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "observer") {
		t.Fatalf("the second grant repeated the warning: %q", out)
	}
}

func TestGrantRefusesARoleNobodyDefines(t *testing.T) {
	reg, _ := roleRegistry(t)
	_, err := reg.Dispatch(context.Background(), []string{"role", "grant", "--principal", "chat:1", "--role", "archduke"})
	if err == nil {
		t.Fatal("an unknown role must be refused at the door")
	}
	if !strings.Contains(err.Error(), "owner") {
		t.Fatalf("refusal %q must name the roles that exist", err)
	}
}

func TestGrantWithNoRoleSaysWhichWordIsMissing(t *testing.T) {
	reg, _ := roleRegistry(t)
	_, err := reg.Dispatch(context.Background(), []string{"role", "grant", "chat:1"})
	if err == nil {
		t.Fatal("a grant with no role must be refused")
	}
	if strings.Contains(err.Error(), `""`) {
		t.Fatalf("refusal %q quotes the absence instead of naming what to type", err)
	}
	if !strings.Contains(err.Error(), "role") || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("refusal %q must ask for a role and list the ones that exist", err)
	}
}

func TestGrantRefusesTheStructuralRoleAndTheStructuralPrincipals(t *testing.T) {
	reg, _ := roleRegistry(t)
	if _, err := reg.Dispatch(context.Background(), []string{"role", "grant", "--principal", "chat:1", "--role", "agent"}); err == nil {
		t.Fatal("agent must not be grantable")
	}
	for _, principal := range []string{"local", "session:revue"} {
		_, err := reg.Dispatch(context.Background(), []string{"role", "grant", "--principal", principal, "--role", "owner"})
		if err == nil {
			t.Fatalf("granting to %q must be refused: the entry point already answers it", principal)
		}
	}
}

func TestGrantTakesItsArgumentsBareOrAsFlags(t *testing.T) {
	reg, st := roleRegistry(t)
	if _, err := reg.Dispatch(context.Background(), []string{"role", "grant", "chat:1", "owner"}); err != nil {
		t.Fatal(err)
	}
	if got := st.RoleAssignments()["chat:1"].Role; got != "owner" {
		t.Fatalf("bare form stored %q, want owner", got)
	}
	if _, err := reg.Dispatch(context.Background(), []string{"role", "grant", "chat:2", "observer", "--label", "revue"}); err != nil {
		t.Fatal(err)
	}
	if got := st.RoleAssignments()["chat:2"]; got.Role != "observer" || got.Label != "revue" {
		t.Fatalf("mixed form stored %+v, want observer/revue", got)
	}
	if _, err := reg.Dispatch(context.Background(), []string{"role", "revoke", "chat:2"}); err != nil {
		t.Fatalf("revoke must take a bare principal: %v", err)
	}
}

func TestGrantStoresTheLabelAndListShowsIt(t *testing.T) {
	reg, st := roleRegistry(t)
	if _, err := reg.Dispatch(context.Background(), []string{"role", "grant", "--principal", "chat:1", "--role", "owner", "--label", "shan"}); err != nil {
		t.Fatal(err)
	}
	if got := st.RoleAssignments()["chat:1"].Label; got != "shan" {
		t.Fatalf("label = %q, want shan", got)
	}
	out, err := cli.Plain(reg.Dispatch(context.Background(), []string{"role", "list"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"chat:1", "owner", "shan"} {
		if !strings.Contains(out, want) {
			t.Fatalf("role list %q must mention %q", out, want)
		}
	}
}

func TestListSaysWhenNothingIsAssigned(t *testing.T) {
	reg, _ := roleRegistry(t)
	out, err := cli.Plain(reg.Dispatch(context.Background(), []string{"role", "list"}))
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Fatal("an empty table must still say what that means")
	}
	if !strings.Contains(out, "observer") {
		t.Fatalf("an empty table must warn what the first grant will do: %q", out)
	}
}

func TestRevokeReportsAPrincipalThatHeldNothing(t *testing.T) {
	reg, _ := roleRegistry(t)
	if _, err := reg.Dispatch(context.Background(), []string{"role", "revoke", "--principal", "chat:9"}); err == nil {
		t.Fatal("revoking what was never granted must say so")
	}
}

func TestShowNamesTheRoleAndWhatItRuns(t *testing.T) {
	reg, _ := roleRegistry(t)
	if _, err := reg.Dispatch(context.Background(), []string{"role", "grant", "--principal", "chat:1", "--role", "observer"}); err != nil {
		t.Fatal(err)
	}
	out, err := cli.Plain(reg.Dispatch(context.Background(), []string{"role", "show", "--principal", "chat:1"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"observer", "session list"} {
		if !strings.Contains(out, want) {
			t.Fatalf("role show %q must mention %q", out, want)
		}
	}
}

func TestRoleGrantHelpListsTheGrantableRolesInEachLanguage(t *testing.T) {
	reg, _ := roleRegistry(t)
	for _, lang := range []i18n.Language{i18n.English, i18n.French} {
		help := roleGrantParamHelp(t, reg, lang, "role")
		if !strings.Contains(help, "observer, operator, owner") {
			t.Fatalf("the role help in %s says %q, without the grantable roles", lang, help)
		}
	}
}

func roleGrantParamHelp(t *testing.T, reg *cli.Registry, lang i18n.Language, name string) string {
	t.Helper()
	for _, spec := range reg.Specs(lang) {
		if strings.Join(spec.Path, " ") != "role grant" {
			continue
		}
		for _, param := range spec.Params {
			if param.Name == name {
				return param.Help
			}
		}
	}
	t.Fatalf("role grant has no %s param in %s", name, lang)
	return ""
}
