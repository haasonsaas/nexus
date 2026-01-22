package models

import (
	"time"
)

// BranchStatus represents the current state of a branch.
type BranchStatus string

const (
	BranchStatusActive   BranchStatus = "active"
	BranchStatusMerged   BranchStatus = "merged"
	BranchStatusArchived BranchStatus = "archived"
)

// MergeStrategy defines how branches are merged.
type MergeStrategy string

const (
	// MergeStrategyReplace replaces the target branch history with the source.
	MergeStrategyReplace MergeStrategy = "replace"

	// MergeStrategyContinue appends source messages after target's divergence point.
	MergeStrategyContinue MergeStrategy = "continue"

	// MergeStrategyInterleave interleaves messages by timestamp.
	MergeStrategyInterleave MergeStrategy = "interleave"
)

// Branch represents a conversation branch within a session.
// Branches allow exploring alternative conversation paths from any point.
type Branch struct {
	// ID is the unique identifier for this branch.
	ID string `json:"id"`

	// SessionID is the session this branch belongs to.
	SessionID string `json:"session_id"`

	// ParentBranchID is the ID of the parent branch (nil for primary branch).
	ParentBranchID *string `json:"parent_branch_id,omitempty"`

	// Name is a human-readable name for the branch.
	Name string `json:"name"`

	// Description provides optional context about the branch purpose.
	Description string `json:"description,omitempty"`

	// BranchPoint is the sequence number in the parent branch where this branch diverges.
	// Messages with sequence <= BranchPoint are inherited from parent.
	BranchPoint int64 `json:"branch_point"`

	// Status indicates whether the branch is active, merged, or archived.
	Status BranchStatus `json:"status"`

	// IsPrimary indicates if this is the session's primary (main) branch.
	IsPrimary bool `json:"is_primary"`

	// Metadata stores additional branch-specific data.
	Metadata map[string]any `json:"metadata,omitempty"`

	// CreatedAt is when the branch was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the branch was last modified.
	UpdatedAt time.Time `json:"updated_at"`

	// MergedAt is when the branch was merged (if applicable).
	MergedAt *time.Time `json:"merged_at,omitempty"`
}

// BranchMerge records a merge operation between branches.
type BranchMerge struct {
	// ID is the unique identifier for this merge record.
	ID string `json:"id"`

	// SourceBranchID is the branch being merged from.
	SourceBranchID string `json:"source_branch_id"`

	// TargetBranchID is the branch being merged into.
	TargetBranchID string `json:"target_branch_id"`

	// Strategy is the merge strategy used.
	Strategy MergeStrategy `json:"strategy"`

	// SourceSequenceStart is the first message sequence from source included in merge.
	SourceSequenceStart int64 `json:"source_sequence_start"`

	// SourceSequenceEnd is the last message sequence from source included in merge.
	SourceSequenceEnd int64 `json:"source_sequence_end"`

	// TargetSequenceInsert is where in target the messages were inserted.
	TargetSequenceInsert int64 `json:"target_sequence_insert"`

	// MessageCount is the number of messages merged.
	MessageCount int `json:"message_count"`

	// Metadata stores additional merge-specific data.
	Metadata map[string]any `json:"metadata,omitempty"`

	// MergedAt is when the merge was performed.
	MergedAt time.Time `json:"merged_at"`

	// MergedBy is the user or system that performed the merge.
	MergedBy string `json:"merged_by,omitempty"`
}

// BranchTree represents the hierarchical structure of branches in a session.
type BranchTree struct {
	// Branch is the branch at this node.
	Branch *Branch `json:"branch"`

	// Children are branches that fork from this branch.
	Children []*BranchTree `json:"children,omitempty"`

	// MessageCount is the number of messages unique to this branch.
	MessageCount int `json:"message_count"`

	// Depth is the nesting level (0 for primary branch).
	Depth int `json:"depth"`
}

// BranchPath represents the full ancestry path to a branch.
type BranchPath struct {
	// BranchID is the target branch.
	BranchID string `json:"branch_id"`

	// Path is the ordered list of branch IDs from root to target.
	Path []string `json:"path"`

	// Branches contains the full branch data for each ID in Path.
	Branches []*Branch `json:"branches,omitempty"`
}

// BranchStats contains statistics about a branch.
type BranchStats struct {
	// BranchID is the branch these stats are for.
	BranchID string `json:"branch_id"`

	// TotalMessages is the total number of messages accessible from this branch.
	// This includes inherited messages from ancestors.
	TotalMessages int `json:"total_messages"`

	// OwnMessages is the number of messages unique to this branch.
	OwnMessages int `json:"own_messages"`

	// ChildBranchCount is the number of direct child branches.
	ChildBranchCount int `json:"child_branch_count"`

	// LastMessageAt is the timestamp of the most recent message.
	LastMessageAt *time.Time `json:"last_message_at,omitempty"`
}

// BranchCompare contains comparison data between two branches.
type BranchCompare struct {
	// SourceBranch is the source branch being compared.
	SourceBranch *Branch `json:"source_branch"`

	// TargetBranch is the target branch being compared against.
	TargetBranch *Branch `json:"target_branch"`

	// CommonAncestor is the closest common ancestor branch.
	CommonAncestor *Branch `json:"common_ancestor,omitempty"`

	// DivergencePoint is the sequence number where branches diverge.
	DivergencePoint int64 `json:"divergence_point"`

	// SourceAhead is messages unique to source after divergence.
	SourceAhead int `json:"source_ahead"`

	// TargetAhead is messages unique to target after divergence.
	TargetAhead int `json:"target_ahead"`
}

// NewBranch creates a new branch with default values.
func NewBranch(sessionID, name string) *Branch {
	now := time.Now()
	return &Branch{
		SessionID: sessionID,
		Name:      name,
		Status:    BranchStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// NewPrimaryBranch creates the primary branch for a session.
func NewPrimaryBranch(sessionID string) *Branch {
	branch := NewBranch(sessionID, "main")
	branch.IsPrimary = true
	branch.Description = "Primary conversation branch"
	return branch
}

// IsRoot returns true if this is a root branch (no parent).
func (b *Branch) IsRoot() bool {
	return b.ParentBranchID == nil
}

// CanMerge checks if this branch can be merged.
func (b *Branch) CanMerge() bool {
	return b.Status == BranchStatusActive && !b.IsPrimary
}

// CanArchive checks if this branch can be archived.
func (b *Branch) CanArchive() bool {
	return b.Status == BranchStatusActive && !b.IsPrimary
}
