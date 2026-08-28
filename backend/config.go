package main

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// defaultDemoUser is seeded at startup so there is always something to sign in
// as, even on a fresh database. Demo scaffolding; the first thing to delete when
// you adapt this to a real service.
const defaultDemoUser = "demo:demo-password"

// debugKeystoreFingerprint is the SHA-256 of the certificate in
// android/debug.keystore, checked into this repository so the demo runs with no
// per-machine setup. Set ANDROID_CERT_FINGERPRINTS to your own certificate as
// soon as you stop signing with the debug key — and if you ship through Play
// App Signing, the fingerprint that matters is the one Play shows you, not your
// upload key.
const debugKeystoreFingerprint = "F9:C6:63:E4:37:EC:EE:B3:A1:F5:D9:38:70:21:7F:6B:42:E4:73:C5:2E:D9:09:34:CA:1A:52:3B:AD:33:52:14"

// Config holds everything the demo server needs, all overridable through the
// environment so the same binary runs behind a tunnel, on a LAN or in a
// container.
//
// Three fields decide whether Restore Keys work for you at all: RPID,
// AndroidPackageName and AndroidCertFingerprints. Get any of them wrong and
// Credential Manager refuses to create anything, with an error that will not
// tell you which one.
type Config struct {
	Addr string

	// DBPath is the SQLite database file, or ":memory:" for a throwaway one.
	DBPath string

	// RPID is the WebAuthn Relying Party ID: the hostname the app talks to, no
	// scheme and no port. It must serve /.well-known/assetlinks.json over real
	// HTTPS, because Credential Manager fetches that document before it will
	// create or use any credential, and a self-signed certificate will not do.
	// In development that is what the tunnel in the README buys you; in
	// production it is simply your API hostname.
	//
	// It must also match zerotap.rpId on the app side. A mismatch surfaces as a
	// create failure with nothing useful in the message.
	RPID   string
	RPName string

	// RPOrigins are the accepted clientDataJSON origins, and on Android that is
	// not https://<rpId>: Credential Manager writes
	// android:apk-key-hash:<base64url of the signing certificate's SHA-256>.
	// LoadConfig derives those below. A web front end would add its https://
	// origin alongside them.
	RPOrigins []string

	SessionTTL  time.Duration
	CeremonyTTL time.Duration

	// AndroidPackageName and AndroidCertFingerprints identify the one app
	// allowed to hold credentials for this RP ID. They do double duty: they are
	// published in the Digital Asset Links statement Credential Manager fetches
	// (see handleAssetLinks), and they derive the apk-key-hash origins above, so
	// a stale fingerprint breaks verification and asset links at once.
	//
	// List every certificate that may sign a build you care about. They default
	// to the demo app signed with android/debug.keystore.
	AndroidPackageName      string
	AndroidCertFingerprints []string

	// RestoreRequireUserPresence demands the User Present flag on restore
	// assertions. It exists so you can watch a real transfer fail: a zero-tap
	// sign-in has no gesture behind it, so turning this on rejects precisely the
	// flow this demo is about. Leave it false. Passkey sign-in requires the flag
	// unconditionally and is unaffected either way.
	RestoreRequireUserPresence bool

	// SeedDemoUsername and SeedDemoPassword create an account at startup so
	// there is something to sign in as without typing. Demo scaffolding: empty
	// when seeding is disabled, a no-op once the account exists, and not
	// something to carry into a real service.
	SeedDemoUsername string
	SeedDemoPassword string
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		Addr:                       env("ADDR", ":8080"),
		DBPath:                     env("DB_PATH", "zerotap.db"),
		RPID:                       env("RP_ID", "localhost"),
		RPName:                     env("RP_NAME", "Zero-Tap Sign-In Demo"),
		SessionTTL:                 24 * time.Hour,
		CeremonyTTL:                5 * time.Minute,
		AndroidPackageName:         env("ANDROID_PACKAGE_NAME", "com.github.jmatsu.zerotap"),
		AndroidCertFingerprints:    splitAndTrim(env("ANDROID_CERT_FINGERPRINTS", debugKeystoreFingerprint)),
		RestoreRequireUserPresence: envBool("RESTORE_REQUIRE_USER_PRESENCE", false),
	}

	if len(cfg.AndroidCertFingerprints) == 0 {
		return nil, fmt.Errorf("ANDROID_CERT_FINGERPRINTS must not be empty (colon separated SHA-256 of the APK signing certificate)")
	}

	if err := cfg.parseDemoUser(env("SEED_DEMO_USER", defaultDemoUser)); err != nil {
		return nil, err
	}

	origins := []string{"https://" + cfg.RPID}
	if cfg.RPID == "localhost" {
		origins = append(origins, "http://localhost", "http://localhost:8080")
	}
	for _, fp := range cfg.AndroidCertFingerprints {
		origin, err := apkKeyHashOrigin(fp)
		if err != nil {
			return nil, err
		}
		// Play services has emitted both spellings over the years, and which
		// one you get depends on the device. Accept both: a single-spelling
		// allowlist works until it reaches somebody's phone.
		origins = append(origins, origin, strings.Replace(origin, "apk-key-hash:", "apk-key-hash-sha256:", 1))
	}
	origins = append(origins, splitAndTrim(env("EXTRA_ORIGINS", ""))...)
	cfg.RPOrigins = origins

	return cfg, nil
}

// parseDemoUser reads the "username:password" form of SEED_DEMO_USER. The
// values "off", "none" and "-" turn seeding off. Demo scaffolding.
func (cfg *Config) parseDemoUser(raw string) error {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "off", "none", "-":
		return nil
	}

	username, password, ok := strings.Cut(raw, ":")
	if !ok {
		return fmt.Errorf("SEED_DEMO_USER must be in username:password form, got %q", raw)
	}

	username, password = strings.TrimSpace(username), strings.TrimSpace(password)
	if len(username) < minUsernameLength || len(password) < minPasswordLength {
		return fmt.Errorf(
			"SEED_DEMO_USER must have a username of at least %d characters and a password of at least %d",
			minUsernameLength, minPasswordLength,
		)
	}

	cfg.SeedDemoUsername, cfg.SeedDemoPassword = username, password
	return nil
}

// apkKeyHashOrigin converts an "AA:BB:.." certificate fingerprint into the
// origin Credential Manager actually writes into clientDataJSON:
// android:apk-key-hash:<base64url, unpadded>. When a FIDO library rejects an
// Android assertion for an origin mismatch, this conversion — or a stale
// fingerprint feeding it — is nearly always the reason.
func apkKeyHashOrigin(fingerprint string) (string, error) {
	raw, err := hex.DecodeString(strings.ReplaceAll(strings.TrimSpace(fingerprint), ":", ""))
	if err != nil {
		return "", fmt.Errorf("invalid certificate fingerprint %q: %w", fingerprint, err)
	}
	if len(raw) != 32 {
		return "", fmt.Errorf("certificate fingerprint %q is %d bytes, want a 32 byte SHA-256", fingerprint, len(raw))
	}
	return "android:apk-key-hash:" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v, err := strconv.ParseBool(env(key, ""))
	if err != nil {
		return fallback
	}
	return v
}

func splitAndTrim(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
