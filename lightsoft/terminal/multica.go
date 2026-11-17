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
	errNotFound        = errors.New("not found")
)

// multicaClient calls the production Multica API with the caller's own
// credential. The gateway never holds a credential of its own, so it can only
// read what the signed-in user can read.
type multicaClient struct {
	base string
	http *http.Client
}

type apiUser struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type apiMember struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

type apiProject struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
}

type apiResource struct {
	ResourceType string          `json:"resource_type"`
	ResourceRef  json.RawMessage `json:"resource_ref"`
}

func newMulticaClient(base string) *multicaClient {
	return &multicaClient{base: base, http: &http.Client{Timeout: 10 * time.Second}}
}

// credentialFromRequest returns the caller's Multica token: an explicit
// bearer token (non-browser clients) or the HttpOnly login cookie.
func credentialFromRequest(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if c, err := r.Cookie(authCookieName); err == nil {
		return c.Value
	}
	return ""
}

func (c *multicaClient) get(ctx context.Context, token, workspaceID, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	// Bearer rather than the cookie: the backend renews cookie sessions with a
	// Set-Cookie that this server-side call would silently drop.
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
		return errNotFound
	case resp.StatusCode != http.StatusOK:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GET %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
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

func (c *multicaClient) project(ctx context.Context, token, workspaceID, projectID string) (apiProject, error) {
	var p apiProject
	err := c.get(ctx, token, workspaceID, "/api/projects/"+url.PathEscape(projectID), &p)
	return p, err
}

func (c *multicaClient) resources(ctx context.Context, token, workspaceID, projectID string) ([]apiResource, error) {
	var out struct {
		Resources []apiResource `json:"resources"`
	}
	err := c.get(ctx, token, workspaceID, "/api/projects/"+url.PathEscape(projectID)+"/resources", &out)
	return out.Resources, err
}
