package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

const (
	testRPID        = "zerotap.example.com"
	testFingerprint = debugKeystoreFingerprint

	defaultSeedUsername = "demo"
	defaultSeedPassword = "demo-password"

	// Flag combinations the two credential types produce on Android.
	flagsInteractive = flagUserPresent | flagUserVerified | flagBackupEligible | flagBackupState
	flagsZeroTap     = flagBackupEligible | flagBackupState
)

type testServer struct {
	t      *testing.T
	http   *httptest.Server
	origin string
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	return newTestServerWith(t, nil)
}

func newTestServerWith(t *testing.T, tweak func(*Config)) *testServer {
	t.Helper()

	origin, err := apkKeyHashOrigin(testFingerprint)
	if err != nil {
		t.Fatalf("apk key hash: %v", err)
	}

	cfg := &Config{
		Addr:                    ":0",
		DBPath:                  ":memory:",
		RPID:                    testRPID,
		RPName:                  "Zero-Tap Sign-In Demo",
		RPOrigins:               []string{"https://" + testRPID, origin},
		SessionTTL:              time.Hour,
		CeremonyTTL:             5 * time.Minute,
		AndroidPackageName:      "com.github.jmatsu.zerotap",
		AndroidCertFingerprints: []string{testFingerprint},
		// Zero-tap sign-in carries no user gesture, matching the server default.
		RestoreRequireUserPresence: false,
		SeedDemoUsername:           defaultSeedUsername,
		SeedDemoPassword:           defaultSeedPassword,
	}

	if tweak != nil {
		tweak(cfg)
	}

	srv, err := NewServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err = srv.SeedDemoUser(); err != nil {
		t.Fatalf("seed demo user: %v", err)
	}

	ts := &testServer{t: t, http: httptest.NewServer(srv.Routes()), origin: origin}
	t.Cleanup(ts.http.Close)
	t.Cleanup(func() { _ = srv.Close() })
	return ts
}

func (ts *testServer) do(method, path, token string, body any) (int, map[string]any) {
	ts.t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			ts.t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, ts.http.URL+path, reader)
	if err != nil {
		ts.t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := ts.http.Client().Do(req)
	if err != nil {
		ts.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil && err != io.EOF {
		ts.t.Fatalf("decode %s %s: %v", method, path, err)
	}
	return resp.StatusCode, decoded
}

func (ts *testServer) mustDo(method, path, token string, body any) map[string]any {
	ts.t.Helper()
	status, decoded := ts.do(method, path, token, body)
	if status != http.StatusOK {
		ts.t.Fatalf("%s %s: status %d: %v", method, path, status, decoded)
	}
	return decoded
}

// signUp creates an account through the password flow and returns its token.
func (ts *testServer) signUp(username string) string {
	ts.t.Helper()
	signup := ts.mustDo("POST", "/api/signup", "", map[string]string{"username": username, "password": "correct-horse"})
	token, _ := signup["token"].(string)
	if token == "" {
		ts.t.Fatal("signup returned no session token")
	}
	return token
}

// register drives a full create ceremony and returns the user handle the
// server put in the options, which is what a discoverable credential later
// echoes back as userHandle.
func (ts *testServer) register(path, token string, auth *softAuthenticator, flags byte) []byte {
	ts.t.Helper()

	begin := ts.mustDo("POST", path+"/begin", token, nil)
	options := optionsOf(ts.t, begin)
	response := auth.Register(testRPID, stringField(ts.t, options, "challenge"), flags)

	finish := ts.mustDo("POST", path+"/finish", token, finishRequest{
		CeremonyID: stringField(ts.t, begin, "ceremonyId"),
		Credential: response,
	})

	// The app renders the account straight from this response instead of
	// re-fetching it, so the credential it just registered has to be in there.
	registered, _ := finish["user"].(map[string]any)
	if id := stringField(ts.t, finish, "credentialId"); !hasCredential(registered, id) {
		ts.t.Fatalf("register response does not list credential %s: %v", id, finish)
	}

	user, ok := options["user"].(map[string]any)
	if !ok {
		ts.t.Fatalf("creation options carry no user entity: %v", options)
	}
	handle, err := base64.RawURLEncoding.DecodeString(stringField(ts.t, user, "id"))
	if err != nil {
		ts.t.Fatalf("decode user handle: %v", err)
	}
	return handle
}

// signIn drives a full get ceremony and returns the raw HTTP status plus body.
func (ts *testServer) signIn(path string, auth *softAuthenticator, userHandle []byte, flags byte) (int, map[string]any) {
	ts.t.Helper()

	begin := ts.mustDo("POST", path+"/begin", "", nil)
	options := optionsOf(ts.t, begin)
	response := auth.Assert(testRPID, stringField(ts.t, options, "challenge"), userHandle, flags)

	return ts.do("POST", path+"/finish", "", finishRequest{
		CeremonyID: stringField(ts.t, begin, "ceremonyId"),
		Credential: response,
	})
}

func optionsOf(t *testing.T, begin map[string]any) map[string]any {
	t.Helper()
	options, ok := begin["requestJson"].(map[string]any)
	if !ok {
		t.Fatalf("begin response has no requestJson object: %v", begin)
	}
	return options
}

func stringField(t *testing.T, m map[string]any, key string) string {
	t.Helper()
	v, ok := m[key].(string)
	if !ok {
		t.Fatalf("expected string field %q in %v", key, m)
	}
	return v
}

func hasCredential(view map[string]any, id string) bool {
	credentials, _ := view["credentials"].([]any)
	for _, raw := range credentials {
		if c, _ := raw.(map[string]any); c["id"] == id {
			return true
		}
	}
	return false
}

func countCredentials(t *testing.T, me map[string]any, kind string) int {
	t.Helper()
	credentials, _ := me["credentials"].([]any)
	count := 0
	for _, raw := range credentials {
		c, _ := raw.(map[string]any)
		if c["kind"] == kind {
			count++
		}
	}
	return count
}

// TestZeroTapSignInAfterDeviceTransfer walks the whole scenario: password
// sign-in on device A, Restore Key creation, then a sign-in on device B that
// needs no user interaction, after which the Restore Key is gone.
func TestZeroTapSignInAfterDeviceTransfer(t *testing.T) {
	ts := newTestServer(t)

	// Device A: password sign-in, then the app silently registers a Restore Key.
	tokenA := ts.signUp("jmatsu")
	restoreKey := newSoftAuthenticator(t, ts.origin)
	userHandle := ts.register("/api/restore/register", tokenA, restoreKey, flagsZeroTap)

	if got := countCredentials(t, ts.mustDo("GET", "/api/me", tokenA, nil), "restore"); got != 1 {
		t.Fatalf("expected 1 Restore Key after registration, got %d", got)
	}

	// Device B: the Restore Key came across with the backup. No prompt, so the
	// assertion carries neither User Present nor User Verified.
	status, body := ts.signIn("/api/restore/login", restoreKey, userHandle, flagsZeroTap)
	if status != http.StatusOK {
		t.Fatalf("zero-tap sign-in failed: status %d: %v", status, body)
	}
	if body["method"] != "restore" {
		t.Errorf("expected sign-in method %q, got %q", "restore", body["method"])
	}
	if body["revokedCredentialId"] == "" || body["revokedCredentialId"] == nil {
		t.Error("server did not report the revoked Restore Key")
	}

	tokenB, _ := body["token"].(string)
	if tokenB == "" {
		t.Fatal("zero-tap sign-in returned no session token")
	}

	// The Restore Key is single use: it is gone from the account...
	if got := countCredentials(t, ts.mustDo("GET", "/api/me", tokenB, nil), "restore"); got != 0 {
		t.Errorf("expected the Restore Key to be revoked, still have %d", got)
	}

	// ...and replaying the same key (say, from a second device restored from
	// the same backup image) is rejected.
	if status, body := ts.signIn("/api/restore/login", restoreKey, userHandle, flagsZeroTap); status != http.StatusUnauthorized {
		t.Errorf("replayed Restore Key: expected 401, got %d: %v", status, body)
	}

	// Device B re-arms itself for the next transfer.
	ts.register("/api/restore/register", tokenB, newSoftAuthenticator(t, ts.origin), flagsZeroTap)
	if got := countCredentials(t, ts.mustDo("GET", "/api/me", tokenB, nil), "restore"); got != 1 {
		t.Errorf("expected a fresh Restore Key on device B, got %d", got)
	}
}

func TestPasskeyRegistrationAndSignIn(t *testing.T) {
	ts := newTestServer(t)

	token := ts.signUp("passkey-user")
	passkey := newSoftAuthenticator(t, ts.origin)
	userHandle := ts.register("/api/passkey/register", token, passkey, flagsInteractive)

	status, body := ts.signIn("/api/passkey/login", passkey, userHandle, flagsInteractive)
	if status != http.StatusOK {
		t.Fatalf("passkey sign-in failed: status %d: %v", status, body)
	}
	if body["method"] != "passkey" {
		t.Errorf("expected sign-in method %q, got %q", "passkey", body["method"])
	}

	// Unlike a Restore Key, a passkey survives being used.
	newToken, _ := body["token"].(string)
	if got := countCredentials(t, ts.mustDo("GET", "/api/me", newToken, nil), "passkey"); got != 1 {
		t.Errorf("expected the passkey to survive sign-in, got %d", got)
	}
}

// TestPasskeySignInRequiresUserVerification guards the asymmetry that makes
// this demo meaningful: only the restore flow is allowed to skip the prompt.
func TestPasskeySignInRequiresUserVerification(t *testing.T) {
	ts := newTestServer(t)

	token := ts.signUp("strict-user")
	passkey := newSoftAuthenticator(t, ts.origin)
	userHandle := ts.register("/api/passkey/register", token, passkey, flagsInteractive)

	status, body := ts.signIn("/api/passkey/login", passkey, userHandle, flagsZeroTap)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected an unverified passkey assertion to be rejected, got %d: %v", status, body)
	}
}

// TestRestoreKeyIsNotAPasskey makes sure a Restore Key cannot be laundered
// through the interactive passkey endpoint to dodge single-use revocation.
func TestRestoreKeyIsNotAPasskey(t *testing.T) {
	ts := newTestServer(t)

	token := ts.signUp("mixed-user")
	restoreKey := newSoftAuthenticator(t, ts.origin)
	userHandle := ts.register("/api/restore/register", token, restoreKey, flagsZeroTap)

	status, body := ts.signIn("/api/passkey/login", restoreKey, userHandle, flagsInteractive)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected the Restore Key to be rejected by the passkey endpoint, got %d: %v", status, body)
	}
	if got := countCredentials(t, ts.mustDo("GET", "/api/me", token, nil), "restore"); got != 1 {
		t.Errorf("the rejected attempt should not have consumed the Restore Key, have %d", got)
	}
}

// TestRestoreKeyReplacesPrevious mirrors Android, which keeps exactly one
// Restore Key per package name.
func TestRestoreKeyReplacesPrevious(t *testing.T) {
	ts := newTestServer(t)

	token := ts.signUp("rotating-user")
	first := newSoftAuthenticator(t, ts.origin)
	userHandle := ts.register("/api/restore/register", token, first, flagsZeroTap)
	ts.register("/api/restore/register", token, newSoftAuthenticator(t, ts.origin), flagsZeroTap)

	if got := countCredentials(t, ts.mustDo("GET", "/api/me", token, nil), "restore"); got != 1 {
		t.Fatalf("expected exactly 1 Restore Key after re-registration, got %d", got)
	}
	if status, _ := ts.signIn("/api/restore/login", first, userHandle, flagsZeroTap); status != http.StatusUnauthorized {
		t.Errorf("expected the superseded Restore Key to be rejected, got %d", status)
	}
}

func TestPasswordLoginRejectsWrongPassword(t *testing.T) {
	ts := newTestServer(t)
	ts.signUp("password-user")

	if status, _ := ts.do("POST", "/api/login/password", "", map[string]string{"username": "password-user", "password": "wrong"}); status != http.StatusUnauthorized {
		t.Errorf("expected 401 for a wrong password, got %d", status)
	}
	if status, _ := ts.do("POST", "/api/login/password", "", map[string]string{"username": "password-user", "password": "correct-horse"}); status != http.StatusOK {
		t.Errorf("expected 200 for the right password, got %d", status)
	}
}

func TestAssetLinksDescribesTheApp(t *testing.T) {
	ts := newTestServer(t)

	resp, err := ts.http.Client().Get(ts.http.URL + "/.well-known/assetlinks.json")
	if err != nil {
		t.Fatalf("fetch asset links: %v", err)
	}
	defer resp.Body.Close()

	var statements []struct {
		Relation []string `json:"relation"`
		Target   struct {
			Namespace    string   `json:"namespace"`
			PackageName  string   `json:"package_name"`
			Fingerprints []string `json:"sha256_cert_fingerprints"`
		} `json:"target"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&statements); err != nil {
		t.Fatalf("decode asset links: %v", err)
	}

	if len(statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(statements))
	}
	target := statements[0].Target
	if target.PackageName != "com.github.jmatsu.zerotap" || len(target.Fingerprints) != 1 {
		t.Errorf("unexpected asset link target: %+v", target)
	}
	if !slices.Contains(statements[0].Relation, "delegate_permission/common.get_login_creds") {
		t.Error("asset links must delegate get_login_creds or Credential Manager refuses the app")
	}
}

func TestSeededDemoUserCanSignIn(t *testing.T) {
	ts := newTestServer(t)

	if status, body := ts.do("POST", "/api/login/password", "", map[string]string{
		"username": defaultSeedUsername,
		"password": defaultSeedPassword,
	}); status != http.StatusOK {
		t.Fatalf("expected the seeded demo account to sign in, got %d: %v", status, body)
	}
}

// TestSeedDemoUserParsing covers the env-var surface, including the values that
// turn seeding off and the ones the account rules must reject.
func TestSeedDemoUserParsing(t *testing.T) {
	for _, tc := range []struct {
		raw      string
		username string
		password string
		wantErr  bool
	}{
		{raw: defaultDemoUser, username: "demo", password: "demo-password"},
		{raw: "  spaced : padded-password ", username: "spaced", password: "padded-password"},
		{raw: "off"},
		{raw: "NONE"},
		{raw: "-"},
		{raw: ""},
		{raw: "nocolon", wantErr: true},
		{raw: "ab:long-enough-password", wantErr: true}, // username too short
		{raw: "someone:short", wantErr: true},           // password too short
	} {
		t.Run(tc.raw, func(t *testing.T) {
			var cfg Config
			err := cfg.parseDemoUser(tc.raw)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.raw, err)
			}
			if cfg.SeedDemoUsername != tc.username || cfg.SeedDemoPassword != tc.password {
				t.Errorf("got %q/%q, want %q/%q", cfg.SeedDemoUsername, cfg.SeedDemoPassword, tc.username, tc.password)
			}
		})
	}
}

// TestSeedingDisabledLeavesNoAccount guards the opt-out.
func TestSeedingDisabledLeavesNoAccount(t *testing.T) {
	ts := newTestServerWith(t, func(cfg *Config) {
		cfg.SeedDemoUsername, cfg.SeedDemoPassword = "", ""
	})

	if status, _ := ts.do("POST", "/api/login/password", "", map[string]string{
		"username": defaultSeedUsername,
		"password": defaultSeedPassword,
	}); status != http.StatusUnauthorized {
		t.Errorf("expected no demo account when seeding is off, got %d", status)
	}
}
