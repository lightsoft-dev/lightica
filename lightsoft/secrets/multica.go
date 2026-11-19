package main

import (
	"bytes"
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
	errNotFound        = errors.New("not found")
)

// multicaClient calls the production Multica API with the caller's own
// credential. The service holds no Multica credential of its own, so it can
// only read (and, for enrollment, write) what the caller is allowed to.
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
	Name   string `json:"name"`
}

type apiAgent struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	OwnerID     *string `json:"owner_id"`
	ArchivedAt  *string `json:"archived_at"`
}

type apiSquad struct {
	ID string `json:"id"`
}

type apiSquadMember struct {
	MemberType string `json:"member_type"`
	MemberID   string `json:"member_id"`
}

func newMulticaClient(base string) *multicaClient {
	return &multicaClient{base: base, http: &http.Client{Timeout: 10 * time.Second}}
}

func (c *multicaClient) do(ctx context.Context, method, token, workspaceID, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	// Bearer rather than the cookie: the backend renews cookie sessions with a
	// Set-Cookie this server-side call would drop, and only cookie requests
	// need the browser's CSRF token.
	req.Header.Set("Authorization", "Bearer "+token)
	if workspaceID != "" {
		req.Header.Set("X-Workspace-ID", workspaceID)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errUnauthenticated
	case resp.StatusCode == http.StatusForbidden:
		return errForbidden
	case resp.StatusCode == http.StatusNotFound:
		return errNotFound
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *multicaClient) me(ctx context.Context, token string) (apiUser, error) {
	var u apiUser
	err := c.do(ctx, http.MethodGet, token, "", "/api/me", nil, &u)
	return u, err
}

func (c *multicaClient) members(ctx context.Context, token, workspaceID string) ([]apiMember, error) {
	var m []apiMember
	err := c.do(ctx, http.MethodGet, token, workspaceID, "/api/workspaces/"+url.PathEscape(workspaceID)+"/members", nil, &m)
	return m, err
}

func (c *multicaClient) agents(ctx context.Context, token, workspaceID string) ([]apiAgent, error) {
	var a []apiAgent
	err := c.do(ctx, http.MethodGet, token, workspaceID, "/api/agents?include_archived=true", nil, &a)
	return a, err
}

func (c *multicaClient) agent(ctx context.Context, token, workspaceID, agentID string) (apiAgent, error) {
	var a apiAgent
	err := c.do(ctx, http.MethodGet, token, workspaceID, "/api/agents/"+url.PathEscape(agentID), nil, &a)
	if err == nil && a.WorkspaceID != workspaceID {
		return apiAgent{}, errNotFound
	}
	return a, err
}

func (c *multicaClient) squads(ctx context.Context, token, workspaceID string) ([]apiSquad, error) {
	var s []apiSquad
	err := c.do(ctx, http.MethodGet, token, workspaceID, "/api/squads", nil, &s)
	return s, err
}

func (c *multicaClient) squadMembers(ctx context.Context, token, workspaceID, squadID string) ([]apiSquadMember, error) {
	var m []apiSquadMember
	err := c.do(ctx, http.MethodGet, token, workspaceID, "/api/squads/"+url.PathEscape(squadID)+"/members", nil, &m)
	return m, err
}

func (c *multicaClient) agentEnv(ctx context.Context, token, workspaceID, agentID string) (map[string]string, error) {
	var out struct {
		CustomEnv map[string]string `json:"custom_env"`
	}
	err := c.do(ctx, http.MethodGet, token, workspaceID, "/api/agents/"+url.PathEscape(agentID)+"/env", nil, &out)
	if out.CustomEnv == nil {
		out.CustomEnv = map[string]string{}
	}
	return out.CustomEnv, err
}

func (c *multicaClient) putAgentEnv(ctx context.Context, token, workspaceID, agentID string, env map[string]string) error {
	return c.do(ctx, http.MethodPut, token, workspaceID, "/api/agents/"+url.PathEscape(agentID)+"/env",
		map[string]any{"custom_env": env}, nil)
}
