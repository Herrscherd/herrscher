package manager

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func (h *Handler) sessionBanner(lang i18n.Language, repo, name, worktree, branch, cmd, modelID string, shared bool) string {
	banner := h.textIn(lang, clitext.SessionBannerReady, clitext.Params{clitext.ParamName: name}) + "\n"
	if repo == "" {
		banner += h.textIn(lang, clitext.SessionBannerProjectCwd, nil) + "\n"
	} else {
		banner += h.textIn(lang, clitext.SessionBannerProject, clitext.Params{clitext.ParamName: filepath.Base(repo), clitext.ParamRepo: fmt.Sprintf("%#q", repo)}) + "\n"
	}
	switch {
	case worktree != "":
		banner += h.textIn(lang, clitext.SessionBannerWorktree, clitext.Params{clitext.ParamPath: fmt.Sprintf("%#q", worktree), clitext.ParamBranch: fmt.Sprintf("%#q", branch)}) + "\n"
	case shared:
		banner += h.textIn(lang, clitext.SessionBannerShared, nil) + "\n"
	default:
		banner += h.textIn(lang, clitext.SessionBannerNotGit, nil) + "\n"
	}
	banner += h.textIn(lang, clitext.SessionBannerCommand, clitext.Params{clitext.ParamCommand: fmt.Sprintf("%#q", cmd)})
	if modelID != "" {
		banner += h.textIn(lang, clitext.SessionBannerModel, clitext.Params{clitext.ParamModel: fmt.Sprintf("%#q", modelID)})
	}
	return banner
}

type channelSlot struct {
	id    string
	owned bool
	typ   string
}

func (h *Handler) openChannel(ctx context.Context, req createRequest, title string) (channelSlot, error) {
	if req.adopted != "" {
		return channelSlot{id: req.adopted, typ: "text"}, nil
	}
	switch req.home.Type {
	case categoryKind, terminalPlace:
		chID, err := req.admin.CreateUnder(ctx, req.home.ID, title)
		if err != nil {
			return channelSlot{}, h.labelled("create channel", clitext.HostErrCreateChannel, err)
		}
		return channelSlot{id: chID, owned: true, typ: "text"}, nil
	case forumKind:
		chID, err := req.admin.ForumPost(ctx, req.home.ID, title, h.textIn(h.noticeLanguage(req.lang), clitext.HostSessionStartedPost, clitext.Params{clitext.ParamName: title}))
		if err != nil {
			return channelSlot{}, h.labelled("create forum post", clitext.HostErrCreateForumPost, err)
		}
		return channelSlot{id: chID, owned: true, typ: forumKind}, nil
	default:
		return channelSlot{}, &UnsupportedHomeTypeError{Type: req.home.Type}
	}
}

func (h *Handler) adminFor(sess state.Session) channelAdmin {
	if h.td != nil && strings.HasPrefix(sess.ChannelID, "terminal/") {
		return h.td
	}
	if admin, routed := h.gatewayRoute(sess.Owned, sess.Gateways); routed {
		return admin
	}
	return h.d
}

func (h *Handler) gatewayRoute(owned bool, gateways []string) (channelAdmin, bool) {
	if owned || len(gateways) != 1 || h.gatewayAdmins == nil {
		return nil, false
	}
	if admin, ok := h.gatewayAdmins[gateways[0]]; ok && admin != nil {
		return admin, true
	}
	return nil, true
}

func (h *Handler) announcingAdmin(adopted string, gateways []string, home channelAdmin) channelAdmin {
	if adopted == "" {
		return home
	}
	if admin, routed := h.gatewayRoute(false, gateways); routed {
		return admin
	}
	return home
}

func channelRef(admin channelAdmin, channelID string) string {
	if admin == nil {
		return channelID
	}
	return admin.ChannelRef(channelID)
}

func (h *Handler) tidyChannel(ctx context.Context, sess state.Session) string {
	if !sess.Owned {
		return ""
	}
	admin := h.adminFor(sess)
	if admin == nil {
		return ""
	}
	if err := admin.Archive(ctx, sess.ChannelID); err != nil {
		return "\n" + h.text(clitext.SessionChannelLeftOpen, clitext.Params{clitext.ParamErr: err.Error()})
	}
	return ""
}
