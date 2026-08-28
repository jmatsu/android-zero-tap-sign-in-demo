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

// debugKeystoreFingerprint is the SHA-256 of the certificate in
// android/debug.keystore, which is checked into this repository so the demo
// runs without any per-machine setup. Override ANDROID_CERT_FINGERPRINTS when
// you sign the app with your own key.
// The demo account is seeded by default so there is always something to sign
// in as, even on a fresh database.
const defaultDemoUser = "demo:demo-password"

const debugKeystoreFingerprint = "F9:C6:63:E4:37:EC:EE:B3:A1:F5:D9:38:70:21:7F:6B:42:E4:73:C5:2E:D9:09:34:CA:1A:52:3B:AD:33:52:14"

// Config holds everything the demo server needs. Every value can be overridden
// through the environment so the same binary works behind a tunnel, on a LAN or
// in a container.
type Config struct {
	Addr string

	// DBPath is the SQLite database file, or ":memory:" for a throwaway one.
	DBPath string

	// RPID is the WebAuthn Relying Party ID. It must be the hostname the app
	// talks to (no scheme, no port) and it must serve
	// /.well-known/assetlinks.json over HTTPS, otherwise Credential Manager
	// refuses to create passkeys or Restore Keys.
	RPID        string
	RPName      string
	RPOrigins   []string
	SessionTTL  time.Duration
	CeremonyTTL time.Duration

	// AndroidPackageName and AndroidCertFingerprints are published through
	// Digital Asset Links and are also used to derive the
	// android:apk-key-hash:<base64url> origins that Credential Manager puts in
	// clientDataJSON. They default to the demo app signed with the debug
	// keystore checked into android/debug.keystore.
	AndroidPackageName      string
	AndroidCertFingerprints []string

	// RestoreRequireUserPresence controls whether a restore-key assertion must
	// carry the User Present flag. Zero-tap sign-in happens without any user
	// gesture, so this defaults to false. Passkey sign-in always requires it.
	RestoreRequireUserPresence bool

	// SeedDemoUsername and SeedDemoPassword are created at startup so there is
	// something to sign in as without typing. Empty when seeding is disabled,
	// and a no-op once the account is in the database.
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
		// Play services has used both spellings over time; accept either.
		origins = append(origins, origin, strings.Replace(origin, "apk-key-hash:", "apk-key-hash-sha256:", 1))
	}
	origins = append(origins, splitAndTrim(env("EXTRA_ORIGINS", ""))...)
	cfg.RPOrigins = origins

	return cfg, nil
}

// parseDemoUser reads the "username:password" form of SEED_DEMO_USER. The
// values "off", "none" and "-" turn seeding off.
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

// apkKeyHashOrigin converts "AA:BB:.." into the origin Credential Manager
// writes into clientDataJSON: android:apk-key-hash:<base64url-no-padding>.
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
