package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	providerClaude = "claude"
	providerCodex  = "codex"

	// defaultClaudeKeychainService is the login keychain item Claude Code
	// keeps its subscription OAuth credential in.
	defaultClaudeKeychainService = "Claude Code-credentials"
)

var (
	uuidPattern      = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	accountIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
)

// AccountsFile is accounts.json: the subscription accounts this Mac's agent
// CLIs run under, and the workspace whose members may see them.
type AccountsFile struct {
	WorkspaceID string    `json:"workspace_id"`
	Accounts    []Account `json:"accounts"`
}

// Account is one subscription login. Several runtimes can share it (two
// daemons on one Mac reading the same ~/.claude), which is why limits are
// tracked per account rather than per runtime.
type Account struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	// CodexHome is the CODEX_HOME the account's Codex runs with (codex only).
	CodexHome string `json:"codex_home,omitempty"`
	// KeychainService overrides the keychain item name (claude only).
	KeychainService string `json:"keychain_service,omitempty"`
	// RuntimeIDs are the Multica runtimes that run under this account; the
	// page names them so readers know which agents draw on it.
	RuntimeIDs []string `json:"runtime_ids"`
}

func loadAccounts(path string) (AccountsFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return AccountsFile{}, fmt.Errorf("read accounts: %w", err)
	}
	var f AccountsFile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return AccountsFile{}, fmt.Errorf("parse %s: %w", path, err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return AccountsFile{}, err
	}
	if err := f.normalize(home); err != nil {
		return AccountsFile{}, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

func (f *AccountsFile) normalize(home string) error {
	if !uuidPattern.MatchString(f.WorkspaceID) {
		return errors.New("workspace_id must be a workspace UUID")
	}
	f.WorkspaceID = strings.ToLower(f.WorkspaceID)
	seen := map[string]bool{}
	for i := range f.Accounts {
		a := &f.Accounts[i]
		if !accountIDPattern.MatchString(a.ID) {
			return fmt.Errorf("account %d: id must match %s", i, accountIDPattern)
		}
		if seen[a.ID] {
			return fmt.Errorf("account %q: duplicate id", a.ID)
		}
		seen[a.ID] = true
		a.Name = strings.TrimSpace(a.Name)
		if a.Name == "" {
			a.Name = a.ID
		}
		switch a.Provider {
		case providerClaude:
			if a.CodexHome != "" {
				return fmt.Errorf("account %q: codex_home is for codex accounts", a.ID)
			}
			if a.KeychainService == "" {
				a.KeychainService = defaultClaudeKeychainService
			}
		case providerCodex:
			if a.KeychainService != "" {
				return fmt.Errorf("account %q: keychain_service is for claude accounts", a.ID)
			}
			if a.CodexHome == "" {
				return fmt.Errorf("account %q: codex_home is required", a.ID)
			}
			a.CodexHome = expandHome(a.CodexHome, home)
			if !filepath.IsAbs(a.CodexHome) {
				return fmt.Errorf("account %q: codex_home must be absolute or start with ~/", a.ID)
			}
		default:
			return fmt.Errorf("account %q: provider must be %q or %q", a.ID, providerClaude, providerCodex)
		}
		if a.RuntimeIDs == nil {
			a.RuntimeIDs = []string{}
		}
		for _, id := range a.RuntimeIDs {
			if !uuidPattern.MatchString(id) {
				return fmt.Errorf("account %q: runtime id %q is not a UUID", a.ID, id)
			}
		}
	}
	return nil
}

func expandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}
