package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// authCookieName matches server/internal/auth.AuthCookieName in the Multica
// backend; the team client shares the production origin's login cookie.
const authCookieName = "multica_auth"

var (
	errUnauthenticated = errors.New("unauthenticated")
	errForbidden       = errors.New("forbidden")
)

// multicaClient reads the production Multica API with the caller's own
// session, only to learn who they are and whether they belong to the
// workspace. The service holds no Multica credential of its own.
type multicaClient struct {
	base string
	http *http.Client
}

type apiUser struct {
	ID string `json:"id"`
}

type apiMember struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

func newMulticaClient(base string) *multicaClient {
	return &multicaClient{base: base, http: &http.Client{Timeout: 10 * time.Second}}
}

func (c *multicaClient) get(ctx context.Context, token, workspaceID, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	// Bearer rather than the cookie: the backend renews cookie sessions with a
	// Set-Cookie this server-side call would drop.
	req.Header.Set("Authorization", "Bearer "+token)
	if workspaceID != "" {
		req.Header.Set("X-Workspace-ID", workspaceID)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errUnauthenticated
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound:
		return errForbidden
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GET %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *multicaClient) me(ctx context.Context, token string) (apiUser, error) {
	var u apiUser
	err := c.get(ctx, token, "", "/api/me", &u)
	return u, err
}

func (c *multicaClient) members(ctx context.Context, token, workspaceID string) ([]apiMember, error) {
	var m []apiMember
	err := c.get(ctx, token, workspaceID, "/api/workspaces/"+url.PathEscape(workspaceID)+"/members", &m)
	return m, err
}
