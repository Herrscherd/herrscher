package host

import "context"

func (h *hub) RestartWithResume(name, token string) error {
	sess, ok := h.st.FindSession(name)
	if !ok {
		return &NotFoundError{Kind: kindSession, Name: name}
	}
	h.goDead(name)
	sess.ResumeToken = token
	sess.InterruptedTurn = ""
	return h.goLiveStarted(context.Background(), sess, func() error {
		return h.st.SetResumeTokenAndClearInterruptedTurn(name, token)
	})
}
