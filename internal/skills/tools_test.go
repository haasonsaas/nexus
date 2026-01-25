package skills

import (
	"testing"

	exectools "github.com/haasonsaas/nexus/internal/tools/exec"
)

func TestBuildSkillTools(t *testing.T) {
	mgr := exectools.NewManager(t.TempDir())
	skill := &SkillEntry{
		Name: "test",
		Path: t.TempDir(),
		Metadata: &SkillMetadata{
			Tools: []SkillToolSpec{
				{Name: "tool1", Description: "desc"},
			},
		},
	}
	tools := BuildSkillTools(skill, mgr)
	if len(tools) != 1 {
		t.Fatalf("expected tool, got %d", len(tools))
	}
	if tools[0].Name() != "tool1" {
		t.Fatalf("expected tool name")
	}
}
