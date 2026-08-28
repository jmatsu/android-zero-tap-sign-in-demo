package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// beginResponse is what the app feeds straight into Credential Manager.
// RequestJSON is the raw PublicKeyCredentialCreationOptionsJSON /
// PublicKeyCredentialRequestOptionsJSON object; the app re-serialises it and
// passes it as requestJson.
type beginResponse struct {
	CeremonyID  string          `json:"ceremonyId"`
	RequestJSON json.RawMessage `json:"requestJson"`
}

// finishRequest carries the raw RegistrationResponseJSON /
// AuthenticationResponseJSON produced by Credential Manager.
type finishRequest struct {
	CeremonyID string          `json:"ceremonyId"`
	Credential json.RawMessage `json:"credential"`
}

func (s *Server) beginRegistration(w http.ResponseWriter, u *User, kind CredentialKind) {
	selection := protocol.AuthenticatorSelection{
		ResidentKey:        protocol.ResidentKeyRequirementRequired,
		RequireResidentKey: protocol.ResidentKeyRequired(),
		UserVerification:   protocol.VerificationRequired,
	}
	mediation := protocol.MediationDefault

	opts := []webauthn.RegistrationOption{
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
	}

	if kind == KindRestore {
		// The Restore Key is minted by the system on behalf of the user, with
		// no prompt, so we must not demand user verification. Registering the
		// ceremony as "conditional" mediation is what tells go-webauthn to skip
		// the User Present check when the response comes back; the mediation
		// field itself is never sent to the client.
		selection.UserVerification = protocol.VerificationDiscouraged
		mediation = protocol.MediationConditional
	} else {
		// Only passkeys are excluded: a Restore Key is meant to replace its
		// predecessor, not to be refused as a duplicate.
		opts = append(opts, webauthn.WithExclusions(descriptorsOf(u.CredentialsOfKind(KindPasskey))))
	}
	opts = append(opts, webauthn.WithAuthenticatorSelection(selection))

	creation, session, err := s.wa.BeginMediatedRegistration(u, mediation, opts...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start registration: "+err.Error())
		return
	}

	requestJSON, err := json.Marshal(creation.Response)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ceremony := s.store.PutCeremony(kind, u.ID, session, s.cfg.CeremonyTTL)
	writeJSON(w, http.StatusOK, beginResponse{CeremonyID: ceremony.ID, RequestJSON: requestJSON})
}

func (s *Server) finishRegistration(w http.ResponseWriter, r *http.Request, u *User, kind CredentialKind) *StoredCredential {
	var req finishRequest
	if !decodeJSON(w, r, &req) {
		return nil
	}

	ceremony, err := s.store.TakeCeremony(req.CeremonyID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unknown or expired ceremony")
		return nil
	}
	if ceremony.Kind != kind || !bytes.Equal(ceremony.UserID, u.ID) {
		writeError(w, http.StatusBadRequest, "ceremony does not belong to this request")
		return nil
	}

	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(req.Credential))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not parse registration response: "+describe(err))
		return nil
	}

	credential, err := s.wa.CreateCredential(u, ceremony.Session, parsed)
	if err != nil {
		writeError(w, http.StatusBadRequest, "registration verification failed: "+describe(err))
		return nil
	}

	if kind == KindRestore {
		return s.store.ReplaceRestoreCredential(u, *credential)
	}
	return s.store.AddCredential(u, *credential, kind)
}

// --------------------------------------------------------------- passkeys

func (s *Server) handlePasskeyRegisterBegin(w http.ResponseWriter, _ *http.Request, u *User, _ *AuthSession) {
	s.beginRegistration(w, u, KindPasskey)
}

func (s *Server) handlePasskeyRegisterFinish(w http.ResponseWriter, r *http.Request, u *User, _ *AuthSession) {
	s.registerFinish(w, r, u, KindPasskey, "passkey registered")
}

// beginLogin starts a discoverable ceremony. Neither kind can name a
// credential up front: a passkey sign-in has no username yet, and a restored
// device has no idea who it belongs to.
func (s *Server) beginLogin(w http.ResponseWriter, kind CredentialKind, uv protocol.UserVerificationRequirement) {
	assertion, session, err := s.wa.BeginDiscoverableLogin(webauthn.WithUserVerification(uv))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	requestJSON, err := json.Marshal(assertion.Response)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ceremony := s.store.PutCeremony(kind, nil, session, s.cfg.CeremonyTTL)
	writeJSON(w, http.StatusOK, beginResponse{CeremonyID: ceremony.ID, RequestJSON: requestJSON})
}

// takeAssertion consumes the pending ceremony and parses the response. It
// returns nil after writing the error response, like finishRegistration.
func (s *Server) takeAssertion(w http.ResponseWriter, r *http.Request, kind CredentialKind) (*protocol.ParsedCredentialAssertionData, *Ceremony) {
	var req finishRequest
	if !decodeJSON(w, r, &req) {
		return nil, nil
	}

	ceremony, err := s.store.TakeCeremony(req.CeremonyID)
	if err != nil || ceremony.Kind != kind {
		writeError(w, http.StatusBadRequest, "unknown or expired ceremony")
		return nil, nil
	}

	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(req.Credential))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not parse assertion: "+describe(err))
		return nil, nil
	}
	return parsed, ceremony
}

func (s *Server) handlePasskeyLoginBegin(w http.ResponseWriter, _ *http.Request) {
	s.beginLogin(w, KindPasskey, protocol.VerificationRequired)
}

func (s *Server) handlePasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	parsed, ceremony := s.takeAssertion(w, r, KindPasskey)
	if parsed == nil {
		return
	}

	waUser, credential, err := s.wa.ValidatePasskeyLogin(s.discoverUser, ceremony.Session, parsed)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "passkey verification failed: "+describe(err))
		return
	}

	user := waUser.(*User)
	stored, err := s.store.FindCredential(user, credential.ID)
	if err != nil || stored.Kind != KindPasskey {
		// A Restore Key must not be redeemable through the interactive passkey
		// endpoint; it has its own single-use flow.
		writeError(w, http.StatusUnauthorized, "credential is not a passkey")
		return
	}

	s.store.TouchCredential(stored, credential.Authenticator.SignCount)

	sess := s.store.IssueSession(user, "passkey", s.cfg.SessionTTL)
	s.log.Info("passkey sign-in", "user", user.Username, "credential", stored.ID())
	writeJSON(w, http.StatusOK, authResponse{Token: sess.Token, Method: sess.Method, User: s.viewOf(user)})
}

// ---------------------------------------------------------- Restore Keys

func (s *Server) handleRestoreRegisterBegin(w http.ResponseWriter, _ *http.Request, u *User, _ *AuthSession) {
	s.beginRegistration(w, u, KindRestore)
}

func (s *Server) handleRestoreRegisterFinish(w http.ResponseWriter, r *http.Request, u *User, _ *AuthSession) {
	s.registerFinish(w, r, u, KindRestore, "Restore Key registered")
}

func (s *Server) registerFinish(w http.ResponseWriter, r *http.Request, u *User, kind CredentialKind, logMessage string) {
	stored := s.finishRegistration(w, r, u, kind)
	if stored == nil {
		return
	}
	s.log.Info(logMessage, "user", u.Username, "credential", stored.ID())
	writeJSON(w, http.StatusOK, registerResponse{CredentialID: stored.ID(), User: s.viewOf(u)})
}

// handleRestoreRevoke drops the account's Restore Key. The app calls this when
// the user signs out, at the same time it clears the local half of the key.
func (s *Server) handleRestoreRevoke(w http.ResponseWriter, _ *http.Request, u *User, _ *AuthSession) {
	revoked := s.store.RevokeCredentialsOfKind(u, KindRestore)
	s.log.Info("Restore Keys revoked", "user", u.Username, "credentials", revoked)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRestoreLoginBegin(w http.ResponseWriter, _ *http.Request) {
	s.beginLogin(w, KindRestore, protocol.VerificationDiscouraged)
}

// handleRestoreLoginFinish verifies a restore-key assertion and then destroys
// the key. Verification is deliberately not routed through
// webauthn.ValidatePasskeyLogin: that helper hard-codes a User Present check,
// and a zero-tap sign-in has no user gesture behind it.
func (s *Server) handleRestoreLoginFinish(w http.ResponseWriter, r *http.Request) {
	parsed, ceremony := s.takeAssertion(w, r, KindRestore)
	if parsed == nil {
		return
	}

	waUser, err := s.discoverUser(parsed.RawID, parsed.Response.UserHandle)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unknown Restore Key")
		return
	}
	user := waUser.(*User)

	stored, err := s.store.FindCredential(user, parsed.RawID)
	if err != nil || stored.Kind != KindRestore {
		writeError(w, http.StatusUnauthorized, "credential is not a Restore Key")
		return
	}

	err = parsed.Verify(
		ceremony.Session.Challenge,
		s.cfg.RPID,
		"", // no legacy FIDO AppID
		s.cfg.RPOrigins,
		nil, // no top origins: this is never a cross-origin ceremony
		protocol.TopOriginExplicitVerificationMode,
		false, // cross-origin not allowed
		false, // user verification not required
		s.cfg.RestoreRequireUserPresence,
		stored.Credential.PublicKey,
	)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Restore Key verification failed: "+describe(err))
		return
	}

	// Single use: the key that just signed in is gone. A second device
	// restored from the same backup image cannot reuse it, and the app has to
	// mint a fresh one for the next transfer.
	revoked := stored.ID()
	s.store.RevokeCredential(user, revoked)

	sess := s.store.IssueSession(user, "restore", s.cfg.SessionTTL)
	s.log.Info("zero-tap sign-in", "user", user.Username, "revokedCredential", revoked)
	writeJSON(w, http.StatusOK, authResponse{
		Token:               sess.Token,
		Method:              sess.Method,
		User:                s.viewOf(user),
		RevokedCredentialID: revoked,
	})
}

// ----------------------------------------------------------------- shared

func (s *Server) discoverUser(rawID, userHandle []byte) (webauthn.User, error) {
	if len(userHandle) > 0 {
		if u, err := s.store.UserByID(userHandle); err == nil {
			return u, nil
		}
	}
	u, err := s.store.UserByCredentialID(rawID)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func descriptorsOf(credentials []*StoredCredential) []protocol.CredentialDescriptor {
	out := make([]protocol.CredentialDescriptor, 0, len(credentials))
	for _, c := range credentials {
		out = append(out, c.Credential.Descriptor())
	}
	return out
}

// describe unwraps go-webauthn errors so the app sees why a ceremony failed
// instead of a generic message. Fine for a demo; do not ship this verbatim.
func describe(err error) string {
	var protoErr *protocol.Error
	if errors.As(err, &protoErr) {
		msg := protoErr.Details
		if protoErr.DevInfo != "" {
			msg += " (" + protoErr.DevInfo + ")"
		}
		return msg
	}
	return err.Error()
}
