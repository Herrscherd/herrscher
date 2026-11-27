package approval

import "testing"

func TestRequiresHumanCatchesEveryWayOfNamingHerrscherAdministration(t *testing.T) {
	for _, subject := range []string{
		"herrscher device approve ABCDE-FGHJK",
		"/bin/herr*  device list",
		"/bin/herr[s]cher device list",
		"herrscher device list",
		"herrscher role grant chat:1 owner",
		"cd /srv && herrscher device pair",
		"/usr/bin/herrscher  device revoke 0123",
		"./herrscher role show chat:1",
		`"herrscher" device list`,
		`herr\scher device list`,
		`herr""scher device list`,
		`her''rscher device list`,
		"herrscher session send please check the device",
		"B=herrscher; $B device list",
		"x=herr; y=scher; $x$y device list",
		"x=her; y=rscher; $x$y role show",
		`eval "herrscher device list"`,
		`eval 'her''rscher device list'`,
		"$(printf herrscher) device list",
		"echo role grant bob owner | xargs herrscher",
		`printf 'role\ngrant\n' | xargs -d '\n' herrscher`,
		"echo role grant bob owner | xargs -I{} sh -c 'herrscher {}'",
		"echo cm9sZSBncmFudCBib2Igb3duZXI= | base64 -d | sh",
		`printf '\x68\x65\x72\x72' | sh`,
		`python3 -c 'import subprocess; subprocess.run(["herrscher", "device", "approve", "X"])'`,
		`node -e 'require("child_process").execFileSync("herrscher", ["device", "list"])'`,
		`perl -e 'system("herrscher", "device", "list")'`,
		`grep "herrscher role" docs/remote.md`,
		"if true; then herrscher device list; fi",
		"for x in 1; do herrscher device approve X; done",
		"! herrscher device list",
		"echo hi\nherrscher device list",
		"sh <<EOF\nherrscher device list\nEOF",
		`echo "herrscher device list" | sh`,
		"timeout 5 herrscher device list",
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
		"grep role $LOG",
		"ls $DIR && cat devices.txt",
		`echo "$HOME/device"`,
		"echo $(date) > device.log",
		"echo $X herring fischer",
		"echo ls | sh",
		"echo cm9sZQ== | base64 -d",
	} {
		if RequiresHuman(Request{Tool: "Bash", Subject: subject}) {
			t.Fatalf("%q must stay under the rules", subject)
		}
	}
	if RequiresHuman(Request{Tool: "Edit", Subject: "herrscher device list"}) {
		t.Fatal("only shell commands are administered this way")
	}
}
