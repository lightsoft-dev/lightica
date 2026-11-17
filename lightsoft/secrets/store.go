package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var (
	errNoRows       = errors.New("not found")
	errNameConflict = errors.New("name already exists")
)

const schema = `
CREATE TABLE IF NOT EXISTS secrets (
  id               TEXT PRIMARY KEY,
  workspace_id     TEXT NOT NULL,
  name             TEXT NOT NULL,
  description      TEXT NOT NULL DEFAULT '',
  owner_id         TEXT NOT NULL,
  ciphertext       BLOB NOT NULL,
  version          INTEGER NOT NULL,
  created_at       TEXT NOT NULL,
  updated_at       TEXT NOT NULL,
  updated_by       TEXT NOT NULL,
  value_updated_at TEXT NOT NULL,
  UNIQUE (workspace_id, name)
);
CREATE TABLE IF NOT EXISTS grants (
  secret_id      TEXT NOT NULL,
  principal_type TEXT NOT NULL,
  principal_id   TEXT NOT NULL,
  created_by     TEXT NOT NULL,
  created_at     TEXT NOT NULL,
  PRIMARY KEY (secret_id, principal_type, principal_id)
);
CREATE TABLE IF NOT EXISTS enrollments (
  agent_id     TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  token_hash   TEXT NOT NULL UNIQUE,
  enrolled_by  TEXT NOT NULL,
  enrolled_at  TEXT NOT NULL,
  last_used_at TEXT
);
CREATE TABLE IF NOT EXISTS audit (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at      TEXT NOT NULL,
  workspace_id    TEXT NOT NULL,
  secret_id       TEXT,
  secret_name     TEXT,
  secret_owner_id TEXT,
  actor_type      TEXT NOT NULL,
  actor_id        TEXT NOT NULL,
  task_id         TEXT,
  action          TEXT NOT NULL,
  result          TEXT NOT NULL,
  detail          TEXT NOT NULL DEFAULT '{}',
  tailscale_login TEXT NOT NULL DEFAULT '',
  remote_addr     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS audit_ws_id ON audit (workspace_id, id);
CREATE INDEX IF NOT EXISTS audit_ws_actor ON audit (workspace_id, actor_type, actor_id, id);
`

type store struct {
	db *sql.DB
}

func openStore(path string) (*store, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection serializes writes; the team-sized load does not need more,
	// and it rules out SQLITE_BUSY between our own connections.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &store{db: db}, nil
}

func (s *store) Close() error { return s.db.Close() }

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

type grant struct {
	PrincipalType string `json:"principal_type"`
	PrincipalID   string `json:"principal_id"`
}

type secretRow struct {
	ID             string
	WorkspaceID    string
	Name           string
	Description    string
	OwnerID        string
	Ciphertext     []byte
	Version        int
	CreatedAt      string
	UpdatedAt      string
	UpdatedBy      string
	ValueUpdatedAt string
	Grants         []grant
}

const secretCols = `id, workspace_id, name, description, owner_id, ciphertext, version, created_at, updated_at, updated_by, value_updated_at`

func scanSecret(sc interface{ Scan(...any) error }) (secretRow, error) {
	var r secretRow
	err := sc.Scan(&r.ID, &r.WorkspaceID, &r.Name, &r.Description, &r.OwnerID, &r.Ciphertext, &r.Version,
		&r.CreatedAt, &r.UpdatedAt, &r.UpdatedBy, &r.ValueUpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, errNoRows
	}
	return r, err
}

func (s *store) listSecrets(ctx context.Context, workspaceID string) ([]secretRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+secretCols+` FROM secrets WHERE workspace_id = ? ORDER BY name`, workspaceID)
	if err != nil {
		return nil, err
	}
	var out []secretRow
	for rows.Next() {
		r, err := scanSecret(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	grants, err := s.grantsForWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Grants = grants[out[i].ID]
	}
	return out, nil
}

func (s *store) grantsForWorkspace(ctx context.Context, workspaceID string) (map[string][]grant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.secret_id, g.principal_type, g.principal_id FROM grants g
		JOIN secrets s ON s.id = g.secret_id
		WHERE s.workspace_id = ? ORDER BY g.principal_type, g.principal_id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]grant{}
	for rows.Next() {
		var id string
		var g grant
		if err := rows.Scan(&id, &g.PrincipalType, &g.PrincipalID); err != nil {
			return nil, err
		}
		out[id] = append(out[id], g)
	}
	return out, rows.Err()
}

func (s *store) getSecret(ctx context.Context, workspaceID, id string) (secretRow, error) {
	r, err := scanSecret(s.db.QueryRowContext(ctx, `SELECT `+secretCols+` FROM secrets WHERE workspace_id = ? AND id = ?`, workspaceID, id))
	if err != nil {
		return r, err
	}
	r.Grants, err = s.grantsFor(ctx, r.ID)
	return r, err
}

func (s *store) getSecretByName(ctx context.Context, workspaceID, name string) (secretRow, error) {
	r, err := scanSecret(s.db.QueryRowContext(ctx, `SELECT `+secretCols+` FROM secrets WHERE workspace_id = ? AND name = ?`, workspaceID, name))
	if err != nil {
		return r, err
	}
	r.Grants, err = s.grantsFor(ctx, r.ID)
	return r, err
}

func (s *store) grantsFor(ctx context.Context, secretID string) ([]grant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT principal_type, principal_id FROM grants WHERE secret_id = ? ORDER BY principal_type, principal_id`, secretID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []grant
	for rows.Next() {
		var g grant
		if err := rows.Scan(&g.PrincipalType, &g.PrincipalID); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// The mutating methods below write the secret change and its audit event in
// one transaction: a change is never stored without its history row.

func (s *store) createSecret(ctx context.Context, r secretRow, ev auditEvent) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO secrets (`+secretCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			r.ID, r.WorkspaceID, r.Name, r.Description, r.OwnerID, r.Ciphertext, r.Version,
			r.CreatedAt, r.UpdatedAt, r.UpdatedBy, r.ValueUpdatedAt)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				return errNameConflict
			}
			return err
		}
		if err := insertGrants(ctx, tx, r.ID, r.Grants, r.OwnerID, r.CreatedAt); err != nil {
			return err
		}
		return insertAudit(ctx, tx, ev)
	})
}

func (s *store) updateSecret(ctx context.Context, r secretRow, ev auditEvent) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE secrets SET description = ?, ciphertext = ?, version = ?, updated_at = ?, updated_by = ?, value_updated_at = ?
			WHERE id = ? AND workspace_id = ?`,
			r.Description, r.Ciphertext, r.Version, r.UpdatedAt, r.UpdatedBy, r.ValueUpdatedAt, r.ID, r.WorkspaceID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errNoRows
		}
		return insertAudit(ctx, tx, ev)
	})
}

func (s *store) replaceGrants(ctx context.Context, r secretRow, by string, ev auditEvent) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM grants WHERE secret_id = ?`, r.ID); err != nil {
			return err
		}
		ts := now()
		if err := insertGrants(ctx, tx, r.ID, r.Grants, by, ts); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE secrets SET updated_at = ?, updated_by = ? WHERE id = ?`, ts, by, r.ID); err != nil {
			return err
		}
		return insertAudit(ctx, tx, ev)
	})
}

func (s *store) deleteSecret(ctx context.Context, workspaceID, id string, ev auditEvent) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM secrets WHERE id = ? AND workspace_id = ?`, id, workspaceID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errNoRows
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM grants WHERE secret_id = ?`, id); err != nil {
			return err
		}
		return insertAudit(ctx, tx, ev)
	})
}

func insertGrants(ctx context.Context, tx *sql.Tx, secretID string, grants []grant, by, ts string) error {
	for _, g := range grants {
		if _, err := tx.ExecContext(ctx, `INSERT INTO grants (secret_id, principal_type, principal_id, created_by, created_at) VALUES (?,?,?,?,?)`,
			secretID, g.PrincipalType, g.PrincipalID, by, ts); err != nil {
			return err
		}
	}
	return nil
}

func (s *store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ── enrollments ────────────────────────────────────────────────────────────

type enrollment struct {
	AgentID     string  `json:"agent_id"`
	WorkspaceID string  `json:"-"`
	EnrolledBy  string  `json:"enrolled_by"`
	EnrolledAt  string  `json:"enrolled_at"`
	LastUsedAt  *string `json:"last_used_at"`
}

func (s *store) upsertEnrollment(ctx context.Context, e enrollment, tokenHash string, ev auditEvent) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO enrollments (agent_id, workspace_id, token_hash, enrolled_by, enrolled_at, last_used_at)
			VALUES (?,?,?,?,?,NULL)
			ON CONFLICT (agent_id) DO UPDATE SET workspace_id = excluded.workspace_id, token_hash = excluded.token_hash,
			  enrolled_by = excluded.enrolled_by, enrolled_at = excluded.enrolled_at, last_used_at = NULL`,
			e.AgentID, e.WorkspaceID, tokenHash, e.EnrolledBy, e.EnrolledAt); err != nil {
			return err
		}
		return insertAudit(ctx, tx, ev)
	})
}

func (s *store) deleteEnrollment(ctx context.Context, workspaceID, agentID string, ev auditEvent) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM enrollments WHERE agent_id = ? AND workspace_id = ?`, agentID, workspaceID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errNoRows
		}
		return insertAudit(ctx, tx, ev)
	})
}

func (s *store) listEnrollments(ctx context.Context, workspaceID string) ([]enrollment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT agent_id, workspace_id, enrolled_by, enrolled_at, last_used_at FROM enrollments WHERE workspace_id = ? ORDER BY enrolled_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []enrollment{}
	for rows.Next() {
		var e enrollment
		if err := rows.Scan(&e.AgentID, &e.WorkspaceID, &e.EnrolledBy, &e.EnrolledAt, &e.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *store) enrollmentByToken(ctx context.Context, token string) (enrollment, error) {
	var e enrollment
	err := s.db.QueryRowContext(ctx, `SELECT agent_id, workspace_id, enrolled_by, enrolled_at, last_used_at FROM enrollments WHERE token_hash = ?`, hashToken(token)).
		Scan(&e.AgentID, &e.WorkspaceID, &e.EnrolledBy, &e.EnrolledAt, &e.LastUsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return e, errNoRows
	}
	return e, err
}

func (s *store) touchEnrollment(ctx context.Context, agentID string) {
	_, _ = s.db.ExecContext(ctx, `UPDATE enrollments SET last_used_at = ? WHERE agent_id = ?`, now(), agentID)
}

// ── audit ──────────────────────────────────────────────────────────────────

const (
	actionCreate    = "secret.create"
	actionUpdate    = "secret.update"
	actionDelete    = "secret.delete"
	actionGrants    = "secret.grants"
	actionReveal    = "secret.reveal" // a person viewed the value in the team client
	actionRead      = "secret.read"   // an agent fetched the value
	actionEnroll    = "agent.enroll"
	actionUnenroll  = "agent.unenroll"
	resultOK        = "ok"
	resultDenied    = "denied"
	actorTypeMember = "member"
	actorTypeAgent  = "agent"
)

type auditEvent struct {
	ID             string         `json:"id"`
	CreatedAt      string         `json:"created_at"`
	WorkspaceID    string         `json:"-"`
	SecretID       *string        `json:"secret_id"`
	SecretName     *string        `json:"secret_name"`
	SecretOwnerID  *string        `json:"-"`
	ActorType      string         `json:"actor_type"`
	ActorID        string         `json:"actor_id"`
	TaskID         *string        `json:"task_id"`
	Action         string         `json:"action"`
	Result         string         `json:"result"`
	Detail         map[string]any `json:"detail"`
	TailscaleLogin string         `json:"-"`
	RemoteAddr     string         `json:"-"`
}

func insertAudit(ctx context.Context, ex interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, ev auditEvent) error {
	if ev.CreatedAt == "" {
		ev.CreatedAt = now()
	}
	detail := []byte("{}")
	if len(ev.Detail) > 0 {
		var err error
		if detail, err = json.Marshal(ev.Detail); err != nil {
			return err
		}
	}
	_, err := ex.ExecContext(ctx, `INSERT INTO audit (created_at, workspace_id, secret_id, secret_name, secret_owner_id, actor_type, actor_id, task_id, action, result, detail, tailscale_login, remote_addr)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		ev.CreatedAt, ev.WorkspaceID, ev.SecretID, ev.SecretName, ev.SecretOwnerID, ev.ActorType, ev.ActorID, ev.TaskID,
		ev.Action, ev.Result, string(detail), ev.TailscaleLogin, ev.RemoteAddr)
	if err != nil {
		return fmt.Errorf("write audit event: %w", err)
	}
	return nil
}

func (s *store) audit(ctx context.Context, ev auditEvent) error {
	return insertAudit(ctx, s.db, ev)
}

// auditScope limits which events a viewer may list. A nil scope means the
// whole workspace (owner/admin).
type auditScope struct {
	userID      string
	ownedAgents []string
}

type auditFilter struct {
	SecretID  string
	ActorType string
	ActorID   string
	Action    string
	Result    string
	Before    int64
	Limit     int
}

func scopeClause(scope *auditScope) (string, []any) {
	if scope == nil {
		return "", nil
	}
	clause := ` AND (secret_owner_id = ? OR (actor_type = 'member' AND actor_id = ?)`
	args := []any{scope.userID, scope.userID}
	if len(scope.ownedAgents) > 0 {
		clause += ` OR (actor_type = 'agent' AND actor_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(scope.ownedAgents)), ",") + `))`
		for _, id := range scope.ownedAgents {
			args = append(args, id)
		}
	}
	return clause + `)`, args
}

func (s *store) listAudit(ctx context.Context, workspaceID string, f auditFilter, scope *auditScope) ([]auditEvent, error) {
	q := `SELECT id, created_at, secret_id, secret_name, actor_type, actor_id, task_id, action, result, detail FROM audit WHERE workspace_id = ?`
	args := []any{workspaceID}
	add := func(col, v string) {
		if v != "" {
			q += ` AND ` + col + ` = ?`
			args = append(args, v)
		}
	}
	add("secret_id", f.SecretID)
	add("actor_type", f.ActorType)
	add("actor_id", f.ActorID)
	add("action", f.Action)
	add("result", f.Result)
	if f.Before > 0 {
		q += ` AND id < ?`
		args = append(args, f.Before)
	}
	sc, scArgs := scopeClause(scope)
	q += sc
	args = append(args, scArgs...)
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, f.Limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []auditEvent{}
	for rows.Next() {
		var ev auditEvent
		var id int64
		var detail string
		if err := rows.Scan(&id, &ev.CreatedAt, &ev.SecretID, &ev.SecretName, &ev.ActorType, &ev.ActorID, &ev.TaskID, &ev.Action, &ev.Result, &detail); err != nil {
			return nil, err
		}
		ev.ID = strconv.FormatInt(id, 10)
		ev.Detail = map[string]any{}
		_ = json.Unmarshal([]byte(detail), &ev.Detail)
		out = append(out, ev)
	}
	return out, rows.Err()
}

type agentAccessSummary struct {
	AgentID     string   `json:"agent_id"`
	ReadCount   int      `json:"read_count"`
	DeniedCount int      `json:"denied_count"`
	LastReadAt  *string  `json:"last_read_at"`
	SecretNames []string `json:"secret_names"`
}

func (s *store) agentSummaries(ctx context.Context, workspaceID string, scope *auditScope) ([]agentAccessSummary, error) {
	q := `SELECT actor_id,
			SUM(CASE WHEN result = 'ok' THEN 1 ELSE 0 END),
			SUM(CASE WHEN result = 'denied' THEN 1 ELSE 0 END),
			MAX(CASE WHEN result = 'ok' THEN created_at END),
			GROUP_CONCAT(DISTINCT CASE WHEN result = 'ok' THEN secret_name END)
		FROM audit WHERE workspace_id = ? AND actor_type = 'agent' AND action = 'secret.read'`
	args := []any{workspaceID}
	sc, scArgs := scopeClause(scope)
	q += sc + ` GROUP BY actor_id ORDER BY MAX(id) DESC`
	args = append(args, scArgs...)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []agentAccessSummary{}
	for rows.Next() {
		var a agentAccessSummary
		var names sql.NullString
		if err := rows.Scan(&a.AgentID, &a.ReadCount, &a.DeniedCount, &a.LastReadAt, &names); err != nil {
			return nil, err
		}
		a.SecretNames = []string{}
		if names.Valid && names.String != "" {
			a.SecretNames = strings.Split(names.String, ",")
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type lastRead struct {
	At        string
	ActorType string
	ActorID   string
}

func (s *store) lastReads(ctx context.Context, workspaceID string) (map[string]lastRead, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT secret_id, created_at, actor_type, actor_id FROM audit WHERE id IN (
		  SELECT MAX(id) FROM audit
		  WHERE workspace_id = ? AND secret_id IS NOT NULL AND action IN ('secret.read', 'secret.reveal') AND result = 'ok'
		  GROUP BY secret_id)`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]lastRead{}
	for rows.Next() {
		var id string
		var lr lastRead
		if err := rows.Scan(&id, &lr.At, &lr.ActorType, &lr.ActorID); err != nil {
			return nil, err
		}
		out[id] = lr
	}
	return out, rows.Err()
}
