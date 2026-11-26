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
		`herr\scher device list`,
		`herr""scher device list`,
		`her''rscher device list`,
		"herrscher --state-dir /tmp/x device list",
		"herrscher -v role show chat:1",
		"B=herrscher; $B device list",
		"x=herr; y=scher; $x$y device list",
		`x=herr; y=scher; $x$y role show`,
		`eval "herrscher device list"`,
		`eval 'her''rscher device list'`,
		"$(printf herrscher) device list",
		"B=herrscher; $B --state-dir /tmp/x role grant",
		"eval \"$(printf dev)ice list\" herrscher",
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
		"herrscher session send hello",
		"git status",
		"echo device",
		"echo $HOME",
		"cd $HOME && git status",
	} {
		if RequiresHuman(Request{Tool: "Bash", Subject: subject}) {
			t.Fatalf("%q must stay under the rules", subject)
		}
	}
	if RequiresHuman(Request{Tool: "Edit", Subject: "herrscher device list"}) {
		t.Fatal("only shell commands are administered this way")
	}
}
