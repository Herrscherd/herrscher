package health

import (
	"errors"
	"fmt"
	"strconv"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func stubRenderNotice(t *testing.T, cause error) {
	t.Helper()
	previous := renderNotice
	t.Cleanup(func() { renderNotice = previous })
	renderNotice = func(i18n.Language, contracts.Notice) (string, error) {
		return "", fmt.Errorf("stub: %w", cause)
	}
}

func TestNoticeFallsBackOnlyWhenItDoesNotMatchItsCode(t *testing.T) {
	withText := contracts.Notice{Code: contracts.CodeSettingInvalid, Args: []contracts.Arg{contracts.Text("Setting", "compactAt")}}
	withoutText := contracts.Notice{Code: contracts.CodeSettingInvalid, Args: []contracts.Arg{contracts.Count(3)}}
	cases := []struct {
		name   string
		cause  error
		notice contracts.Notice
		want   string
	}{
		{"unknown code shows the text", contracts.ErrUnknownCode, withText, "compactAt"},
		{"arguments mismatch show the text", contracts.ErrNoticeArgs, withText, "compactAt"},
		{"unknown code without text shows the code", contracts.ErrUnknownCode, withoutText, strconv.FormatUint(uint64(contracts.CodeSettingInvalid), 10)},
		{"arguments mismatch without text shows the code", contracts.ErrNoticeArgs, withoutText, strconv.FormatUint(uint64(contracts.CodeSettingInvalid), 10)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubRenderNotice(t, tc.cause)
			r := &renderer{lang: i18n.English}
			if got := r.notice(tc.notice); got != tc.want || r.err != nil {
				t.Fatalf("notice() = %q, err %v, want %q and no error", got, r.err, tc.want)
			}
		})
	}
}

func TestNoticeKeepsEveryOtherRenderingError(t *testing.T) {
	notice := contracts.Notice{Code: contracts.CodeSettingInvalid, Args: []contracts.Arg{contracts.Text("Setting", "compactAt")}}
	for _, cause := range []error{contracts.ErrMissingMessage, errors.New("catalogue unreadable")} {
		t.Run(cause.Error(), func(t *testing.T) {
			stubRenderNotice(t, cause)
			r := &renderer{lang: i18n.English}
			r.notice(notice)
			if !errors.Is(r.err, cause) {
				t.Fatalf("notice() error = %v, want %v kept", r.err, cause)
			}
		})
	}
}
