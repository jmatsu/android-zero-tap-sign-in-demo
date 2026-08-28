package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

// Authenticator flag bits, §6.1 of the WebAuthn spec.
const (
	flagUserPresent         byte = 1 << 0
	flagUserVerified        byte = 1 << 2
	flagBackupEligible      byte = 1 << 3
	flagBackupState         byte = 1 << 4
	flagAttestedCredentials byte = 1 << 6
)

// softAuthenticator is a minimal ES256 authenticator that drives the server the
// way Credential Manager would. It exists because the difference that matters is
// a single flag bit: this can produce an assertion carrying neither User Present
// nor User Verified, which is exactly what a zero-tap sign-in looks like and
// exactly what no real device will hand you in a unit test.
//
// If you are porting the server side, something like this is the cheapest way to
// prove two things at once: that your restore path accepts a flagless assertion,
// and that your passkey path still refuses one.
type softAuthenticator struct {
	t            *testing.T
	key          *ecdsa.PrivateKey
	credentialID []byte
	signCount    uint32
	origin       string
}

func newSoftAuthenticator(t *testing.T, origin string) *softAuthenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		t.Fatalf("credential id: %v", err)
	}
	return &softAuthenticator{t: t, key: key, credentialID: id, origin: origin}
}

func (a *softAuthenticator) clientData(ceremonyType, challenge string) []byte {
	a.t.Helper()
	data, err := json.Marshal(map[string]any{
		"type":      ceremonyType,
		"challenge": challenge,
		"origin":    a.origin,
	})
	if err != nil {
		a.t.Fatalf("client data: %v", err)
	}
	return data
}

func (a *softAuthenticator) coseKey() []byte {
	a.t.Helper()
	// COSE_Key for ES256: kty=EC2(2), alg=-7, crv=P-256(1), x, y.
	key := map[int]any{
		1:  2,
		3:  -7,
		-1: 1,
		-2: a.key.PublicKey.X.FillBytes(make([]byte, 32)),
		-3: a.key.PublicKey.Y.FillBytes(make([]byte, 32)),
	}
	encoded, err := cbor.Marshal(key)
	if err != nil {
		a.t.Fatalf("cose key: %v", err)
	}
	return encoded
}

// authData builds authenticator data; the attested-credential block is
// included exactly when the flags say it is.
func (a *softAuthenticator) authData(rpID string, flags byte) []byte {
	a.t.Helper()

	rpIDHash := sha256.Sum256([]byte(rpID))
	data := append([]byte{}, rpIDHash[:]...)
	data = append(data, flags)

	a.signCount++
	counter := make([]byte, 4)
	binary.BigEndian.PutUint32(counter, a.signCount)
	data = append(data, counter...)

	if flags&flagAttestedCredentials != 0 {
		data = append(data, make([]byte, 16)...) // zero AAGUID
		idLen := make([]byte, 2)
		binary.BigEndian.PutUint16(idLen, uint16(len(a.credentialID)))
		data = append(data, idLen...)
		data = append(data, a.credentialID...)
		data = append(data, a.coseKey()...)
	}
	return data
}

// Register produces a RegistrationResponseJSON with "none" attestation, which is
// what Android returns for both passkeys and Restore Keys. Do not expect
// attestation to distinguish the two for you — your own credential kind is the
// only thing that does.
func (a *softAuthenticator) Register(rpID, challenge string, flags byte) json.RawMessage {
	a.t.Helper()

	clientData := a.clientData("webauthn.create", challenge)
	authData := a.authData(rpID, flags|flagAttestedCredentials)

	attestationObject, err := cbor.Marshal(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": authData,
	})
	if err != nil {
		a.t.Fatalf("attestation object: %v", err)
	}

	return a.marshal(map[string]any{
		"id":                     b64(a.credentialID),
		"rawId":                  b64(a.credentialID),
		"type":                   "public-key",
		"clientExtensionResults": map[string]any{},
		"response": map[string]any{
			"clientDataJSON":    b64(clientData),
			"attestationObject": b64(attestationObject),
			"transports":        []string{"internal", "hybrid"},
		},
	})
}

// Assert produces an AuthenticationResponseJSON signed with the credential.
func (a *softAuthenticator) Assert(rpID, challenge string, userHandle []byte, flags byte) json.RawMessage {
	a.t.Helper()

	clientData := a.clientData("webauthn.get", challenge)
	authData := a.authData(rpID, flags)

	clientDataHash := sha256.Sum256(clientData)
	signed := sha256.Sum256(append(authData, clientDataHash[:]...))
	signature, err := ecdsa.SignASN1(rand.Reader, a.key, signed[:])
	if err != nil {
		a.t.Fatalf("sign: %v", err)
	}

	response := map[string]any{
		"clientDataJSON":    b64(clientData),
		"authenticatorData": b64(authData),
		"signature":         b64(signature),
	}
	if len(userHandle) > 0 {
		response["userHandle"] = b64(userHandle)
	}

	return a.marshal(map[string]any{
		"id":                     b64(a.credentialID),
		"rawId":                  b64(a.credentialID),
		"type":                   "public-key",
		"clientExtensionResults": map[string]any{},
		"response":               response,
	})
}

func (a *softAuthenticator) marshal(v any) json.RawMessage {
	a.t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		a.t.Fatalf("marshal: %v", err)
	}
	return raw
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
