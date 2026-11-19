package handler

// Lightsoft fork (LIGH-253): sign in with the Cloudflare Access identity.
// Outside the tailnet the team client sits behind Cloudflare Access, which has
// already verified the user's email. This exchanges that verified email for a
// Multica session under exactly the rules of the email-code login: an existing
// user signs in, and a new email gets an account only through the usual signup
// gates (a pending invitation when ALLOW_SIGNUP=false).

import (
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
)

// Error codes the team login page branches on.
const (
	cfAccessCodeNoAssertion      = "cf_access_no_assertion"
	cfAccessCodeInvalidAssertion = "cf_access_invalid_assertion"
	cfAccessCodeNotInvited       = "cf_access_not_invited"
	cfAccessCodeDisabled         = "cf_access_user_disabled"
)

var (
	cfAccessVerifierOnce sync.Once
	cfAccessVerifier     *auth.CloudflareAccessVerifier
)

// lightsoftCFAccessVerifier is configured from the environment once. Tests
// replace it with setLightsoftCFAccessVerifierForTest.
func lightsoftCFAccessVerifier() *auth.CloudflareAccessVerifier {
	cfAccessVerifierOnce.Do(func() {
		cfAccessVerifier = auth.NewCloudflareAccessVerifierFromEnv()
	})
	return cfAccessVerifier
}

// LightsoftCloudflareAccessLogin is POST /api/lightsoft/auth/cloudflare-access.
// 404 when Access login is not configured, so the team client treats this
// backend like an upstream one. Failures that mean "no usable Access identity"
// are 403, never 401: the API client reads 401 as an ended session.
func (h *Handler) LightsoftCloudflareAccessLogin(w http.ResponseWriter, r *http.Request) {
	verifier := lightsoftCFAccessVerifier()
	if verifier == nil {
		writeError(w, http.StatusNotFound, "cloudflare access login is not configured")
		return
	}
	assertion := r.Header.Get(auth.CloudflareAccessHeader)
	if assertion == "" {
		writeErrorCode(w, http.StatusForbidden, cfAccessCodeNoAssertion, "request did not come through cloudflare access")
		return
	}
	identity, err := verifier.Verify(r.Context(), assertion)
	if err != nil {
		slog.Warn("cloudflare access login: assertion rejected", append(logger.RequestAttrs(r), "error", err)...)
		writeErrorCode(w, http.StatusForbidden, cfAccessCodeInvalidAssertion, "cloudflare access identity could not be verified")
		return
	}

	user, isNew, err := h.findOrCreateUser(r.Context(), identity.Email)
	if err != nil {
		if errors.Is(err, auth.ErrTemporarilyDisabledUser) {
			writeErrorCode(w, http.StatusForbidden, cfAccessCodeDisabled, auth.TemporarilyDisabledUserError)
			return
		}
		var signupErr SignupError
		if errors.As(err, &signupErr) {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": signupErr.Error(),
				"code":  cfAccessCodeNotInvited,
				"email": identity.Email,
			})
			return
		}
		slog.Warn("cloudflare access login: user lookup or creation failed", append(logger.RequestAttrs(r), "error", err, "email", identity.Email)...)
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	if isNew {
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.Signup(uuidToString(user.ID), user.Email, signupSourceFromRequest(r)))
	}

	// Same session issuance as VerifyCode; kept here so the fork does not
	// reshape an upstream handler.
	tokenString, err := h.issueJWT(user)
	if err != nil {
		if errors.Is(err, auth.ErrTemporarilyDisabledUser) {
			writeErrorCode(w, http.StatusForbidden, cfAccessCodeDisabled, auth.TemporarilyDisabledUserError)
			return
		}
		slog.Warn("cloudflare access login failed", append(logger.RequestAttrs(r), "error", err, "email", identity.Email)...)
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	if err := auth.SetAuthCookies(w, tokenString); err != nil {
		slog.Warn("failed to set auth cookies", "error", err)
	}
	if h.CFSigner != nil {
		for _, cookie := range h.CFSigner.SignedCookies(time.Now().Add(auth.AuthTokenTTL())) {
			http.SetCookie(w, cookie)
		}
	}

	slog.Info("user logged in via cloudflare access", append(logger.RequestAttrs(r), "user_id", uuidToString(user.ID), "email", user.Email)...)
	writeJSON(w, http.StatusOK, LoginResponse{
		Token: tokenString,
		User:  h.userToResponse(user),
	})
}
