package forgecreds

import (
	"testing"
)

func TestConnectSavesTheGrantedTokensAndShowsOnlyTheCode(t *testing.T) {
	script := &forgeScript{
		codeReply: deviceCodeReply,
		tokenReplies: []string{
			`{"error":"authorization_pending"}`,
			`{"access_token":"ghu_granted","refresh_token":"ghr_granted","expires_in":28800}`,
		},
	}
	server := newForgeServer(t, script)
	setup := newTestCredentials(t, server.URL)
	host := mustHost(t, "github.com")
	var shown DeviceCode

	err := setup.creds.Connect(t.Context(), host, func(code DeviceCode) error {
		shown = code
		return nil
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if shown.UserCode != "WDJB-MJHT" {
		t.Fatalf("shown code = %+v, want the user code", shown)
	}
	record, found, err := setup.creds.Store().Get(host)
	if err != nil || !found || record.AccessToken != "ghu_granted" {
		t.Fatalf("saved record = %+v, found %v, err %v; want the granted token", record, found, err)
	}
}
