package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"golang.org/x/crypto/bcrypt"
)

// Account rules, shared by the signup handler and the demo seeder.
const (
	minUsernameLength = 3
	minPasswordLength = 8
)

type Server struct {
	cfg   *Config
	store *Store
	wa    *webauthn.WebAuthn
	log   *slog.Logger
}

func NewServer(cfg *Config, log *slog.Logger) (*Server, error) {
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          cfg.RPID,
		RPDisplayName: cfg.RPName,
		RPOrigins:     cfg.RPOrigins,
	})
	if err != nil {
		return nil, err
	}
	store, err := NewStore(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, store: store, wa: wa, log: log}, nil
}

func (s *Server) Close() error { return s.store.Close() }

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, map[string]any{"ok": true}) })
	mux.HandleFunc("GET /.well-known/assetlinks.json", s.handleAssetLinks)

	mux.HandleFunc("POST /api/signup", s.handleSignup)
	mux.HandleFunc("POST /api/login/password", s.handlePasswordLogin)
	mux.HandleFunc("POST /api/logout", s.authed(s.handleLogout))
	mux.HandleFunc("GET /api/me", s.authed(s.handleMe))

	mux.HandleFunc("POST /api/passkey/register/begin", s.authed(s.handlePasskeyRegisterBegin))
	mux.HandleFunc("POST /api/passkey/register/finish", s.authed(s.handlePasskeyRegisterFinish))
	mux.HandleFunc("POST /api/passkey/login/begin", s.handlePasskeyLoginBegin)
	mux.HandleFunc("POST /api/passkey/login/finish", s.handlePasskeyLoginFinish)

	mux.HandleFunc("POST /api/restore/register/begin", s.authed(s.handleRestoreRegisterBegin))
	mux.HandleFunc("POST /api/restore/register/finish", s.authed(s.handleRestoreRegisterFinish))
	mux.HandleFunc("POST /api/restore/revoke", s.authed(s.handleRestoreRevoke))
	mux.HandleFunc("POST /api/restore/login/begin", s.handleRestoreLoginBegin)
	mux.HandleFunc("POST /api/restore/login/finish", s.handleRestoreLoginFinish)

	return s.withLogging(mux)
}

// ---------------------------------------------------------------- middleware

// authed resolves the bearer token once and hands the caller both the session
// and its user, so no handler has to re-parse the header.
func (s *Server) authed(next func(http.ResponseWriter, *http.Request, *User, *AuthSession)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		sess, err := s.store.SessionByToken(strings.TrimSpace(token))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired session")
			return
		}
		user, err := s.store.UserByID(sess.UserID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unknown user")
			return
		}
		next(w, r, user, sess)
	}
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		// The store panics on database errors, which are bugs or a broken
		// DB_PATH rather than something a handler can report. Catch them here
		// so the client gets a 500 and the failure reaches the log.
		defer func() {
			if v := recover(); v != nil {
				s.log.Error("panic", "method", r.Method, "path", r.URL.Path, "value", v)
				writeError(rec, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(rec, r)
		s.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "duration", time.Since(start).Round(time.Millisecond))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// ------------------------------------------------------------ asset links

func (s *Server) handleAssetLinks(w http.ResponseWriter, r *http.Request) {
	statement := []map[string]any{{
		// get_login_creds is what Credential Manager checks before it lets the
		// app create or use passkeys and Restore Keys for this RP ID.
		"relation": []string{
			"delegate_permission/common.handle_all_urls",
			"delegate_permission/common.get_login_creds",
		},
		"target": map[string]any{
			"namespace":                "android_app",
			"package_name":             s.cfg.AndroidPackageName,
			"sha256_cert_fingerprints": s.cfg.AndroidCertFingerprints,
		},
	}}
	writeJSON(w, http.StatusOK, statement)
}

// ------------------------------------------------------------ password auth

type credentialsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type userView struct {
	Username    string           `json:"username"`
	DisplayName string           `json:"displayName"`
	Credentials []credentialView `json:"credentials"`
}

type credentialView struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"createdAt"`
}

type authResponse struct {
	Token  string   `json:"token"`
	Method string   `json:"method"`
	User   userView `json:"user"`

	// Set only by the restore flow: the single-use key that was just spent.
	RevokedCredentialID string `json:"revokedCredentialId,omitempty"`
}

// registerResponse is the reply to both register/finish endpoints.
type registerResponse struct {
	CredentialID string   `json:"credentialId"`
	User         userView `json:"user"`
}

func (s *Server) viewOf(u *User) userView {
	view := userView{Username: u.Username, DisplayName: u.DisplayName}
	for _, c := range u.credentials {
		view.Credentials = append(view.Credentials, credentialView{ID: c.ID(), Kind: string(c.Kind), CreatedAt: c.CreatedAt})
	}
	return view
}

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) < minUsernameLength || len(req.Password) < minPasswordLength {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"username must be at least %d characters and password at least %d",
			minUsernameLength, minPasswordLength,
		))
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not hash password")
		return
	}

	user, err := s.store.CreateUser(req.Username, req.Username, hash)
	if errors.Is(err, ErrAlreadyExists) {
		writeError(w, http.StatusConflict, "username already taken")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	sess := s.store.IssueSession(user, "password", s.cfg.SessionTTL)
	writeJSON(w, http.StatusOK, authResponse{Token: sess.Token, Method: sess.Method, User: s.viewOf(user)})
}

func (s *Server) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	user, err := s.store.UserByName(strings.TrimSpace(req.Username))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if bcrypt.CompareHashAndPassword(user.PasswordHash, []byte(req.Password)) != nil {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	sess := s.store.IssueSession(user, "password", s.cfg.SessionTTL)
	writeJSON(w, http.StatusOK, authResponse{Token: sess.Token, Method: sess.Method, User: s.viewOf(user)})
}

// SeedDemoUser creates the configured demo account, if any. It is a no-op when
// seeding is disabled, and safe to call on a store that already has the user.
func (s *Server) SeedDemoUser() error {
	if s.cfg.SeedDemoUsername == "" {
		return nil
	}
	if _, err := s.store.UserByName(s.cfg.SeedDemoUsername); err == nil {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(s.cfg.SeedDemoPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hashing the demo password: %w", err)
	}
	if _, err = s.store.CreateUser(s.cfg.SeedDemoUsername, s.cfg.SeedDemoUsername, hash); err != nil {
		return fmt.Errorf("creating the demo user: %w", err)
	}

	// Logged at WARN, and with the password in plain sight, because that is the
	// point: it is a well-known account on a demo server.
	s.log.Warn("seeded demo account",
		"username", s.cfg.SeedDemoUsername,
		"password", s.cfg.SeedDemoPassword,
	)
	return nil
}

func (s *Server) handleLogout(w http.ResponseWriter, _ *http.Request, _ *User, sess *AuthSession) {
	s.store.RevokeSession(sess.Token)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, _ *http.Request, u *User, _ *AuthSession) {
	writeJSON(w, http.StatusOK, s.viewOf(u))
}

// ----------------------------------------------------------------- helpers

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "malformed JSON body: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
