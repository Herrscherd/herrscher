package host

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/authz"
	"github.com/Herrscherd/herrscher/core/internal/devices"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

var (
	errDeviceIDMissing   = errors.New("name a device (see `device list`)")
	errDeviceCodeMissing = errors.New("name the pairing code that `device pair` printed")
	errDeviceNeedsRoles  = errors.New("the device verbs need a role table: grant a role with `role grant` first, until then only the local operator runs them")
)

type deviceView struct {
	ID          string    `json:"id"`
	Name        string    `json:"name,omitempty"`
	Role        string    `json:"role"`
	Status      string    `json:"status"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"createdAt"`
	LastSeen    time.Time `json:"lastSeen"`
	lifecycle   devices.Status
}

type pendingView struct {
	Name        string    `json:"name,omitempty"`
	Role        string    `json:"role"`
	Fingerprint string    `json:"fingerprint"`
	RedeemedAt  time.Time `json:"redeemedAt"`
}

type deviceListing struct {
	Devices []deviceView `json:"devices"`
	Pending *pendingView `json:"pending,omitempty"`
}

func addAccessCommands(reg *cli.Registry, cliMessages *clitext.Messages, lang languageSource, store *state.State) error {
	if err := addRoleCommands(reg, cliMessages, lang, store); err != nil {
		return err
	}
	return addDeviceCommands(reg, cliMessages, lang, store)
}

func addDeviceCommands(reg *cli.Registry, cliMessages *clitext.Messages, lang languageSource, store *state.State) error {
	gate := &devices.AuthorityGate{}
	return addVerbs(
		reg,
		devicePairVerb(cliMessages, lang, store),
		deviceApproveVerb(cliMessages, lang, store, gate),
		deviceDenyVerb(cliMessages, lang, store),
		deviceListVerb(cliMessages, lang, store),
		deviceRevokeVerb(cliMessages, lang, store),
	)
}

func devicePairVerb(cliMessages *clitext.Messages, lang languageSource, store *state.State) contracts.Cmd {
	return contracts.New("device", "pair").
		Help(clitext.CmdHelpDevicePair).
		ValueParam("name", clitext.CmdParamDevicePairName, false).
		ValueParam("role", clitext.CmdParamDevicePairRole, false).
		Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
			return cli.Data(pairDevice(cliMessages, lang(), store, input))
		})
}

func pairDevice(cliMessages *clitext.Messages, lang i18n.Language, store *state.State, input contracts.Input) (string, error) {
	name := strings.TrimSpace(input.Get("name"))
	role := strings.TrimSpace(input.Get("role"))
	if role == "" {
		role = string(authz.RoleApprover)
	}
	var code devices.PairingCode
	err := store.UpdateDevices(func(registry *devices.Registry) error {
		opened, openErr := registry.OpenWindow(time.Now(), name, authz.Role(role), rand.Reader)
		code = opened
		return openErr
	})
	if err != nil {
		return "", err
	}
	slog.Info("device pairing window opened", "name", name, "role", role)
	return replyText(cliMessages, lang).Text(clitext.HostDevicePaired, clitext.Params{clitext.ParamCode: code.String(), clitext.ParamRole: role}), nil
}

func deviceApproveVerb(cliMessages *clitext.Messages, lang languageSource, store *state.State, gate *devices.AuthorityGate) contracts.Cmd {
	return contracts.New("device", "approve").
		Help(clitext.CmdHelpDeviceApprove).
		ValueParam("code", clitext.CmdParamDeviceApproveCode, false).
		ValueParam("fingerprint", clitext.CmdParamDeviceApproveFingerprint, false).
		Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
			return cli.Data(approveDevice(cliMessages, lang(), store, gate, input))
		})
}

func approveDevice(cliMessages *clitext.Messages, lang i18n.Language, store *state.State, gate *devices.AuthorityGate, input contracts.Input) (string, error) {
	code, err := codeOperand(input)
	if err != nil {
		return "", err
	}
	typed := strings.TrimSpace(input.Get("fingerprint"))
	if typed == "" {
		return previewDevice(cliMessages, lang, store, code)
	}
	fingerprint, err := devices.ParseFingerprint(typed)
	if err != nil {
		return "", err
	}
	return signDevice(cliMessages, lang, store, gate, code, fingerprint)
}

func previewDevice(cliMessages *clitext.Messages, lang i18n.Language, store *state.State, code devices.PairingCode) (string, error) {
	var preview devices.Preview
	err := store.UpdateDevices(func(registry *devices.Registry) error {
		shown, previewErr := registry.Preview(time.Now(), code)
		preview = shown
		return previewErr
	})
	if err != nil {
		return "", err
	}
	text := replyText(cliMessages, lang)
	name := preview.Name
	if name == "" {
		name = text.Text(clitext.HostDeviceUnnamed, nil)
	}
	return text.Text(clitext.HostDevicePreview, clitext.Params{
		clitext.ParamName:        name,
		clitext.ParamRole:        string(preview.Role),
		clitext.ParamFingerprint: preview.Fingerprint,
	}), nil
}

func signDevice(cliMessages *clitext.Messages, lang i18n.Language, store *state.State, gate *devices.AuthorityGate, code devices.PairingCode, fingerprint devices.Fingerprint) (string, error) {
	authority, err := gate.LoadOrCreate(store.PrivateDir(), time.Now(), rand.Reader, len(store.DeviceRegistry().Devices) > 0)
	if err != nil {
		return "", err
	}
	enrolment, first, err := store.EnrollDevice(func(registry *devices.Registry) (devices.Enrolment, error) {
		sign := func(csr *x509.CertificateRequest, deviceID devices.DeviceID, signedAt time.Time) (*x509.Certificate, error) {
			return authority.SignClient(csr, deviceID, signedAt, rand.Reader)
		}
		return registry.Approve(time.Now(), code, fingerprint, rand.Reader, sign)
	})
	if err != nil {
		return "", err
	}
	role := string(enrolment.Role)
	name := enrolment.Device.Name
	if name == "" {
		name = enrolment.Device.ID.String()
	}
	slog.Info("device approved", "device", enrolment.Device.ID.String(), "role", role)
	text := replyText(cliMessages, lang)
	out := text.Text(clitext.HostDeviceApproved, clitext.Params{
		clitext.ParamID:          enrolment.Device.ID.String(),
		clitext.ParamName:        name,
		clitext.ParamRole:        role,
		clitext.ParamFingerprint: enrolment.Fingerprint,
	})
	if first {
		out += "\n" + text.Text(clitext.HostDeviceFirstGrant, nil)
	}
	return out, nil
}

func deviceDenyVerb(cliMessages *clitext.Messages, lang languageSource, store *state.State) contracts.Cmd {
	return contracts.New("device", verbDeny).
		Help(clitext.CmdHelpDeviceDeny).
		ValueParam("code", clitext.CmdParamDeviceDenyCode, false).
		Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
			return cli.Data(denyDevice(cliMessages, lang(), store, input))
		})
}

func denyDevice(cliMessages *clitext.Messages, lang i18n.Language, store *state.State, input contracts.Input) (string, error) {
	code, err := codeOperand(input)
	if err != nil {
		return "", err
	}
	var closed devices.Window
	err = store.UpdateDevices(func(registry *devices.Registry) error {
		if registry.Pairing != nil {
			closed = *registry.Pairing
		}
		return registry.Deny(time.Now(), code)
	})
	if err != nil {
		return "", err
	}
	slog.Info("device pairing window closed without a grant", "name", closed.Name, "role", string(closed.Role), "redeemed", closed.Pending != nil)
	return replyText(cliMessages, lang).Text(clitext.HostDeviceDenied, nil), nil
}

func deviceListVerb(cliMessages *clitext.Messages, lang languageSource, store *state.State) contracts.Cmd {
	return contracts.New("device", "list").
		Help(clitext.CmdHelpDeviceList).
		Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
			return cli.Data(listDevices(cliMessages, lang(), store, input.JSON))
		})
}

func listDevices(cliMessages *clitext.Messages, lang i18n.Language, store *state.State, asJSON bool) (string, error) {
	now := time.Now()
	registry := store.DeviceRegistry()
	names := store.RoleNames()
	listing := deviceListing{Devices: make([]deviceView, 0, len(registry.Devices))}
	for idx := range registry.Devices {
		device := &registry.Devices[idx]
		listing.Devices = append(listing.Devices, deviceView{
			ID:          device.ID.String(),
			Name:        device.Name,
			Role:        string(authz.RoleOf(device.ID.Principal(), names)),
			Status:      device.StatusAt(now).String(),
			Fingerprint: device.Fingerprint,
			CreatedAt:   device.CreatedAt,
			LastSeen:    device.LastSeen,
			lifecycle:   device.StatusAt(now),
		})
	}
	if registry.Pairing != nil && registry.Pairing.Pending != nil {
		listing.Pending = &pendingView{
			Name:        registry.Pairing.Name,
			Role:        string(registry.Pairing.Role),
			Fingerprint: registry.Pairing.Pending.Fingerprint,
			RedeemedAt:  registry.Pairing.Pending.RedeemedAt,
		}
	}
	if asJSON {
		body, err := json.Marshal(listing)
		return string(body), err
	}
	return renderDeviceListing(cliMessages, lang, listing), nil
}

func renderDeviceListing(cliMessages *clitext.Messages, lang i18n.Language, listing deviceListing) string {
	text := replyText(cliMessages, lang)
	if len(listing.Devices) == 0 && listing.Pending == nil {
		return text.Text(clitext.HostDeviceNone, nil)
	}
	var body strings.Builder
	for idx := range listing.Devices {
		device := &listing.Devices[idx]
		name := device.Name
		if name == "" {
			name = device.ID
		}
		row := text.Text(clitext.HostDeviceRow, clitext.Params{
			clitext.ParamID:     device.ID,
			clitext.ParamName:   name,
			clitext.ParamRole:   device.Role,
			clitext.ParamStatus: text.Text(statusKey(device.lifecycle), nil),
			clitext.ParamWhen:   device.LastSeen.Format(time.RFC3339),
		})
		_, _ = body.WriteString("- " + row + "\n")
	}
	if listing.Pending != nil {
		_, _ = body.WriteString(text.Text(clitext.HostDevicePending, clitext.Params{
			clitext.ParamName:        listing.Pending.Name,
			clitext.ParamRole:        listing.Pending.Role,
			clitext.ParamFingerprint: listing.Pending.Fingerprint,
		}) + "\n")
	}
	return body.String()
}

func deviceRevokeVerb(cliMessages *clitext.Messages, lang languageSource, store *state.State) contracts.Cmd {
	return contracts.New("device", "revoke").
		Help(clitext.CmdHelpDeviceRevoke).
		ValueParam("id", clitext.CmdParamDeviceRevokeID, false).
		Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
			return cli.Data(revokeDevice(cliMessages, lang(), store, input))
		})
}

func revokeDevice(cliMessages *clitext.Messages, lang i18n.Language, store *state.State, input contracts.Input) (string, error) {
	operand := strings.TrimSpace(firstOf(input.Get("id"), input.Rest))
	if operand == "" {
		return "", errDeviceIDMissing
	}
	deviceID, err := devices.ParseDeviceID(operand)
	if err != nil {
		return "", err
	}
	err = store.UpdateDevices(func(registry *devices.Registry) error {
		return registry.Revoke(time.Now(), deviceID)
	})
	if err != nil {
		return "", err
	}
	slog.Info("device revoked", "device", deviceID.String(), "role", store.RoleNames()[deviceID.Principal().ID])
	return replyText(cliMessages, lang).Text(clitext.HostDeviceRevoked, clitext.Params{clitext.ParamID: deviceID.String()}), nil
}

func codeOperand(input contracts.Input) (devices.PairingCode, error) {
	operand := strings.TrimSpace(firstOf(input.Get("code"), input.Rest))
	if operand == "" {
		return devices.PairingCode{}, errDeviceCodeMissing
	}
	return devices.ParsePairingCode(operand)
}

func statusKey(status devices.Status) clitext.Key {
	switch status {
	case devices.StatusActive:
		return clitext.HostDeviceStatusActive
	case devices.StatusExpired:
		return clitext.HostDeviceStatusExpired
	case devices.StatusRevoked:
		return clitext.HostDeviceStatusRevoked
	}
	return clitext.HostDeviceStatusActive
}
