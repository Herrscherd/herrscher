package forgecreds

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixedNow() time.Time {
	return time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
}

type forgeScript struct {
	mu           sync.Mutex
	codeReply    string
	tokenReplies []string
	forms        []url.Values
	codeStatus   int
}

func writeReply(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	if _, err := w.Write([]byte(body)); err != nil {
		t.Errorf("write reply: %v", err)
	}
}

func newForgeServer(t *testing.T, script *forgeScript) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		script.mu.Lock()
		defer script.mu.Unlock()
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		script.forms = append(script.forms, r.PostForm)
		switch r.URL.Path {
		case deviceCodePath:
			if script.codeStatus != 0 {
				w.WriteHeader(script.codeStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			writeReply(t, w, script.codeReply)
		case grantPath:
			reply := `{"error":"authorization_pending"}`
			if len(script.tokenReplies) > 0 {
				reply = script.tokenReplies[0]
				script.tokenReplies = script.tokenReplies[1:]
			}
			w.Header().Set("Content-Type", "application/json")
			writeReply(t, w, reply)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

const deviceCodeReply = `{"device_code":"dev-123","user_code":"WDJB-MJHT","verification_uri":"https://github.com/login/device","expires_in":900,"interval":1}`

type waitRecorder struct {
	mu     sync.Mutex
	delays []time.Duration
}

func (w *waitRecorder) wait(_ context.Context, delay time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.delays = append(w.delays, delay)
	return nil
}

func (w *waitRecorder) recorded() []time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]time.Duration(nil), w.delays...)
}

func testFlow(client *http.Client, waits *waitRecorder) Flow {
	return Flow{
		http:     client,
		clientID: ClientID,
		wait:     waits.wait,
		now:      fixedNow,
	}
}

func TestBeginReturnsTheCodeToShowTheUser(t *testing.T) {
	script := &forgeScript{codeReply: deviceCodeReply}
	server := newForgeServer(t, script)
	flow := testFlow(server.Client(), &waitRecorder{})

	code, err := flow.Begin(t.Context(), server.URL)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if code.UserCode != "WDJB-MJHT" || code.VerificationURI != "https://github.com/login/device" {
		t.Fatalf("Begin = %+v, want the user code and the verification URL", code)
	}
	if code.Interval != time.Second {
		t.Fatalf("Begin interval = %v, want the one the forge gave", code.Interval)
	}
}

func TestBeginReportsAForgeWithoutTheAppAsAppUnavailable(t *testing.T) {
	script := &forgeScript{codeStatus: http.StatusNotFound}
	server := newForgeServer(t, script)
	flow := testFlow(server.Client(), &waitRecorder{})

	_, err := flow.Begin(t.Context(), server.URL)
	if !errors.Is(err, ErrAppUnavailable) {
		t.Fatalf("Begin on a forge without the app = %v, want ErrAppUnavailable", err)
	}
}

func TestAwaitGrantsTheTokensOnceTheUserAuthorizes(t *testing.T) {
	script := &forgeScript{tokenReplies: []string{
		`{"error":"authorization_pending"}`,
		`{"access_token":"ghu_access","expires_in":28800,"refresh_token":"ghr_refresh","refresh_token_expires_in":15897600}`,
	}}
	server := newForgeServer(t, script)
	waits := &waitRecorder{}
	flow := testFlow(server.Client(), waits)
	code := DeviceCode{DeviceCode: "dev-123", UserCode: "WDJB-MJHT", Interval: time.Second, ExpiresAt: fixedNow().Add(time.Minute)}

	record, err := flow.Await(t.Context(), server.URL, code)
	if err != nil {
		t.Fatalf("Await: %v", err)
	}
	if record.AccessToken != "ghu_access" || record.RefreshToken != "ghr_refresh" {
		t.Fatalf("Await returned %+v, want both tokens", record)
	}
	if want := fixedNow().Add(8 * time.Hour); !record.AccessExpiresAt.Equal(want) {
		t.Fatalf("access expiry = %v, want %v", record.AccessExpiresAt, want)
	}
}

func TestAwaitBacksOffWhenTheForgeAsksForSlowDown(t *testing.T) {
	script := &forgeScript{tokenReplies: []string{
		`{"error":"slow_down"}`,
		`{"access_token":"ghu_access"}`,
	}}
	server := newForgeServer(t, script)
	waits := &waitRecorder{}
	flow := testFlow(server.Client(), waits)
	code := DeviceCode{DeviceCode: "dev-123", Interval: time.Second, ExpiresAt: fixedNow().Add(time.Minute)}

	if _, err := flow.Await(t.Context(), server.URL, code); err != nil {
		t.Fatalf("Await: %v", err)
	}
	want := []time.Duration{time.Second, 6 * time.Second}
	got := waits.recorded()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("waits between polls = %v, want %v", got, want)
	}
}

func TestAwaitStopsWhenTheCodeExpires(t *testing.T) {
	script := &forgeScript{tokenReplies: []string{`{"error":"expired_token"}`}}
	server := newForgeServer(t, script)
	flow := testFlow(server.Client(), &waitRecorder{})
	code := DeviceCode{DeviceCode: "dev-123", Interval: time.Second, ExpiresAt: fixedNow().Add(time.Minute)}

	_, err := flow.Await(t.Context(), server.URL, code)
	if !errors.Is(err, ErrDeviceExpired) {
		t.Fatalf("Await on expired_token = %v, want ErrDeviceExpired", err)
	}
}

func TestAwaitStopsWhenTheUserDenies(t *testing.T) {
	script := &forgeScript{tokenReplies: []string{`{"error":"access_denied"}`}}
	server := newForgeServer(t, script)
	flow := testFlow(server.Client(), &waitRecorder{})
	code := DeviceCode{DeviceCode: "dev-123", Interval: time.Second, ExpiresAt: fixedNow().Add(time.Minute)}

	_, err := flow.Await(t.Context(), server.URL, code)
	if !errors.Is(err, ErrDeviceDenied) {
		t.Fatalf("Await on access_denied = %v, want ErrDeviceDenied", err)
	}
}

func TestAwaitStopsOnAnUnexpectedForgeError(t *testing.T) {
	script := &forgeScript{tokenReplies: []string{`{"error":"unsupported_grant_type"}`}}
	server := newForgeServer(t, script)
	flow := testFlow(server.Client(), &waitRecorder{})
	code := DeviceCode{DeviceCode: "dev-123", Interval: time.Second, ExpiresAt: fixedNow().Add(time.Minute)}

	_, err := flow.Await(t.Context(), server.URL, code)
	if !errors.Is(err, ErrDeviceRejected) {
		t.Fatalf("Await on an unknown error = %v, want ErrDeviceRejected", err)
	}
}

func TestRefreshSendsTheClientIDAndNoSecret(t *testing.T) {
	script := &forgeScript{tokenReplies: []string{`{"access_token":"ghu_new","refresh_token":"ghr_new","expires_in":28800}`}}
	server := newForgeServer(t, script)
	flow := testFlow(server.Client(), &waitRecorder{})
	record := Record{Login: "octo", AccessToken: "ghu_old", RefreshToken: "ghr_old"}

	refreshed, err := flow.Refresh(t.Context(), server.URL, record)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if refreshed.AccessToken != "ghu_new" || refreshed.RefreshToken != "ghr_new" || refreshed.Login != "octo" {
		t.Fatalf("Refresh returned %+v, want the new tokens and the kept login", refreshed)
	}
	script.mu.Lock()
	form := script.forms[0]
	script.mu.Unlock()
	if form.Get("client_secret") != "" {
		t.Fatalf("refresh form carries a client secret")
	}
	if form.Get("grant_type") != refreshGrantType || form.Get("client_id") != ClientID {
		t.Fatalf("refresh form = %v, want grant_type refresh_token and the client id", form)
	}
	if strings.Contains(form.Encode(), "device_code") {
		t.Fatalf("refresh form carries a device code")
	}
}

func TestRefreshReportsATokenTheForgeNoLongerAccepts(t *testing.T) {
	script := &forgeScript{tokenReplies: []string{`{"error":"bad_refresh_token"}`}}
	server := newForgeServer(t, script)
	flow := testFlow(server.Client(), &waitRecorder{})

	_, err := flow.Refresh(t.Context(), server.URL, Record{RefreshToken: "ghr_old"})
	if !errors.Is(err, ErrRefreshRejected) {
		t.Fatalf("Refresh with a revoked token = %v, want ErrRefreshRejected", err)
	}
}
