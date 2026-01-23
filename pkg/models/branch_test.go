package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBranchStatus_Constants(t *testing.T) {
	tests := []struct {
		constant BranchStatus
		expected string
	}{
		{BranchStatusActive, "active"},
		{BranchStatusMerged, "merged"},
		{BranchStatusArchived, "archived"},
	}

	for _, tt := range tests {
		t.Run(string(tt.constant), func(t *testing.T) {
			if string(tt.constant) != tt.expected {
				t.Errorf("constant = %q, want %q", tt.constant, tt.expected)
			}
		})
	}
}

func TestMergeStrategy_Constants(t *testing.T) {
	tests := []struct {
		constant MergeStrategy
		expected string
	}{
		{MergeStrategyReplace, "replace"},
		{MergeStrategyContinue, "continue"},
		{MergeStrategyInterleave, "interleave"},
	}

	for _, tt := range tests {
		t.Run(string(tt.constant), func(t *testing.T) {
			if string(tt.constant) != tt.expected {
				t.Errorf("constant = %q, want %q", tt.constant, tt.expected)
			}
		})
	}
}

func TestBranch_Struct(t *testing.T) {
	now := time.Now()
	parentID := "parent-123"
	branch := Branch{
		ID:             "branch-123",
		SessionID:      "session-456",
		ParentBranchID: &parentID,
		Name:           "feature-branch",
		Description:    "Testing a new feature",
		BranchPoint:    10,
		Status:         BranchStatusActive,
		IsPrimary:      false,
		Metadata:       map[string]any{"key": "value"},
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if branch.ID != "branch-123" {
		t.Errorf("ID = %q, want %q", branch.ID, "branch-123")
	}
	if branch.Status != BranchStatusActive {
		t.Errorf("Status = %v, want %v", branch.Status, BranchStatusActive)
	}
	if branch.BranchPoint != 10 {
		t.Errorf("BranchPoint = %d, want 10", branch.BranchPoint)
	}
}

func TestBranch_JSONRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	parentID := "parent-123"
	original := Branch{
		ID:             "branch-123",
		SessionID:      "session-456",
		ParentBranchID: &parentID,
		Name:           "test-branch",
		Status:         BranchStatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded Branch
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.ParentBranchID == nil || *decoded.ParentBranchID != parentID {
		t.Errorf("ParentBranchID = %v, want %q", decoded.ParentBranchID, parentID)
	}
}

func TestNewBranch(t *testing.T) {
	branch := NewBranch("session-123", "test-branch")

	if branch.SessionID != "session-123" {
		t.Errorf("SessionID = %q, want %q", branch.SessionID, "session-123")
	}
	if branch.Name != "test-branch" {
		t.Errorf("Name = %q, want %q", branch.Name, "test-branch")
	}
	if branch.Status != BranchStatusActive {
		t.Errorf("Status = %v, want %v", branch.Status, BranchStatusActive)
	}
	if branch.IsPrimary {
		t.Error("IsPrimary should be false")
	}
	if branch.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

func TestNewPrimaryBranch(t *testing.T) {
	branch := NewPrimaryBranch("session-123")

	if branch.SessionID != "session-123" {
		t.Errorf("SessionID = %q, want %q", branch.SessionID, "session-123")
	}
	if branch.Name != "main" {
		t.Errorf("Name = %q, want %q", branch.Name, "main")
	}
	if !branch.IsPrimary {
		t.Error("IsPrimary should be true")
	}
	if branch.Description != "Primary conversation branch" {
		t.Errorf("Description = %q", branch.Description)
	}
}

func TestBranch_IsRoot(t *testing.T) {
	t.Run("root branch (no parent)", func(t *testing.T) {
		branch := &Branch{ID: "branch-1"}
		if !branch.IsRoot() {
			t.Error("expected IsRoot() to be true")
		}
	})

	t.Run("child branch (has parent)", func(t *testing.T) {
		parentID := "parent-1"
		branch := &Branch{ID: "branch-2", ParentBranchID: &parentID}
		if branch.IsRoot() {
			t.Error("expected IsRoot() to be false")
		}
	})
}

func TestBranch_CanMerge(t *testing.T) {
	t.Run("active non-primary can merge", func(t *testing.T) {
		branch := &Branch{Status: BranchStatusActive, IsPrimary: false}
		if !branch.CanMerge() {
			t.Error("expected CanMerge() to be true")
		}
	})

	t.Run("primary branch cannot merge", func(t *testing.T) {
		branch := &Branch{Status: BranchStatusActive, IsPrimary: true}
		if branch.CanMerge() {
			t.Error("expected CanMerge() to be false for primary")
		}
	})

	t.Run("merged branch cannot merge", func(t *testing.T) {
		branch := &Branch{Status: BranchStatusMerged, IsPrimary: false}
		if branch.CanMerge() {
			t.Error("expected CanMerge() to be false for merged")
		}
	})

	t.Run("archived branch cannot merge", func(t *testing.T) {
		branch := &Branch{Status: BranchStatusArchived, IsPrimary: false}
		if branch.CanMerge() {
			t.Error("expected CanMerge() to be false for archived")
		}
	})
}

func TestBranch_CanArchive(t *testing.T) {
	t.Run("active non-primary can archive", func(t *testing.T) {
		branch := &Branch{Status: BranchStatusActive, IsPrimary: false}
		if !branch.CanArchive() {
			t.Error("expected CanArchive() to be true")
		}
	})

	t.Run("primary branch cannot archive", func(t *testing.T) {
		branch := &Branch{Status: BranchStatusActive, IsPrimary: true}
		if branch.CanArchive() {
			t.Error("expected CanArchive() to be false for primary")
		}
	})
}

func TestBranchMerge_Struct(t *testing.T) {
	now := time.Now()
	merge := BranchMerge{
		ID:                   "merge-123",
		SourceBranchID:       "branch-a",
		TargetBranchID:       "branch-b",
		Strategy:             MergeStrategyContinue,
		SourceSequenceStart:  5,
		SourceSequenceEnd:    15,
		TargetSequenceInsert: 10,
		MessageCount:         11,
		Metadata:             map[string]any{"reason": "completed feature"},
		MergedAt:             now,
		MergedBy:             "user-123",
	}

	if merge.Strategy != MergeStrategyContinue {
		t.Errorf("Strategy = %v, want %v", merge.Strategy, MergeStrategyContinue)
	}
	if merge.MessageCount != 11 {
		t.Errorf("MessageCount = %d, want 11", merge.MessageCount)
	}
}

func TestBranchTree_Struct(t *testing.T) {
	tree := BranchTree{
		Branch:       &Branch{ID: "branch-1"},
		Children:     []*BranchTree{{Branch: &Branch{ID: "branch-2"}}},
		MessageCount: 25,
		Depth:        0,
	}

	if tree.Branch == nil {
		t.Fatal("Branch is nil")
	}
	if len(tree.Children) != 1 {
		t.Errorf("Children length = %d, want 1", len(tree.Children))
	}
	if tree.Depth != 0 {
		t.Errorf("Depth = %d, want 0", tree.Depth)
	}
}

func TestBranchPath_Struct(t *testing.T) {
	path := BranchPath{
		BranchID: "branch-3",
		Path:     []string{"branch-1", "branch-2", "branch-3"},
		Branches: []*Branch{
			{ID: "branch-1"},
			{ID: "branch-2"},
			{ID: "branch-3"},
		},
	}

	if path.BranchID != "branch-3" {
		t.Errorf("BranchID = %q, want %q", path.BranchID, "branch-3")
	}
	if len(path.Path) != 3 {
		t.Errorf("Path length = %d, want 3", len(path.Path))
	}
}

func TestBranchStats_Struct(t *testing.T) {
	now := time.Now()
	stats := BranchStats{
		BranchID:         "branch-123",
		TotalMessages:    100,
		OwnMessages:      25,
		ChildBranchCount: 3,
		LastMessageAt:    &now,
	}

	if stats.TotalMessages != 100 {
		t.Errorf("TotalMessages = %d, want 100", stats.TotalMessages)
	}
	if stats.OwnMessages != 25 {
		t.Errorf("OwnMessages = %d, want 25", stats.OwnMessages)
	}
}

func TestBranchCompare_Struct(t *testing.T) {
	compare := BranchCompare{
		SourceBranch:    &Branch{ID: "source"},
		TargetBranch:    &Branch{ID: "target"},
		CommonAncestor:  &Branch{ID: "ancestor"},
		DivergencePoint: 50,
		SourceAhead:     10,
		TargetAhead:     5,
	}

	if compare.DivergencePoint != 50 {
		t.Errorf("DivergencePoint = %d, want 50", compare.DivergencePoint)
	}
	if compare.SourceAhead != 10 {
		t.Errorf("SourceAhead = %d, want 10", compare.SourceAhead)
	}
}
