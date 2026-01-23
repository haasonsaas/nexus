package artifacts

import (
	"testing"

	pb "github.com/haasonsaas/nexus/pkg/proto"
)

func TestRedactionPolicy(t *testing.T) {
	policy, err := NewRedactionPolicy(RedactionConfig{
		Enabled:          true,
		Types:            []string{"screenshot"},
		MimeTypes:        []string{"image/*"},
		FilenamePatterns: []string{`secret-.*\.png`},
	})
	if err != nil {
		t.Fatalf("NewRedactionPolicy: %v", err)
	}

	tests := []struct {
		name     string
		artifact *pb.Artifact
		want     bool
	}{
		{
			name: "type match",
			artifact: &pb.Artifact{
				Type: "screenshot",
			},
			want: true,
		},
		{
			name: "mime prefix match",
			artifact: &pb.Artifact{
				Type:     "file",
				MimeType: "image/png",
			},
			want: true,
		},
		{
			name: "filename regex match",
			artifact: &pb.Artifact{
				Type:     "file",
				MimeType: "application/octet-stream",
				Filename: "secret-123.png",
			},
			want: true,
		},
		{
			name: "no match",
			artifact: &pb.Artifact{
				Type:     "file",
				MimeType: "text/plain",
				Filename: "notes.txt",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := policy.ShouldRedact(tt.artifact); got != tt.want {
				t.Fatalf("ShouldRedact = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRedactionPolicyApply(t *testing.T) {
	policy, err := NewRedactionPolicy(RedactionConfig{
		Enabled:         true,
		Types:           []string{"recording"},
	})
	if err != nil {
		t.Fatalf("NewRedactionPolicy: %v", err)
	}

	artifact := &pb.Artifact{
		Type: "recording",
	}
	if !policy.Apply(artifact) {
		t.Fatal("expected redaction to apply")
	}
	if artifact.Id == "" {
		t.Fatal("expected id to be set")
	}
	if artifact.Reference == "" {
		t.Fatal("expected reference to be set")
	}
	if artifact.Data != nil {
		t.Fatal("expected data to be cleared")
	}
	if artifact.Size != 0 {
		t.Fatal("expected size to be 0")
	}
}
