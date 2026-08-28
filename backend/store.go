package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"modernc.org/sqlite"
)

// CredentialKind separates ordinary user-facing passkeys from the
// system-managed Restore Keys. Restore Keys are never shown in credential
// management UIs and are single use: the server deletes them as soon as one is
// redeemed.
type CredentialKind string

const (
	KindPasskey CredentialKind = "passkey"
	KindRestore CredentialKind = "restore"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
)

// StoredCredential is a WebAuthn credential plus the demo specific metadata.
type StoredCredential struct {
	Credential webauthn.Credential
	Kind       CredentialKind
	CreatedAt  time.Time
}

// ID is the base64url form of the raw credential id, which is also the primary
// key of the credentials table.
func (c *StoredCredential) ID() string { return credentialKey(c.Credential.ID) }

func credentialKey(rawID []byte) string { return base64.RawURLEncoding.EncodeToString(rawID) }

// User implements webauthn.User. Credentials are loaded with the user, so the
// WebAuthn callbacks below never have to reach back into the database.
type User struct {
	ID           []byte
	Username     string
	DisplayName  string
	PasswordHash []byte
	CreatedAt    time.Time

	credentials []*StoredCredential
}

func (u *User) WebAuthnID() []byte          { return u.ID }
func (u *User) WebAuthnName() string        { return u.Username }
func (u *User) WebAuthnDisplayName() string { return u.DisplayName }

func (u *User) WebAuthnCredentials() []webauthn.Credential {
	out := make([]webauthn.Credential, 0, len(u.credentials))
	for _, c := range u.credentials {
		out = append(out, c.Credential)
	}
	return out
}

func (u *User) CredentialsOfKind(kind CredentialKind) []*StoredCredential {
	var out []*StoredCredential
	for _, c := range u.credentials {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

// Ceremony is a pending WebAuthn registration or authentication. The client
// gets an opaque id back and echoes it in the finish call, which keeps the
// challenge server side without needing cookies.
type Ceremony struct {
	ID        string
	Kind      CredentialKind
	UserID    []byte
	Session   webauthn.SessionData
	ExpiresAt time.Time
}

// AuthSession is an issued bearer token.
type AuthSession struct {
	Token     string
	UserID    []byte
	Method    string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id            BLOB PRIMARY KEY,
	username      TEXT NOT NULL UNIQUE,
	display_name  TEXT NOT NULL,
	password_hash BLOB NOT NULL,
	created_at    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS credentials (
	id         TEXT PRIMARY KEY,
	user_id    BLOB NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	kind       TEXT NOT NULL,
	credential TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS credentials_by_user ON credentials(user_id, created_at);
CREATE TABLE IF NOT EXISTS ceremonies (
	id         TEXT PRIMARY KEY,
	kind       TEXT NOT NULL,
	user_id    BLOB,
	session    TEXT NOT NULL,
	expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
	token      TEXT PRIMARY KEY,
	user_id    BLOB NOT NULL,
	method     TEXT NOT NULL,
	issued_at  INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
);
`

// Store keeps every piece of demo state in SQLite.
type Store struct {
	db *sql.DB
}

// NewStore opens the database at path and creates the schema if needed. Use
// ":memory:" for a throwaway database.
func NewStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+
		"?_pragma=journal_mode(WAL)"+ // survive a kill without a fsync per write
		"&_pragma=synchronous(NORMAL)"+
		"&_pragma=busy_timeout(5000)"+
		"&_pragma=foreign_keys(1)") // otherwise ON DELETE CASCADE is inert
	if err != nil {
		return nil, err
	}
	// One connection: it keeps ":memory:" a single database, it serialises the
	// few multi-statement operations below so they need no transaction, and it
	// takes the place of the mutex an in-memory store would need.
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(schema); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// must panics on a database error. The schema is fixed and the only writer is
// this file, so the callers below have nothing useful to do with one; the
// recover in withLogging turns it into a logged 500.
func must(err error) {
	if err != nil {
		panic(err)
	}
}

func (s *Store) exec(query string, args ...any) sql.Result {
	res, err := s.db.Exec(query, args...)
	must(err)
	return res
}

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	must(err)
	return string(raw)
}

func randomToken(n int) string {
	return base64.RawURLEncoding.EncodeToString(randomBytes(n))
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

// ------------------------------------------------------------------- users

func (s *Store) CreateUser(username, displayName string, passwordHash []byte) (*User, error) {
	u := &User{
		ID:           randomBytes(32),
		Username:     username,
		DisplayName:  displayName,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now(),
	}
	_, err := s.db.Exec(
		`INSERT INTO users (id, username, display_name, password_hash, created_at) VALUES (?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.DisplayName, u.PasswordHash, u.CreatedAt.UnixMilli(),
	)
	// SQLITE_CONSTRAINT_UNIQUE: the username is taken. Anything else is a bug.
	var serr *sqlite.Error
	if errors.As(err, &serr) && serr.Code() == 2067 {
		return nil, ErrAlreadyExists
	}
	must(err)
	return u, nil
}

func (s *Store) UserByName(username string) (*User, error) {
	return s.userWhere("username = ?", username)
}

func (s *Store) UserByID(id []byte) (*User, error) {
	return s.userWhere("id = ?", id)
}

func (s *Store) UserByCredentialID(credentialID []byte) (*User, error) {
	return s.userWhere("id = (SELECT user_id FROM credentials WHERE id = ?)", credentialKey(credentialID))
}

// userWhere runs the one user query there is. The condition is always a
// literal from the three callers above, never anything a request supplies.
func (s *Store) userWhere(where string, arg any) (*User, error) {
	u := &User{}
	var createdAt int64
	err := s.db.QueryRow(
		`SELECT id, username, display_name, password_hash, created_at FROM users WHERE `+where, arg,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	must(err)

	u.CreatedAt = time.UnixMilli(createdAt)
	s.reload(u)
	return u, nil
}

// ------------------------------------------------------------- credentials

// reload refreshes the credentials cached on the user. Every mutation below
// ends with it, so the slice is a projection of the table rather than a second
// copy kept in step by hand.
func (s *Store) reload(u *User) {
	rows, err := s.db.Query(
		`SELECT kind, credential, created_at FROM credentials WHERE user_id = ? ORDER BY created_at`, u.ID)
	must(err)
	defer rows.Close()

	u.credentials = nil
	for rows.Next() {
		c := &StoredCredential{}
		var raw string
		var createdAt int64
		must(rows.Scan(&c.Kind, &raw, &createdAt))
		must(json.Unmarshal([]byte(raw), &c.Credential))
		c.CreatedAt = time.UnixMilli(createdAt)
		u.credentials = append(u.credentials, c)
	}
	must(rows.Err())
}

// FindCredential looks in the credentials already loaded with the user.
func (s *Store) FindCredential(u *User, credentialID []byte) (*StoredCredential, error) {
	if c := find(u, credentialKey(credentialID)); c != nil {
		return c, nil
	}
	return nil, ErrNotFound
}

func find(u *User, credentialID string) *StoredCredential {
	for _, c := range u.credentials {
		if c.ID() == credentialID {
			return c
		}
	}
	return nil
}

func (s *Store) AddCredential(u *User, cred webauthn.Credential, kind CredentialKind) *StoredCredential {
	s.exec(`INSERT INTO credentials (id, user_id, kind, credential, created_at) VALUES (?, ?, ?, ?, ?)`,
		credentialKey(cred.ID), u.ID, string(kind), mustJSON(cred), time.Now().UnixMilli())
	s.reload(u)
	return find(u, credentialKey(cred.ID))
}

// ReplaceRestoreCredential installs a fresh Restore Key and drops any previous
// one. Android keeps a single Restore Key per package name, so keeping more
// than one server side would only leave dead entries behind.
//
// The two statements are not wrapped in a transaction: the single connection
// set up in NewStore already serialises them, and the worst a crash in between
// can do is leave the account without a Restore Key, which the app re-creates.
func (s *Store) ReplaceRestoreCredential(u *User, cred webauthn.Credential) *StoredCredential {
	s.RevokeCredentialsOfKind(u, KindRestore)
	return s.AddCredential(u, cred, KindRestore)
}

// RevokeCredential removes a credential. Redeeming a Restore Key calls this so
// the key cannot be replayed on a third device.
func (s *Store) RevokeCredential(u *User, credentialID string) bool {
	n, err := s.exec(`DELETE FROM credentials WHERE id = ? AND user_id = ?`, credentialID, u.ID).RowsAffected()
	must(err)
	s.reload(u)
	return n > 0
}

// RevokeCredentialsOfKind drops every credential of one kind and reports what
// it removed.
func (s *Store) RevokeCredentialsOfKind(u *User, kind CredentialKind) []string {
	rows, err := s.db.Query(
		`DELETE FROM credentials WHERE user_id = ? AND kind = ? RETURNING id`, u.ID, string(kind))
	must(err)

	var revoked []string
	for rows.Next() {
		var id string
		must(rows.Scan(&id))
		revoked = append(revoked, id)
	}
	must(rows.Err())
	must(rows.Close()) // the single connection is busy until this returns

	s.reload(u)
	return revoked
}

// TouchCredential records that a credential was just used.
func (s *Store) TouchCredential(c *StoredCredential, signCount uint32) {
	c.Credential.Authenticator.UpdateCounter(signCount)
	s.exec(`UPDATE credentials SET credential = ? WHERE id = ?`, mustJSON(c.Credential), c.ID())
}

// -------------------------------------------------------------- ceremonies

func (s *Store) PutCeremony(kind CredentialKind, userID []byte, session *webauthn.SessionData, ttl time.Duration) *Ceremony {
	c := &Ceremony{
		ID:        randomToken(16),
		Kind:      kind,
		UserID:    userID,
		Session:   *session,
		ExpiresAt: time.Now().Add(ttl),
	}
	s.exec(`INSERT INTO ceremonies (id, kind, user_id, session, expires_at) VALUES (?, ?, ?, ?, ?)`,
		c.ID, string(c.Kind), c.UserID, mustJSON(c.Session), c.ExpiresAt.UnixMilli())
	return c
}

// TakeCeremony consumes a pending ceremony; challenges are single use.
func (s *Store) TakeCeremony(id string) (*Ceremony, error) {
	c := &Ceremony{ID: id}
	var raw string
	var expiresAt int64
	err := s.db.QueryRow(`DELETE FROM ceremonies WHERE id = ? RETURNING kind, user_id, session, expires_at`, id).
		Scan(&c.Kind, &c.UserID, &raw, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	must(err)

	c.ExpiresAt = time.UnixMilli(expiresAt)
	if time.Now().After(c.ExpiresAt) {
		return nil, ErrNotFound
	}
	must(json.Unmarshal([]byte(raw), &c.Session))
	return c, nil
}

// ---------------------------------------------------------------- sessions

func (s *Store) IssueSession(u *User, method string, ttl time.Duration) *AuthSession {
	now := time.Now()
	sess := &AuthSession{
		Token:     randomToken(32),
		UserID:    u.ID,
		Method:    method,
		IssuedAt:  now,
		ExpiresAt: now.Add(ttl),
	}
	s.exec(`INSERT INTO sessions (token, user_id, method, issued_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		sess.Token, sess.UserID, sess.Method, sess.IssuedAt.UnixMilli(), sess.ExpiresAt.UnixMilli())
	return sess
}

// SessionByToken reports an expired session as missing and leaves the row for
// GC to sweep.
func (s *Store) SessionByToken(token string) (*AuthSession, error) {
	sess := &AuthSession{Token: token}
	var issuedAt, expiresAt int64
	err := s.db.QueryRow(`SELECT user_id, method, issued_at, expires_at FROM sessions WHERE token = ?`, token).
		Scan(&sess.UserID, &sess.Method, &issuedAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	must(err)

	sess.IssuedAt, sess.ExpiresAt = time.UnixMilli(issuedAt), time.UnixMilli(expiresAt)
	if time.Now().After(sess.ExpiresAt) {
		return nil, ErrNotFound
	}
	return sess, nil
}

func (s *Store) RevokeSession(token string) {
	s.exec(`DELETE FROM sessions WHERE token = ?`, token)
}

// GC drops expired ceremonies and sessions.
func (s *Store) GC() {
	now := time.Now().UnixMilli()
	s.exec(`DELETE FROM ceremonies WHERE expires_at < ?`, now)
	s.exec(`DELETE FROM sessions WHERE expires_at < ?`, now)
}
