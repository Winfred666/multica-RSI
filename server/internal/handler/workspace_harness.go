package handler

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/multica-ai/multica/harness"
	"github.com/multica-ai/multica/server/internal/auth"
)

func (h *Handler) installWorkspaceHarness(r *http.Request, workspaceID string, keepContext bool) error {
	baseURL := h.cfg.PublicURL
	if baseURL == "" {
		address, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
		if !ok {
			return fmt.Errorf("server listen address is unavailable")
		}
		_, port, err := net.SplitHostPort(address.String())
		if err != nil {
			return err
		}
		baseURL = "http://" + net.JoinHostPort("127.0.0.1", port)
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		if cookie, err := r.Cookie(auth.AuthCookieName); err == nil {
			token = cookie.Value
		}
	}
	if token == "" {
		return fmt.Errorf("workspace installer needs the authenticated request's credentials")
	}
	return harness.Install(r.Context(), baseURL, token, workspaceID, keepContext, h.pluginsV1Enabled(r.Context()))
}
