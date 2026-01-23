package artifacts

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	pb "github.com/haasonsaas/nexus/pkg/proto"
)

// RedactionConfig defines rules for redacting sensitive artifacts.
type RedactionConfig struct {
	Enabled          bool
	Types            []string
	MimeTypes        []string
	FilenamePatterns []string
}

// RedactionPolicy evaluates artifacts against redaction rules.
type RedactionPolicy struct {
	enabled          bool
	typeSet          map[string]struct{}
	mimeExact        map[string]struct{}
	mimePrefixes     []string
	filenamePatterns []*regexp.Regexp
}

// NewRedactionPolicy compiles a policy from config.
func NewRedactionPolicy(cfg RedactionConfig) (*RedactionPolicy, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	policy := &RedactionPolicy{
		enabled:   true,
		typeSet:   make(map[string]struct{}),
		mimeExact: make(map[string]struct{}),
	}

	for _, t := range cfg.Types {
		t = strings.TrimSpace(strings.ToLower(t))
		if t == "" {
			continue
		}
		policy.typeSet[t] = struct{}{}
	}

	for _, m := range cfg.MimeTypes {
		m = strings.TrimSpace(strings.ToLower(m))
		if m == "" {
			continue
		}
		if strings.HasSuffix(m, "/*") {
			prefix := strings.TrimSuffix(m, "/*")
			if prefix != "" {
				policy.mimePrefixes = append(policy.mimePrefixes, prefix+"/")
			}
			continue
		}
		policy.mimeExact[m] = struct{}{}
	}

	for _, pattern := range cfg.FilenamePatterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid redaction filename pattern %q: %w", pattern, err)
		}
		policy.filenamePatterns = append(policy.filenamePatterns, re)
	}

	return policy, nil
}

// ShouldRedact returns true if the artifact matches redaction rules.
func (p *RedactionPolicy) ShouldRedact(artifact *pb.Artifact) bool {
	if p == nil || !p.enabled || artifact == nil {
		return false
	}

	if artifact.Type != "" {
		if _, ok := p.typeSet[strings.ToLower(artifact.Type)]; ok {
			return true
		}
	}

	if artifact.MimeType != "" {
		mime := strings.ToLower(artifact.MimeType)
		if _, ok := p.mimeExact[mime]; ok {
			return true
		}
		for _, prefix := range p.mimePrefixes {
			if strings.HasPrefix(mime, prefix) {
				return true
			}
		}
	}

	if artifact.Filename != "" {
		for _, re := range p.filenamePatterns {
			if re.MatchString(artifact.Filename) {
				return true
			}
		}
	}

	return false
}

// Apply redacts the artifact in-place and returns true if redaction occurred.
func (p *RedactionPolicy) Apply(artifact *pb.Artifact) bool {
	if !p.ShouldRedact(artifact) {
		return false
	}
	if artifact.Id == "" {
		artifact.Id = uuid.NewString()
	}
	artifact.Reference = fmt.Sprintf("redacted://%s", artifact.Id)
	artifact.Data = nil
	artifact.Size = 0
	return true
}
