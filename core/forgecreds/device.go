package forgecreds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ClientID is the public identifier of the herrscher GitHub App. A device
// flow needs no client secret, so none is held by the host.
const ClientID = "Iv23liFEq6dF9ABLEueS"

const (
	deviceCodePath      = "/login/device/code"
	formClientID        = "client_id"
	grantPath           = "/login/oauth/access_token"
	deviceGrantType     = "urn:ietf:params:oauth:grant-type:device_code"
	refreshGrantType    = "refresh_token"
	defaultPollInterval = 5 * time.Second
	slowDownStep        = 5 * time.Second
	maxResponseBytes    = 64 << 10
	refreshSkew         = 5 * time.Minute
)

const (
	errAuthorizationPending = "authorization_pending"
	errSlowDown             = "slow_down"
	errExpiredToken         = "expired_token"
	errAccessDenied         = "access_denied"
	errBadRefreshToken      = "bad_refresh_token"
	errIncorrectCredentials = "incorrect_client_credentials"
	errDeviceFlowDisabled   = "device_flow_disabled"
)

// ErrAppUnavailable reports a forge host that does not offer the herrscher App
// for a device sign-in, as a GitHub Enterprise Server without the App does.
var ErrAppUnavailable = errors.New("the forge has no herrscher app for sign-in")

// ErrDeviceExpired reports a device code the user did not enter in time.
var ErrDeviceExpired = errors.New("the sign-in code expired")

// ErrDeviceDenied reports a sign-in the user refused on the forge.
var ErrDeviceDenied = errors.New("the sign-in was denied on the forge")

// ErrDeviceRejected reports a sign-in the forge answered in a way that is not
// a normal step of the flow.
var ErrDeviceRejected = errors.New("the forge refused the sign-in")

// ErrRefreshRejected reports a refresh token the forge no longer accepts.
var ErrRefreshRejected = errors.New("the forge refused the refresh token")

// ErrAppMisconfigured reports client credentials the forge does not accept for
// the herrscher App: the App is misconfigured, the refresh token is not at fault.
var ErrAppMisconfigured = errors.New("the forge does not accept the herrscher app credentials")

// DeviceCode is the code a user enters on the forge to authorize the host.
type DeviceCode struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	Interval        time.Duration
	ExpiresAt       time.Time
}

// Flow runs the device flow of the GitHub App against one forge base URL.
type Flow struct {
	http     *http.Client
	clientID string
	wait     func(context.Context, time.Duration) error
	now      func() time.Time
}

type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int64  `json:"expires_in"`
	Interval        int64  `json:"interval"`
	Error           string `json:"error"`
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshExpiresIn int64  `json:"refresh_token_expires_in"`
	Interval         int64  `json:"interval"`
	Error            string `json:"error"`
}

// Begin asks the forge at base for a device code.
func (f Flow) Begin(ctx context.Context, base string) (DeviceCode, error) {
	var resp deviceCodeResponse
	status, err := f.postForm(ctx, base+deviceCodePath, url.Values{formClientID: {f.clientID}}, &resp)
	if err != nil {
		return DeviceCode{}, err
	}
	if status == http.StatusNotFound || isAppMissing(resp.Error) {
		return DeviceCode{}, ErrAppUnavailable
	}
	if status != http.StatusOK || resp.DeviceCode == "" || resp.UserCode == "" {
		return DeviceCode{}, fmt.Errorf("sign-in code request: HTTP %d: %w", status, ErrDeviceRejected)
	}
	return DeviceCode{
		DeviceCode:      resp.DeviceCode,
		UserCode:        resp.UserCode,
		VerificationURI: resp.VerificationURI,
		Interval:        secondsOr(resp.Interval, defaultPollInterval),
		ExpiresAt:       f.now().Add(time.Duration(resp.ExpiresIn) * time.Second),
	}, nil
}

// Await polls the forge until the user authorizes the code, and returns the
// record of the tokens it granted.
func (f Flow) Await(ctx context.Context, base string, code DeviceCode) (Record, error) {
	form := url.Values{
		formClientID:  {f.clientID},
		"device_code": {code.DeviceCode},
		"grant_type":  {deviceGrantType},
	}
	interval := code.Interval
	for f.now().Before(code.ExpiresAt) {
		if err := f.wait(ctx, interval); err != nil {
			return Record{}, err
		}
		resp, err := f.exchange(ctx, base, form)
		if err != nil {
			return Record{}, err
		}
		switch resp.Error {
		case "":
			return f.recordFrom(resp)
		case errAuthorizationPending:
		case errSlowDown:
			interval = secondsOr(resp.Interval, interval+slowDownStep)
		case errExpiredToken:
			return Record{}, ErrDeviceExpired
		case errAccessDenied:
			return Record{}, ErrDeviceDenied
		default:
			return Record{}, fmt.Errorf("sign-in: %s: %w", resp.Error, ErrDeviceRejected)
		}
	}
	return Record{}, ErrDeviceExpired
}

// Refresh exchanges the refresh token of record for a new access token.
func (f Flow) Refresh(ctx context.Context, base string, record Record) (Record, error) {
	form := url.Values{
		formClientID:    {f.clientID},
		"grant_type":    {refreshGrantType},
		"refresh_token": {record.RefreshToken},
	}
	resp, err := f.exchange(ctx, base, form)
	if err != nil {
		return Record{}, err
	}
	if resp.Error == errBadRefreshToken {
		return Record{}, ErrRefreshRejected
	}
	if resp.Error == errIncorrectCredentials {
		return Record{}, ErrAppMisconfigured
	}
	if resp.Error != "" {
		return Record{}, fmt.Errorf("refresh: %s: %w", resp.Error, ErrDeviceRejected)
	}
	refreshed, err := f.recordFrom(resp)
	if err != nil {
		return Record{}, err
	}
	if refreshed.RefreshToken == "" {
		return Record{}, fmt.Errorf("refresh granted no refresh token: %w", ErrDeviceRejected)
	}
	refreshed.Login = record.Login
	return refreshed, nil
}

func (f Flow) exchange(ctx context.Context, base string, form url.Values) (tokenResponse, error) {
	var resp tokenResponse
	status, err := f.postForm(ctx, base+grantPath, form, &resp)
	if err != nil {
		return tokenResponse{}, err
	}
	if status != http.StatusOK && resp.Error == "" {
		return tokenResponse{}, fmt.Errorf("sign-in: HTTP %d: %w", status, ErrDeviceRejected)
	}
	return resp, nil
}

func (f Flow) recordFrom(resp tokenResponse) (Record, error) {
	if resp.AccessToken == "" {
		return Record{}, fmt.Errorf("sign-in granted no token: %w", ErrDeviceRejected)
	}
	now := f.now()
	record := Record{AccessToken: resp.AccessToken, RefreshToken: resp.RefreshToken}
	if resp.ExpiresIn > 0 {
		record.AccessExpiresAt = now.Add(time.Duration(resp.ExpiresIn) * time.Second)
	}
	if resp.RefreshExpiresIn > 0 {
		record.RefreshExpiresAt = now.Add(time.Duration(resp.RefreshExpiresIn) * time.Second)
	}
	return record, nil
}

func (f Flow) postForm(ctx context.Context, endpoint string, form url.Values, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, fmt.Errorf("build sign-in request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := f.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("reach the forge: %w", err)
	}
	defer closeBody(resp.Body)
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return 0, fmt.Errorf("read the forge answer: %w", err)
	}
	if err := json.Unmarshal(raw, out); err != nil && resp.StatusCode == http.StatusOK {
		return resp.StatusCode, fmt.Errorf("decode the forge answer: %w", ErrDeviceRejected)
	}
	return resp.StatusCode, nil
}

func isAppMissing(code string) bool {
	return code == errIncorrectCredentials || code == errDeviceFlowDisabled
}

func secondsOr(seconds int64, fallback time.Duration) time.Duration {
	if seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func closeBody(body io.Closer) {
	if err := body.Close(); err != nil {
		slog.Warn("close forge reply", "err", err)
	}
}

func waitFor(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
