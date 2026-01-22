package sessions

import (
	"context"
	"errors"

	"github.com/haasonsaas/nexus/pkg/models"
)

// Common branch store errors.
var (
	ErrBranchNotFound      = errors.New("branch not found")
	ErrBranchAlreadyExists = errors.New("branch already exists")
	ErrPrimaryBranchExists = errors.New("session already has a primary branch")
	ErrCannotDeletePrimary = errors.New("cannot delete primary branch")
	ErrCannotMergePrimary  = errors.New("cannot merge primary branch")
	ErrInvalidBranchPoint  = errors.New("invalid branch point")
	ErrBranchMerged        = errors.New("branch has been merged")
	ErrBranchArchived      = errors.New("branch is archived")
	ErrCircularReference   = errors.New("circular branch reference detected")
)

// BranchStore defines the interface for branch persistence operations.
type BranchStore interface {
	// Branch CRUD operations

	// CreateBranch creates a new branch in a session.
	// If parentBranchID is nil and this is the first branch, it becomes the primary branch.
	CreateBranch(ctx context.Context, branch *models.Branch) error

	// GetBranch retrieves a branch by ID.
	GetBranch(ctx context.Context, branchID string) (*models.Branch, error)

	// UpdateBranch updates an existing branch.
	UpdateBranch(ctx context.Context, branch *models.Branch) error

	// DeleteBranch deletes a branch and optionally its messages.
	// Returns ErrCannotDeletePrimary if attempting to delete the primary branch.
	DeleteBranch(ctx context.Context, branchID string, deleteMessages bool) error

	// Branch queries

	// GetPrimaryBranch returns the primary branch for a session.
	GetPrimaryBranch(ctx context.Context, sessionID string) (*models.Branch, error)

	// ListBranches returns all branches for a session.
	ListBranches(ctx context.Context, sessionID string, opts BranchListOptions) ([]*models.Branch, error)

	// GetBranchTree returns the hierarchical branch structure for a session.
	GetBranchTree(ctx context.Context, sessionID string) (*models.BranchTree, error)

	// GetFullBranchPath returns the ancestry path from root to the specified branch.
	// Uses recursive CTE for efficient traversal.
	GetFullBranchPath(ctx context.Context, branchID string) (*models.BranchPath, error)

	// GetBranchStats returns statistics for a branch.
	GetBranchStats(ctx context.Context, branchID string) (*models.BranchStats, error)

	// Branch operations

	// ForkBranch creates a new branch from an existing branch at the specified message sequence.
	// The new branch inherits all messages up to and including branchPoint.
	ForkBranch(ctx context.Context, parentBranchID string, branchPoint int64, name string) (*models.Branch, error)

	// MergeBranch merges a source branch into a target branch using the specified strategy.
	MergeBranch(ctx context.Context, sourceBranchID, targetBranchID string, strategy models.MergeStrategy) (*models.BranchMerge, error)

	// ArchiveBranch marks a branch as archived.
	ArchiveBranch(ctx context.Context, branchID string) error

	// CompareBranches compares two branches and returns their differences.
	CompareBranches(ctx context.Context, sourceBranchID, targetBranchID string) (*models.BranchCompare, error)

	// Branch-aware message operations

	// AppendMessageToBranch adds a message to a specific branch.
	// If branchID is empty, uses the session's primary branch.
	AppendMessageToBranch(ctx context.Context, sessionID, branchID string, msg *models.Message) error

	// GetBranchHistory retrieves messages for a branch, including inherited messages from ancestors.
	// The limit applies to the total messages returned.
	GetBranchHistory(ctx context.Context, branchID string, limit int) ([]*models.Message, error)

	// GetBranchHistoryFromSequence retrieves messages from a specific sequence number.
	// Useful for paginating through branch history.
	GetBranchHistoryFromSequence(ctx context.Context, branchID string, fromSequence int64, limit int) ([]*models.Message, error)

	// GetBranchOwnMessages retrieves only messages directly belonging to this branch (not inherited).
	GetBranchOwnMessages(ctx context.Context, branchID string, limit int) ([]*models.Message, error)

	// EnsurePrimaryBranch creates a primary branch for a session if one doesn't exist.
	// This is used for backward compatibility with existing sessions.
	EnsurePrimaryBranch(ctx context.Context, sessionID string) (*models.Branch, error)

	// MigrateSessionToBranches migrates an existing session's messages to the primary branch.
	// This is used for backward compatibility with existing data.
	MigrateSessionToBranches(ctx context.Context, sessionID string) error
}

// BranchListOptions configures branch listing queries.
type BranchListOptions struct {
	// Status filters by branch status.
	Status *models.BranchStatus

	// ParentBranchID filters by parent branch (nil means root branches only).
	ParentBranchID *string

	// IncludeArchived includes archived branches in results.
	IncludeArchived bool

	// Limit limits the number of results.
	Limit int

	// Offset for pagination.
	Offset int

	// OrderBy specifies sort order ("created_at", "updated_at", "name").
	OrderBy string

	// OrderDesc reverses sort order.
	OrderDesc bool
}

// DefaultBranchListOptions returns sensible defaults for branch listing.
func DefaultBranchListOptions() BranchListOptions {
	return BranchListOptions{
		IncludeArchived: false,
		Limit:           50,
		OrderBy:         "created_at",
		OrderDesc:       true,
	}
}

// BranchHistoryOptions configures branch history retrieval.
type BranchHistoryOptions struct {
	// Limit is the maximum number of messages to return.
	Limit int

	// FromSequence starts from this sequence number (inclusive).
	FromSequence int64

	// ToSequence ends at this sequence number (inclusive).
	ToSequence int64

	// IncludeInherited includes messages inherited from ancestor branches.
	IncludeInherited bool

	// ReverseOrder returns messages in reverse chronological order.
	ReverseOrder bool
}

// DefaultBranchHistoryOptions returns sensible defaults for history retrieval.
func DefaultBranchHistoryOptions() BranchHistoryOptions {
	return BranchHistoryOptions{
		Limit:            100,
		IncludeInherited: true,
		ReverseOrder:     false,
	}
}
