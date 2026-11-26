package approval

import "testing"

func TestRequiresHumanCatchesEveryWayOfNamingHerrscherAdministration(t *testing.T) {
	for _, subject := range []string{
		"herrscher device approve ABCDE-FGHJK",
		"herrscher device list",
		"herrscher role grant --principal chat:1 --role owner",
		"cd /srv && herrscher device pair",
		"/usr/bin/herrscher  device revoke 0123",
		`"herrscher" device list`,
		"env HERRSCHER_X=1 ./herrscher role show chat:1",
	} {
		if !RequiresHuman(Request{Tool: "Bash", Subject: subject}) {
			t.Fatalf("%q must need a person", subject)
		}
	}
}

func TestRequiresHumanLeavesOrdinaryCommandsToTheRules(t *testing.T) {
	for _, subject := range []string{
		"herrscher session list",
		"herrscher approve list",
		"git status",
		"echo device",
	} {
		if RequiresHuman(Request{Tool: "Bash", Subject: subject}) {
			t.Fatalf("%q must stay under the rules", subject)
		}
	}
	if RequiresHuman(Request{Tool: "Edit", Subject: "herrscher device list"}) {
		t.Fatal("only shell commands are administered this way")
	}
}
