package marketplace

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

// Verifier handles signature and checksum verification for plugins.
type Verifier struct {
	trustedKeys map[string]ed25519.PublicKey
	logger      *slog.Logger
}

// VerifierOption configures a Verifier.
type VerifierOption func(*Verifier)

// WithTrustedKey adds a trusted public key.
func WithTrustedKey(name string, publicKey ed25519.PublicKey) VerifierOption {
	return func(v *Verifier) {
		v.trustedKeys[name] = publicKey
	}
}

// WithTrustedKeyBase64 adds a trusted public key from base64.
func WithTrustedKeyBase64(name, base64Key string) VerifierOption {
	return func(v *Verifier) {
		key, err := base64.StdEncoding.DecodeString(base64Key)
		if err != nil {
			v.logger.Warn("invalid base64 key", "name", name, "error", err)
			return
		}
		if len(key) != ed25519.PublicKeySize {
			v.logger.Warn("invalid key size", "name", name, "size", len(key))
			return
		}
		v.trustedKeys[name] = ed25519.PublicKey(key)
	}
}

// WithVerifierLogger sets the logger.
func WithVerifierLogger(logger *slog.Logger) VerifierOption {
	return func(v *Verifier) {
		v.logger = logger
	}
}

// NewVerifier creates a new signature verifier.
func NewVerifier(opts ...VerifierOption) *Verifier {
	v := &Verifier{
		trustedKeys: make(map[string]ed25519.PublicKey),
		logger:      slog.Default().With("component", "marketplace.verifier"),
	}

	for _, opt := range opts {
		opt(v)
	}

	return v
}

// VerificationResult contains the result of a verification.
type VerificationResult struct {
	// Valid indicates whether verification passed.
	Valid bool

	// ChecksumValid indicates checksum verification passed.
	ChecksumValid bool

	// SignatureValid indicates signature verification passed.
	SignatureValid bool

	// SignedBy is the name of the key that signed the artifact.
	SignedBy string

	// ComputedChecksum is the SHA256 hash of the data.
	ComputedChecksum string

	// ExpectedChecksum is the expected checksum from the manifest.
	ExpectedChecksum string

	// Error is the verification error, if any.
	Error error
}

// VerifyChecksum verifies the SHA256 checksum of data.
func (v *Verifier) VerifyChecksum(data []byte, expectedChecksum string) *VerificationResult {
	result := &VerificationResult{
		ExpectedChecksum: expectedChecksum,
	}

	// Compute SHA256
	hash := sha256.Sum256(data)
	result.ComputedChecksum = hex.EncodeToString(hash[:])

	// Compare (case-insensitive)
	if strings.EqualFold(result.ComputedChecksum, expectedChecksum) {
		result.ChecksumValid = true
		result.Valid = true
		v.logger.Debug("checksum verified",
			"expected", expectedChecksum,
			"computed", result.ComputedChecksum)
	} else {
		result.Error = fmt.Errorf("checksum mismatch: expected %s, got %s",
			expectedChecksum, result.ComputedChecksum)
		v.logger.Warn("checksum mismatch",
			"expected", expectedChecksum,
			"computed", result.ComputedChecksum)
	}

	return result
}

// VerifySignature verifies an Ed25519 signature.
func (v *Verifier) VerifySignature(data []byte, signatureBase64 string) *VerificationResult {
	result := &VerificationResult{}

	if signatureBase64 == "" {
		result.Error = fmt.Errorf("no signature provided")
		return result
	}

	signature, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		result.Error = fmt.Errorf("invalid signature encoding: %w", err)
		return result
	}

	if len(signature) != ed25519.SignatureSize {
		result.Error = fmt.Errorf("invalid signature size: %d", len(signature))
		return result
	}

	// Try each trusted key
	for name, publicKey := range v.trustedKeys {
		if ed25519.Verify(publicKey, data, signature) {
			result.SignatureValid = true
			result.Valid = true
			result.SignedBy = name
			v.logger.Debug("signature verified", "signedBy", name)
			return result
		}
	}

	result.Error = fmt.Errorf("signature verification failed: no trusted key matched")
	v.logger.Warn("signature verification failed")
	return result
}

// VerifyArtifact verifies both checksum and signature of an artifact.
func (v *Verifier) VerifyArtifact(data []byte, artifact *pluginsdk.PluginArtifact) *VerificationResult {
	result := &VerificationResult{}

	// Verify checksum
	if artifact.Checksum != "" {
		checksumResult := v.VerifyChecksum(data, artifact.Checksum)
		result.ChecksumValid = checksumResult.ChecksumValid
		result.ComputedChecksum = checksumResult.ComputedChecksum
		result.ExpectedChecksum = checksumResult.ExpectedChecksum
		if !checksumResult.ChecksumValid {
			result.Error = checksumResult.Error
			return result
		}
	}

	// Verify signature if provided and we have trusted keys
	if artifact.Signature != "" && len(v.trustedKeys) > 0 {
		sigResult := v.VerifySignature(data, artifact.Signature)
		result.SignatureValid = sigResult.SignatureValid
		result.SignedBy = sigResult.SignedBy
		if !sigResult.SignatureValid {
			result.Error = sigResult.Error
			return result
		}
	} else if len(v.trustedKeys) > 0 {
		// We have trusted keys but no signature
		v.logger.Debug("no signature provided for artifact")
	}

	result.Valid = result.ChecksumValid
	return result
}

// VerifyManifest verifies the signature of a marketplace manifest.
func (v *Verifier) VerifyManifest(manifest *pluginsdk.MarketplaceManifest) *VerificationResult {
	result := &VerificationResult{}

	if manifest.Signature == "" {
		result.Error = fmt.Errorf("manifest has no signature")
		return result
	}

	// Create a copy without the signature for verification
	manifestCopy := *manifest
	manifestCopy.Signature = ""

	// Serialize to canonical JSON
	data, err := json.Marshal(manifestCopy)
	if err != nil {
		result.Error = fmt.Errorf("serialize manifest: %w", err)
		return result
	}

	return v.VerifySignature(data, manifest.Signature)
}

// AddTrustedKey adds a trusted public key at runtime.
func (v *Verifier) AddTrustedKey(name string, publicKey ed25519.PublicKey) {
	v.trustedKeys[name] = publicKey
}

// AddTrustedKeyFromBase64 adds a trusted public key from base64 at runtime.
func (v *Verifier) AddTrustedKeyFromBase64(name, base64Key string) error {
	key, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return fmt.Errorf("decode base64 key: %w", err)
	}
	if len(key) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid key size: %d", len(key))
	}
	v.trustedKeys[name] = ed25519.PublicKey(key)
	return nil
}

// RemoveTrustedKey removes a trusted public key.
func (v *Verifier) RemoveTrustedKey(name string) {
	delete(v.trustedKeys, name)
}

// TrustedKeyNames returns the names of all trusted keys.
func (v *Verifier) TrustedKeyNames() []string {
	names := make([]string, 0, len(v.trustedKeys))
	for name := range v.trustedKeys {
		names = append(names, name)
	}
	return names
}

// HasTrustedKeys returns whether any trusted keys are configured.
func (v *Verifier) HasTrustedKeys() bool {
	return len(v.trustedKeys) > 0
}

// ComputeChecksum computes the SHA256 checksum of data.
func ComputeChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// SignData signs data with an Ed25519 private key.
func SignData(data []byte, privateKey ed25519.PrivateKey) string {
	signature := ed25519.Sign(privateKey, data)
	return base64.StdEncoding.EncodeToString(signature)
}

// GenerateKeyPair generates a new Ed25519 key pair.
func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key pair: %w", err)
	}
	return publicKey, privateKey, nil
}

// EncodePublicKey encodes a public key to base64.
func EncodePublicKey(key ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(key)
}

// EncodePrivateKey encodes a private key to base64.
func EncodePrivateKey(key ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(key)
}

// DecodePublicKey decodes a public key from base64.
func DecodePublicKey(base64Key string) (ed25519.PublicKey, error) {
	key, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key size: %d", len(key))
	}
	return ed25519.PublicKey(key), nil
}

// DecodePrivateKey decodes a private key from base64.
func DecodePrivateKey(base64Key string) (ed25519.PrivateKey, error) {
	key, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	if len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid private key size: %d", len(key))
	}
	return ed25519.PrivateKey(key), nil
}
