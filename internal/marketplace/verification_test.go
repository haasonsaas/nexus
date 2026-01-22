package marketplace

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"

	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

func TestNewVerifier(t *testing.T) {
	v := NewVerifier()
	if v == nil {
		t.Fatal("expected non-nil verifier")
	}
	if v.HasTrustedKeys() {
		t.Error("expected no trusted keys by default")
	}
}

func TestVerifierWithTrustedKey(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	v := NewVerifier(WithTrustedKey("test", pub))

	if !v.HasTrustedKeys() {
		t.Error("expected verifier to have trusted keys")
	}

	names := v.TrustedKeyNames()
	if len(names) != 1 || names[0] != "test" {
		t.Errorf("expected trusted key name 'test', got %v", names)
	}
}

func TestVerifierWithTrustedKeyBase64(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	pubBase64 := base64.StdEncoding.EncodeToString(pub)

	v := NewVerifier(WithTrustedKeyBase64("test", pubBase64))

	if !v.HasTrustedKeys() {
		t.Error("expected verifier to have trusted keys")
	}
}

func TestVerifierWithInvalidBase64Key(t *testing.T) {
	v := NewVerifier(WithTrustedKeyBase64("invalid", "not-valid-base64!@#$"))

	// Invalid key should be silently ignored
	if v.HasTrustedKeys() {
		t.Error("expected no trusted keys for invalid base64")
	}
}

func TestVerifierWithInvalidSizeKey(t *testing.T) {
	// Create a key that's the wrong size
	shortKey := base64.StdEncoding.EncodeToString([]byte("too-short"))

	v := NewVerifier(WithTrustedKeyBase64("short", shortKey))

	if v.HasTrustedKeys() {
		t.Error("expected no trusted keys for wrong size key")
	}
}

func TestVerifyChecksum(t *testing.T) {
	v := NewVerifier()

	data := []byte("hello world")
	// SHA256 of "hello world"
	expectedChecksum := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"

	result := v.VerifyChecksum(data, expectedChecksum)

	if !result.Valid {
		t.Error("expected valid checksum")
	}
	if !result.ChecksumValid {
		t.Error("expected ChecksumValid to be true")
	}
	if result.ComputedChecksum != expectedChecksum {
		t.Errorf("expected computed checksum %s, got %s", expectedChecksum, result.ComputedChecksum)
	}
	if result.Error != nil {
		t.Errorf("expected no error, got %v", result.Error)
	}
}

func TestVerifyChecksumMismatch(t *testing.T) {
	v := NewVerifier()

	data := []byte("hello world")
	wrongChecksum := "0000000000000000000000000000000000000000000000000000000000000000"

	result := v.VerifyChecksum(data, wrongChecksum)

	if result.Valid {
		t.Error("expected invalid checksum")
	}
	if result.ChecksumValid {
		t.Error("expected ChecksumValid to be false")
	}
	if result.Error == nil {
		t.Error("expected error for checksum mismatch")
	}
}

func TestVerifyChecksumCaseInsensitive(t *testing.T) {
	v := NewVerifier()

	data := []byte("hello world")
	// Uppercase checksum should still match
	upperChecksum := "B94D27B9934D3E08A52E52D7DA7DABFAC484EFE37A5380EE9088F7ACE2EFCDE9"

	result := v.VerifyChecksum(data, upperChecksum)

	if !result.Valid {
		t.Error("expected valid checksum (case insensitive)")
	}
}

func TestVerifySignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	v := NewVerifier(WithTrustedKey("test", pub))

	data := []byte("hello world")
	signature := ed25519.Sign(priv, data)
	sigBase64 := base64.StdEncoding.EncodeToString(signature)

	result := v.VerifySignature(data, sigBase64)

	if !result.Valid {
		t.Error("expected valid signature")
	}
	if !result.SignatureValid {
		t.Error("expected SignatureValid to be true")
	}
	if result.SignedBy != "test" {
		t.Errorf("expected SignedBy 'test', got %s", result.SignedBy)
	}
	if result.Error != nil {
		t.Errorf("expected no error, got %v", result.Error)
	}
}

func TestVerifySignatureNoSignature(t *testing.T) {
	v := NewVerifier()

	result := v.VerifySignature([]byte("data"), "")

	if result.Valid {
		t.Error("expected invalid for empty signature")
	}
	if result.Error == nil {
		t.Error("expected error for empty signature")
	}
}

func TestVerifySignatureInvalidBase64(t *testing.T) {
	v := NewVerifier()

	result := v.VerifySignature([]byte("data"), "not-valid-base64!@#$")

	if result.Valid {
		t.Error("expected invalid for bad base64")
	}
	if result.Error == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestVerifySignatureWrongSize(t *testing.T) {
	v := NewVerifier()

	shortSig := base64.StdEncoding.EncodeToString([]byte("too-short"))
	result := v.VerifySignature([]byte("data"), shortSig)

	if result.Valid {
		t.Error("expected invalid for wrong size signature")
	}
	if result.Error == nil {
		t.Error("expected error for wrong size signature")
	}
}

func TestVerifySignatureNoTrustedKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	v := NewVerifier() // No trusted keys

	data := []byte("hello world")
	signature := ed25519.Sign(priv, data)
	sigBase64 := base64.StdEncoding.EncodeToString(signature)

	result := v.VerifySignature(data, sigBase64)

	if result.Valid {
		t.Error("expected invalid when no trusted keys match")
	}
}

func TestVerifyArtifact(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	v := NewVerifier(WithTrustedKey("test", pub))

	data := []byte("plugin binary data")
	checksum := ComputeChecksum(data)
	signature := SignData(data, priv)

	artifact := &pluginsdk.PluginArtifact{
		Checksum:  checksum,
		Signature: signature,
	}

	result := v.VerifyArtifact(data, artifact)

	if !result.Valid {
		t.Error("expected valid artifact")
	}
	if !result.ChecksumValid {
		t.Error("expected ChecksumValid to be true")
	}
	if !result.SignatureValid {
		t.Error("expected SignatureValid to be true")
	}
}

func TestVerifyArtifactChecksumOnly(t *testing.T) {
	v := NewVerifier() // No trusted keys

	data := []byte("plugin binary data")
	checksum := ComputeChecksum(data)

	artifact := &pluginsdk.PluginArtifact{
		Checksum: checksum,
	}

	result := v.VerifyArtifact(data, artifact)

	if !result.Valid {
		t.Error("expected valid artifact (checksum only)")
	}
	if !result.ChecksumValid {
		t.Error("expected ChecksumValid to be true")
	}
}

func TestVerifyArtifactChecksumMismatch(t *testing.T) {
	v := NewVerifier()

	data := []byte("plugin binary data")

	artifact := &pluginsdk.PluginArtifact{
		Checksum: "0000000000000000000000000000000000000000000000000000000000000000",
	}

	result := v.VerifyArtifact(data, artifact)

	if result.Valid {
		t.Error("expected invalid artifact (checksum mismatch)")
	}
}

func TestAddTrustedKey(t *testing.T) {
	v := NewVerifier()

	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	v.AddTrustedKey("runtime", pub)

	if !v.HasTrustedKeys() {
		t.Error("expected trusted keys after AddTrustedKey")
	}

	names := v.TrustedKeyNames()
	found := false
	for _, name := range names {
		if name == "runtime" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'runtime' in trusted key names")
	}
}

func TestAddTrustedKeyFromBase64(t *testing.T) {
	v := NewVerifier()

	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	pubBase64 := EncodePublicKey(pub)

	err = v.AddTrustedKeyFromBase64("runtime", pubBase64)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !v.HasTrustedKeys() {
		t.Error("expected trusted keys")
	}
}

func TestAddTrustedKeyFromBase64Invalid(t *testing.T) {
	v := NewVerifier()

	err := v.AddTrustedKeyFromBase64("bad", "not-valid-base64")
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestAddTrustedKeyFromBase64WrongSize(t *testing.T) {
	v := NewVerifier()

	shortKey := base64.StdEncoding.EncodeToString([]byte("short"))
	err := v.AddTrustedKeyFromBase64("short", shortKey)
	if err == nil {
		t.Error("expected error for wrong size key")
	}
}

func TestRemoveTrustedKey(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	v := NewVerifier(WithTrustedKey("test", pub))

	if !v.HasTrustedKeys() {
		t.Fatal("expected trusted keys")
	}

	v.RemoveTrustedKey("test")

	if v.HasTrustedKeys() {
		t.Error("expected no trusted keys after removal")
	}
}

func TestComputeChecksum(t *testing.T) {
	data := []byte("hello world")
	expected := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"

	result := ComputeChecksum(data)
	if result != expected {
		t.Errorf("ComputeChecksum() = %s, want %s", result, expected)
	}
}

func TestSignDataAndVerify(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	data := []byte("data to sign")
	signature := SignData(data, priv)

	v := NewVerifier(WithTrustedKey("test", pub))
	result := v.VerifySignature(data, signature)

	if !result.Valid {
		t.Error("expected valid signature from SignData")
	}
}

func TestGenerateKeyPair(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error = %v", err)
	}

	if len(pub) != ed25519.PublicKeySize {
		t.Errorf("expected public key size %d, got %d", ed25519.PublicKeySize, len(pub))
	}
	if len(priv) != ed25519.PrivateKeySize {
		t.Errorf("expected private key size %d, got %d", ed25519.PrivateKeySize, len(priv))
	}

	// Test that keys work together
	data := []byte("test data")
	sig := ed25519.Sign(priv, data)
	if !ed25519.Verify(pub, data, sig) {
		t.Error("generated keys don't work together")
	}
}

func TestEncodeDecodePublicKey(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	encoded := EncodePublicKey(pub)
	decoded, err := DecodePublicKey(encoded)
	if err != nil {
		t.Fatalf("DecodePublicKey() error = %v", err)
	}

	if !pub.Equal(decoded) {
		t.Error("decoded key doesn't match original")
	}
}

func TestDecodePublicKeyInvalid(t *testing.T) {
	_, err := DecodePublicKey("not-valid-base64!@#$")
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestDecodePublicKeyWrongSize(t *testing.T) {
	shortKey := base64.StdEncoding.EncodeToString([]byte("short"))
	_, err := DecodePublicKey(shortKey)
	if err == nil {
		t.Error("expected error for wrong size key")
	}
}

func TestEncodeDecodePrivateKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	encoded := EncodePrivateKey(priv)
	decoded, err := DecodePrivateKey(encoded)
	if err != nil {
		t.Fatalf("DecodePrivateKey() error = %v", err)
	}

	if !priv.Equal(decoded) {
		t.Error("decoded key doesn't match original")
	}
}

func TestDecodePrivateKeyInvalid(t *testing.T) {
	_, err := DecodePrivateKey("not-valid-base64!@#$")
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestDecodePrivateKeyWrongSize(t *testing.T) {
	shortKey := base64.StdEncoding.EncodeToString([]byte("short"))
	_, err := DecodePrivateKey(shortKey)
	if err == nil {
		t.Error("expected error for wrong size key")
	}
}
