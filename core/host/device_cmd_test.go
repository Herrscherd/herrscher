package host

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/devices"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

var pairingCodePattern = regexp.MustCompile(`[ABCDEFGHJKLMNPQRSTUVWXYZ23456789]{5}-[ABCDEFGHJKLMNPQRSTUVWXYZ23456789]{5}`)

func deviceRegistry(t *testing.T) (*cli.Registry, *state.State) {
	t.Helper()
	st, err := state.LoadState(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	reg := cli.NewRegistry(clitext.NewMessages())
	if err := addDeviceCommands(reg, clitext.NewMessages(), fixedLanguage(i18n.English), st); err != nil {
		t.Fatal(err)
	}
	return reg, st
}

func dispatchDevice(t *testing.T, reg *cli.Registry, args ...string) (string, error) {
	t.Helper()
	return cli.Plain(reg.Dispatch(context.Background(), args))
}

func pairCode(t *testing.T, reg *cli.Registry, args ...string) string {
	t.Helper()
	out, err := dispatchDevice(t, reg, append([]string{"device", "pair"}, args...)...)
	if err != nil {
		t.Fatalf("device pair: %v", err)
	}
	code := pairingCodePattern.FindString(out)
	if code == "" {
		t.Fatalf("device pair printed no code: %q", out)
	}
	return code
}

func redeemWith(t *testing.T, st *state.State, code string) string {
	t.Helper()
	parsed, err := devices.ParsePairingCode(code)
	if err != nil {
		t.Fatal(err)
	}
	csr := phoneRequest(t)
	if err := st.UpdateDevices(func(registry *devices.Registry) error {
		return registry.Redeem(time.Now(), parsed, csr)
	}); err != nil {
		t.Fatalf("redeem: %v", err)
	}
	return devices.ShortFingerprint(csr.RawSubjectPublicKeyInfo)
}

func phoneRequest(t *testing.T) *x509.CertificateRequest {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "phone"}}, key)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil {
		t.Fatal(err)
	}
	return csr
}

func wrongCodeLike(code string) string {
	if strings.HasPrefix(code, "A") {
		return "B" + code[1:]
	}
	return "A" + code[1:]
}

func wrongFingerprintLike(fingerprint string) string {
	if strings.HasPrefix(fingerprint, "A") {
		return "B" + fingerprint[1:]
	}
	return "A" + fingerprint[1:]
}

func devicesDirExists(st *state.State) bool {
	_, err := os.Stat(filepath.Join(st.PrivateDir(), "devices"))
	return !errors.Is(err, fs.ErrNotExist)
}

func TestPairPrintsACodeThatGrantsApproverByDefault(t *testing.T) {
	reg, _ := deviceRegistry(t)
	out, err := dispatchDevice(t, reg, "device", "pair")
	if err != nil {
		t.Fatal(err)
	}
	if pairingCodePattern.FindString(out) == "" {
		t.Fatalf("pair printed no code: %q", out)
	}
	if !strings.Contains(out, "approver") {
		t.Fatalf("pair must say the default role is approver: %q", out)
	}
}

func TestASecondPairingWindowIsRefused(t *testing.T) {
	reg, _ := deviceRegistry(t)
	pairCode(t, reg, "device", "pair")
	if _, err := dispatchDevice(t, reg, "device", "pair"); !errors.Is(err, devices.ErrPairingOpen) {
		t.Fatalf("second window = %v, want ErrPairingOpen", err)
	}
}

func TestPairRefusesARoleThatIsNotGrantable(t *testing.T) {
	reg, _ := deviceRegistry(t)
	if _, err := dispatchDevice(t, reg, "device", "pair", "--role", "agent"); !errors.Is(err, devices.ErrUngrantableRole) {
		t.Fatalf("pair --role agent = %v, want ErrUngrantableRole", err)
	}
}

func TestApproveWithoutAFingerprintOnlyShowsTheRequestAndSignsNothing(t *testing.T) {
	reg, st := deviceRegistry(t)
	code := pairCode(t, reg, "device", "pair", "--name", "telephone", "--role", "observer")
	fingerprint := redeemWith(t, st, code)
	out, err := dispatchDevice(t, reg, "device", "approve", code)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !strings.Contains(out, fingerprint) || !strings.Contains(out, "telephone") {
		t.Fatalf("the preview must show the name and the fingerprint the device shows: %q", out)
	}
	if len(st.DeviceRegistry().Devices) != 0 || devicesDirExists(st) {
		t.Fatal("a preview must not sign, nor create the authority")
	}
	if st.DeviceRegistry().Pairing == nil || st.DeviceRegistry().Pairing.Pending == nil {
		t.Fatal("a preview must leave the request waiting")
	}
}

func TestAWrongFingerprintSignsNothingAndCountsAsAnAttempt(t *testing.T) {
	reg, st := deviceRegistry(t)
	code := pairCode(t, reg, "device", "pair")
	fingerprint := redeemWith(t, st, code)
	_, err := dispatchDevice(t, reg, "device", "approve", code, "--fingerprint", wrongFingerprintLike(fingerprint))
	if !errors.Is(err, devices.ErrFingerprintMismatch) {
		t.Fatalf("wrong fingerprint = %v, want ErrFingerprintMismatch", err)
	}
	if len(st.DeviceRegistry().Devices) != 0 || devicesDirExists(st) {
		t.Fatal("a wrong fingerprint must not sign, nor create the authority")
	}
	if st.DeviceRegistry().Pairing.Failures != 1 {
		t.Fatal("a wrong fingerprint must be saved as one attempt")
	}
}

func TestTheFullPairingGrantsTheRoleAndShowsTheDevice(t *testing.T) {
	reg, st := deviceRegistry(t)
	code := pairCode(t, reg, "device", "pair", "--name", "telephone", "--role", "observer")
	fingerprint := redeemWith(t, st, code)

	out, err := dispatchDevice(t, reg, "device", "approve", code, "--fingerprint", fingerprint)
	if err != nil {
		t.Fatalf("device approve: %v", err)
	}
	if !strings.Contains(out, "observer") || !strings.Contains(out, "telephone") {
		t.Fatalf("approve must name the device and its role: %q", out)
	}
	registry := st.DeviceRegistry()
	if len(registry.Devices) != 1 {
		t.Fatalf("approve registered %d devices, want 1", len(registry.Devices))
	}
	device := registry.Devices[0]
	if got := st.RoleNames()[device.ID.Principal().ID]; got != "observer" {
		t.Fatalf("the device holds %q, want observer", got)
	}

	list, err := dispatchDevice(t, reg, "device", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(list, device.ID.String()) || !strings.Contains(list, "active") {
		t.Fatalf("list must show the device as active: %q", list)
	}

	if _, err = dispatchDevice(t, reg, "device", "revoke", device.ID.String()); err != nil {
		t.Fatalf("device revoke: %v", err)
	}
	list, err = dispatchDevice(t, reg, "device", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var listing struct {
		Devices []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"devices"`
	}
	if unmarshalErr := json.Unmarshal([]byte(list), &listing); unmarshalErr != nil {
		t.Fatalf("list --json is not JSON: %v: %q", unmarshalErr, list)
	}
	if len(listing.Devices) != 1 || listing.Devices[0].Status != "revoked" {
		t.Fatalf("after revoke the list says %q", list)
	}
}

func TestTheFirstDeviceGrantWarnsAboutTheCliffAndTheApprovingAccount(t *testing.T) {
	reg, st := deviceRegistry(t)
	code := pairCode(t, reg, "device", "pair")
	fingerprint := redeemWith(t, st, code)
	out, err := dispatchDevice(t, reg, "device", "approve", code, "--fingerprint", fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "observer") || !strings.Contains(out, "approved this device") {
		t.Fatalf("the first grant must warn that gateways without a role and the approving account lose their rights: %q", out)
	}
}

func TestApproveRefusesAWrongCodeAndCountsIt(t *testing.T) {
	reg, st := deviceRegistry(t)
	code := pairCode(t, reg, "device", "pair")
	redeemWith(t, st, code)
	if _, err := dispatchDevice(t, reg, "device", "approve", wrongCodeLike(code)); !errors.Is(err, devices.ErrWrongCode) {
		t.Fatalf("wrong code = %v, want ErrWrongCode", err)
	}
	if st.DeviceRegistry().Pairing.Failures != 1 {
		t.Fatal("a refused code must be counted in the saved window")
	}
}

func TestApproveOutputNeverRepeatsTheCode(t *testing.T) {
	reg, st := deviceRegistry(t)
	code := pairCode(t, reg, "device", "pair")
	fingerprint := redeemWith(t, st, code)
	out, err := dispatchDevice(t, reg, "device", "approve", code, "--fingerprint", fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, code) || strings.Contains(out, strings.ReplaceAll(code, "-", "")) {
		t.Fatalf("approve repeated the pairing code: %q", out)
	}
}

func TestNoLogLineCarriesThePairingCode(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	reg, st := deviceRegistry(t)
	code := pairCode(t, reg, "device", "pair")
	fingerprint := redeemWith(t, st, code)
	if _, err := dispatchDevice(t, reg, "device", "approve", code, "--fingerprint", fingerprint); err != nil {
		t.Fatal(err)
	}
	if logs.Len() == 0 {
		t.Fatal("the journal recorded nothing: pairing and approval must be logged")
	}
	if strings.Contains(logs.String(), code) {
		t.Fatalf("a log line carries the pairing code: %s", logs.String())
	}
}

func TestDenyClosesTheWindowAndNothingIsGranted(t *testing.T) {
	reg, st := deviceRegistry(t)
	code := pairCode(t, reg, "device", "pair")
	redeemWith(t, st, code)
	if _, err := dispatchDevice(t, reg, "device", "deny", code); err != nil {
		t.Fatalf("device deny: %v", err)
	}
	if _, err := dispatchDevice(t, reg, "device", "approve", code); !errors.Is(err, devices.ErrNoPairingWindow) {
		t.Fatalf("approve after deny = %v, want ErrNoPairingWindow", err)
	}
	if len(st.DeviceRegistry().Devices) != 0 {
		t.Fatal("deny must not register a device")
	}
}

func TestRevokeAndApproveNameWhatTheyNeed(t *testing.T) {
	reg, _ := deviceRegistry(t)
	if _, err := dispatchDevice(t, reg, "device", "revoke"); !errors.Is(err, errDeviceIDMissing) {
		t.Fatalf("revoke with no id = %v, want errDeviceIDMissing", err)
	}
	if _, err := dispatchDevice(t, reg, "device", "approve"); !errors.Is(err, errDeviceCodeMissing) {
		t.Fatalf("approve with no code = %v, want errDeviceCodeMissing", err)
	}
}
