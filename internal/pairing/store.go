package pairing

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/security"
)

const (
	CodeLength = 8
	CodeTTL    = time.Hour
)

var (
	ErrCodeNotFound = errors.New("pairing code not found")
)

type Request struct {
	Code        string    `json:"code"`
	SenderID    string    `json:"sender_id"`
	SenderName  string    `json:"sender_name,omitempty"`
	RequestedAt time.Time `json:"requested_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type Store struct {
	provider string
	stateDir string
	now      func() time.Time
	rand     io.Reader
	mu       sync.Mutex
}

func NewStore(provider string) *Store {
	return NewStoreWithDir(provider, security.DefaultStateDir())
}

func NewStoreWithDir(provider string, stateDir string) *Store {
	if strings.TrimSpace(stateDir) == "" {
		stateDir = security.DefaultStateDir()
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = "default"
	}
	return &Store{
		provider: provider,
		stateDir: stateDir,
		now:      time.Now,
		rand:     rand.Reader,
	}
}

func (s *Store) LoadAllowlist() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadAllowlistLocked()
}

func (s *Store) SaveAllowlist(allowlist []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	allowlist = sanitizeAllowlist(allowlist)
	return s.writeJSONLocked(s.allowlistPath(), allowlist)
}

func (s *Store) Pending() ([]Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadPendingLocked()
}

func (s *Store) GetOrCreateRequest(senderID string, senderName string) (Request, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	senderID = strings.TrimSpace(senderID)
	if senderID == "" {
		return Request{}, false, errors.New("sender id is required")
	}

	pending, err := s.loadPendingLocked()
	if err != nil {
		return Request{}, false, err
	}

	now := s.now()
	for _, req := range pending {
		if req.SenderID == senderID && req.ExpiresAt.After(now) {
			return req, false, nil
		}
	}

	existingCodes := map[string]struct{}{}
	for _, req := range pending {
		if req.Code != "" {
			existingCodes[req.Code] = struct{}{}
		}
	}

	code, err := s.generateCode(existingCodes)
	if err != nil {
		return Request{}, false, err
	}

	req := Request{
		Code:        code,
		SenderID:    senderID,
		SenderName:  strings.TrimSpace(senderName),
		RequestedAt: now,
		ExpiresAt:   now.Add(CodeTTL),
	}
	pending = append(pending, req)
	if err := s.writeJSONLocked(s.pendingPath(), pending); err != nil {
		return Request{}, false, err
	}
	return req, true, nil
}

func (s *Store) Approve(code string) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	code = normalizeCode(code)
	if code == "" {
		return Request{}, ErrCodeNotFound
	}

	pending, err := s.loadPendingLocked()
	if err != nil {
		return Request{}, err
	}

	index := -1
	var req Request
	for i, candidate := range pending {
		if normalizeCode(candidate.Code) == code {
			index = i
			req = candidate
			break
		}
	}
	if index == -1 {
		return Request{}, ErrCodeNotFound
	}

	allowlist, err := s.loadAllowlistLocked()
	if err != nil {
		return Request{}, err
	}
	allowlist = append(allowlist, req.SenderID)
	allowlist = sanitizeAllowlist(allowlist)
	if err := s.writeJSONLocked(s.allowlistPath(), allowlist); err != nil {
		return Request{}, err
	}

	pending = append(pending[:index], pending[index+1:]...)
	if err := s.writeJSONLocked(s.pendingPath(), pending); err != nil {
		return Request{}, err
	}

	return req, nil
}

func (s *Store) Deny(code string) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	code = normalizeCode(code)
	if code == "" {
		return Request{}, ErrCodeNotFound
	}

	pending, err := s.loadPendingLocked()
	if err != nil {
		return Request{}, err
	}

	index := -1
	var req Request
	for i, candidate := range pending {
		if normalizeCode(candidate.Code) == code {
			index = i
			req = candidate
			break
		}
	}
	if index == -1 {
		return Request{}, ErrCodeNotFound
	}

	pending = append(pending[:index], pending[index+1:]...)
	if err := s.writeJSONLocked(s.pendingPath(), pending); err != nil {
		return Request{}, err
	}

	return req, nil
}

func (s *Store) allowlistPath() string {
	return filepath.Join(s.credentialsDir(), fmt.Sprintf("%s-allowFrom.json", s.provider))
}

func (s *Store) pendingPath() string {
	return filepath.Join(s.credentialsDir(), fmt.Sprintf("%s-pairing.json", s.provider))
}

func (s *Store) credentialsDir() string {
	return filepath.Join(s.stateDir, "credentials")
}

func (s *Store) loadAllowlistLocked() ([]string, error) {
	path := s.allowlistPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return []string{}, nil
	}
	var allowlist []string
	if err := json.Unmarshal(data, &allowlist); err != nil {
		return nil, err
	}
	return sanitizeAllowlist(allowlist), nil
}

func (s *Store) loadPendingLocked() ([]Request, error) {
	path := s.pendingPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Request{}, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return []Request{}, nil
	}
	var pending []Request
	if err := json.Unmarshal(data, &pending); err != nil {
		return nil, err
	}
	filtered := pending[:0]
	now := s.now()
	for _, req := range pending {
		if req.Code == "" || req.SenderID == "" {
			continue
		}
		if req.ExpiresAt.IsZero() || req.ExpiresAt.After(now) {
			req.Code = normalizeCode(req.Code)
			filtered = append(filtered, req)
		}
	}
	if len(filtered) != len(pending) {
		if err := s.writeJSONLocked(path, filtered); err != nil {
			return nil, err
		}
	}
	return filtered, nil
}

func (s *Store) writeJSONLocked(path string, payload any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0600)
}

func (s *Store) generateCode(existing map[string]struct{}) (string, error) {
	for i := 0; i < 20; i++ {
		code, err := randomCode(s.rand, CodeLength)
		if err != nil {
			return "", err
		}
		if _, ok := existing[code]; ok {
			continue
		}
		return code, nil
	}
	return "", errors.New("failed to generate unique pairing code")
}

func randomCode(r io.Reader, length int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	if length <= 0 {
		return "", errors.New("invalid code length")
	}
	buf := make([]byte, length)
	_, err := io.ReadFull(r, buf)
	if err != nil {
		return "", err
	}
	out := make([]byte, length)
	for i := range buf {
		out[i] = alphabet[int(buf[i])%len(alphabet)]
	}
	return string(out), nil
}

func normalizeCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func sanitizeAllowlist(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		normalized := normalizeAllowToken(trimmed)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func normalizeAllowToken(value string) string {
	token := strings.TrimSpace(value)
	if token == "" {
		return ""
	}
	token = strings.TrimPrefix(token, "@")
	token = strings.TrimPrefix(token, "#")
	if idx := strings.Index(token, ":"); idx >= 0 {
		token = token[idx+1:]
	}
	return strings.ToLower(strings.TrimSpace(token))
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
