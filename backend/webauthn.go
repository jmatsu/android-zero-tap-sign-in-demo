package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// Everything a Relying Party has to do differently for Restore Keys lives in
// this file.
//
// A Restore Key is an ordinary WebAuthn credential. If you already support
// passkeys you do not need a second FIDO library, a second credential table or
// a second notion of what a credential is. You need four deliberate
// differences, and they are all below:
//
//  1. Register it without demanding user verification, because the system mints
//     it with no prompt (beginRegistration).
//  2. File it under its own kind, so it can never be redeemed through your
//     interactive passkey endpoint (finishRegistration, and the kind check in
//     handlePasskeyLoginFinish).
//  3. Verify the assertion without requiring the User Present or User Verified
//     flags, because nobody was there to press anything
//     (handleRestoreLoginFinish).
//  4. Delete it the instant it verifies, so one backup image cannot sign in
//     twice (handleRestoreLoginFinish).
//
// Challenges, origins, signature checking and sign counters are unchanged: that
// is your existing passkey code, and it should stay that way.

// beginResponse is the "begin" half of both ceremonies. RequestJSON is a raw
// PublicKeyCredentialCreationOptionsJSON / PublicKeyCredentialRequestOptionsJSON
// that the app hands to Credential Manager unchanged, so keep it a verbatim JSON
// object rather than a string or a struct you re-model.
//
// CeremonyID is how this demo pins the challenge server side without a cookie.
// If your service already has a cache or table for pending ceremonies, use it;
// the restore login in particular begins on a device that has no session and no
// cookie jar worth the name.
type beginResponse struct {
	CeremonyID  string          `json:"ceremonyId"`
	RequestJSON json.RawMessage `json:"requestJson"`
}

// finishRequest is the "finish" half: the RegistrationResponseJSON /
// AuthenticationResponseJSON that Credential Manager produced, forwarded byte
// for byte. The app parses none of it, and neither should yours.
type finishRequest struct {
	CeremonyID string          `json:"ceremonyId"`
	Credential json.RawMessage `json:"credential"`
}

// beginRegistration issues creation options for either kind of credential.
// Both must be discoverable (resident): a restored device knows nothing about
// whose account it inherited, so it can never send an allowCredentials list.
//
// Difference 1 lives here. Passkey registration requires user verification;
// Restore Key registration must not, or Credential Manager's silent create
// produces a response your own server then rejects.
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
		// The system mints a Restore Key on the user's behalf with no prompt,
		// so the response carries neither the User Verified nor the User
		// Present flag. Asking for either one guarantees a failure at finish
		// time, and the error will not obviously say so.
		//
		// The go-webauthn-specific half: recording the ceremony as
		// "conditional" mediation is what makes the library skip its own User
		// Present check on the way back in. The mediation value never reaches
		// the client. Porting this means finding the equivalent knob on your
		// library's registration verifier — and if it does not expose one, you
		// are parsing the attestation object yourself.
		selection.UserVerification = protocol.VerificationDiscouraged
		mediation = protocol.MediationConditional
	} else {
		// excludeCredentials lists passkeys only. A new Restore Key is meant to
		// supersede the old one, so naming the old one here would have the
		// authenticator refuse the replacement as a duplicate.
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

// finishRegistration verifies a create response and files the credential under
// its kind. That kind is difference 2, and it is the only thing standing between
// a Restore Key and your interactive passkey endpoint: store it, and check it on
// every path that spends a credential.
//
// Returns nil after writing the error response.
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

// beginLogin starts a discoverable ceremony: no allowCredentials, no username.
// Both kinds need it, for different reasons — a passkey sign-in has no username
// typed yet, and a restored device does not yet know whose account it inherited.
//
// The uv argument is the whole story. Passkeys pass "required"; Restore Keys
// pass "discouraged", because asking a device with nobody in front of it to
// verify a user can only end one way.
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

// takeAssertion consumes the pending ceremony and parses the response without
// verifying anything. The two login paths verify very differently, and keeping
// that difference visible in the handlers is the point. Returns nil after
// writing the error response.
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
		// Do not drop this check when you port the flow. Without it a Restore
		// Key can be spent through the passkey endpoint, which revokes nothing
		// — and the single-use guarantee is gone, silently.
		writeError(w, http.StatusUnauthorized, "credential is not a passkey")
		return
	}

	s.store.TouchCredential(stored, credential.Authenticator.SignCount)

	sess := s.store.IssueSession(user, "passkey", s.cfg.SessionTTL)
	s.log.Info("passkey sign-in", "user", user.Username, "credential", stored.ID())
	writeJSON(w, http.StatusOK, authResponse{Token: sess.Token, Method: sess.Method, User: s.viewOf(user)})
}

// ---------------------------------------------------------- Restore Keys

// handleRestoreRegisterBegin runs on a session the app already holds, right
// after every successful sign-in by any method. There is no user-facing trigger
// for it, and there should not be one: nobody is meant to be asked anything.
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

// handleRestoreRevoke drops the account's Restore Key. Give yourself this
// endpoint: the app calls it on sign-out, in the same breath as it clears the
// device's copy, and without it a signed-out phone can still hand the account to
// whatever device inherits its backup.
func (s *Server) handleRestoreRevoke(w http.ResponseWriter, _ *http.Request, u *User, _ *AuthSession) {
	revoked := s.store.RevokeCredentialsOfKind(u, KindRestore)
	s.log.Info("Restore Keys revoked", "user", u.Username, "credentials", revoked)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRestoreLoginBegin(w http.ResponseWriter, _ *http.Request) {
	s.beginLogin(w, KindRestore, protocol.VerificationDiscouraged)
}

// handleRestoreLoginFinish is the endpoint that makes the sign-in zero-tap, and
// it carries differences 3 and 4.
//
// It deliberately does not call webauthn.ValidatePasskeyLogin. That helper
// hard-codes a User Present check, and a zero-tap assertion has no gesture
// behind it, so this drops to parsed.Verify and spells the requirements out.
// Expect the same in whatever library you use: the convenient wrapper is nearly
// always the one that assumes a human.
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

	// Difference 3. Challenge, RP ID, origin and signature are checked exactly
	// as they are for a passkey; the two relaxations are the last flags — user
	// verification is not required, and user presence only if you deliberately
	// set RESTORE_REQUIRE_USER_PRESENCE (which breaks real transfers; that is
	// what it is for).
	//
	// s.cfg.RPOrigins is what makes any of this work on Android: the origin in
	// clientDataJSON is android:apk-key-hash:<...>, not https://<rpId>. See
	// apkKeyHashOrigin in config.go, which is the usual culprit when a FIDO
	// library rejects an Android assertion for an origin mismatch.
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

	// Difference 4, and the property that pays for the missing prompt: a backup
	// image is a file, and files get copied. Deleting the credential here means
	// the second device to present it gets a 401. Do it before you issue the
	// session, and do it even if what follows fails — a Restore Key that
	// survives its own redemption is a reusable one.
	//
	// The response names the key that died so the app can drop its local copy
	// and register a replacement for the next transfer.
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

// discoverUser resolves the account behind a discoverable assertion, preferring
// the user handle and falling back to the credential id. Both login flows share
// it: a Restore Key needs no lookup path your passkeys do not already need.
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

// describe unwraps go-webauthn errors so the app's on-screen log can say why a
// ceremony failed. Invaluable while you are bringing an integration up, and the
// first thing to delete before you ship: it hands an unauthenticated caller a
// running commentary on your verification.
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
