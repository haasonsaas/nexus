package sessions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/pkg/models"
)

// CockroachBranchStore implements BranchStore using CockroachDB.
type CockroachBranchStore struct {
	db *sql.DB
}

// NewCockroachBranchStore creates a new CockroachDB branch store.
func NewCockroachBranchStore(db *sql.DB) *CockroachBranchStore {
	return &CockroachBranchStore{db: db}
}

// CreateBranch creates a new branch in a session.
func (s *CockroachBranchStore) CreateBranch(ctx context.Context, branch *models.Branch) error {
	if branch.ID == "" {
		branch.ID = uuid.NewString()
	}
	if branch.CreatedAt.IsZero() {
		branch.CreatedAt = time.Now()
	}
	branch.UpdatedAt = branch.CreatedAt

	metadata, err := json.Marshal(branch.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		INSERT INTO branches (id, session_id, parent_branch_id, name, description, branch_point, status, is_primary, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err = s.db.ExecContext(ctx, query,
		branch.ID, branch.SessionID, branch.ParentBranchID, branch.Name, branch.Description,
		branch.BranchPoint, branch.Status, branch.IsPrimary, metadata, branch.CreatedAt, branch.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create branch: %w", err)
	}
	return nil
}

// GetBranch retrieves a branch by ID.
func (s *CockroachBranchStore) GetBranch(ctx context.Context, branchID string) (*models.Branch, error) {
	query := `
		SELECT id, session_id, parent_branch_id, name, description, branch_point, status, is_primary, metadata, created_at, updated_at, merged_at
		FROM branches WHERE id = $1
	`
	return s.scanBranch(s.db.QueryRowContext(ctx, query, branchID))
}

// UpdateBranch updates an existing branch.
func (s *CockroachBranchStore) UpdateBranch(ctx context.Context, branch *models.Branch) error {
	branch.UpdatedAt = time.Now()
	metadata, err := json.Marshal(branch.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		UPDATE branches SET name = $1, description = $2, status = $3, metadata = $4, updated_at = $5, merged_at = $6
		WHERE id = $7
	`
	result, err := s.db.ExecContext(ctx, query,
		branch.Name, branch.Description, branch.Status, metadata, branch.UpdatedAt, branch.MergedAt, branch.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update branch: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return ErrBranchNotFound
	}
	return nil
}

// DeleteBranch deletes a branch and optionally its messages.
func (s *CockroachBranchStore) DeleteBranch(ctx context.Context, branchID string, deleteMessages bool) error {
	branch, err := s.GetBranch(ctx, branchID)
	if err != nil {
		return err
	}
	if branch.IsPrimary {
		return ErrCannotDeletePrimary
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			_ = err
		}
	}()

	if deleteMessages {
		_, err = tx.ExecContext(ctx, "DELETE FROM messages WHERE branch_id = $1", branchID)
		if err != nil {
			return fmt.Errorf("failed to delete messages: %w", err)
		}
	}

	_, err = tx.ExecContext(ctx, "DELETE FROM branches WHERE id = $1", branchID)
	if err != nil {
		return fmt.Errorf("failed to delete branch: %w", err)
	}

	return tx.Commit()
}

// GetPrimaryBranch returns the primary branch for a session.
func (s *CockroachBranchStore) GetPrimaryBranch(ctx context.Context, sessionID string) (*models.Branch, error) {
	query := `
		SELECT id, session_id, parent_branch_id, name, description, branch_point, status, is_primary, metadata, created_at, updated_at, merged_at
		FROM branches WHERE session_id = $1 AND is_primary = true
	`
	return s.scanBranch(s.db.QueryRowContext(ctx, query, sessionID))
}

// ListBranches returns all branches for a session.
func (s *CockroachBranchStore) ListBranches(ctx context.Context, sessionID string, opts BranchListOptions) ([]*models.Branch, error) {
	query := `SELECT id, session_id, parent_branch_id, name, description, branch_point, status, is_primary, metadata, created_at, updated_at, merged_at FROM branches WHERE session_id = $1`
	args := []interface{}{sessionID}
	argPos := 2

	if opts.Status != nil {
		query += fmt.Sprintf(" AND status = $%d", argPos)
		args = append(args, *opts.Status)
		argPos++
	}
	if !opts.IncludeArchived {
		query += fmt.Sprintf(" AND status != $%d", argPos)
		args = append(args, models.BranchStatusArchived)
		argPos++
	}

	orderCol := "created_at"
	if opts.OrderBy != "" {
		orderCol = opts.OrderBy
	}
	if opts.OrderDesc {
		query += fmt.Sprintf(" ORDER BY %s DESC", orderCol)
	} else {
		query += fmt.Sprintf(" ORDER BY %s ASC", orderCol)
	}

	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argPos)
		args = append(args, opts.Limit)
		argPos++
	}
	if opts.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argPos)
		args = append(args, opts.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
	}
	defer rows.Close()

	return s.scanBranches(rows)
}

// GetFullBranchPath returns the ancestry path using recursive CTE.
func (s *CockroachBranchStore) GetFullBranchPath(ctx context.Context, branchID string) (*models.BranchPath, error) {
	query := `
		WITH RECURSIVE branch_path AS (
			SELECT id, session_id, parent_branch_id, name, description, branch_point, status, is_primary, metadata, created_at, updated_at, merged_at, 0 AS depth
			FROM branches WHERE id = $1
			UNION ALL
			SELECT b.id, b.session_id, b.parent_branch_id, b.name, b.description, b.branch_point, b.status, b.is_primary, b.metadata, b.created_at, b.updated_at, b.merged_at, bp.depth + 1
			FROM branches b
			INNER JOIN branch_path bp ON b.id = bp.parent_branch_id
		)
		SELECT id, session_id, parent_branch_id, name, description, branch_point, status, is_primary, metadata, created_at, updated_at, merged_at
		FROM branch_path ORDER BY depth DESC
	`
	rows, err := s.db.QueryContext(ctx, query, branchID)
	if err != nil {
		return nil, fmt.Errorf("failed to get branch path: %w", err)
	}
	defer rows.Close()

	branches, err := s.scanBranches(rows)
	if err != nil {
		return nil, err
	}
	if len(branches) == 0 {
		return nil, ErrBranchNotFound
	}

	path := &models.BranchPath{
		BranchID: branchID,
		Path:     make([]string, len(branches)),
		Branches: branches,
	}
	for i, b := range branches {
		path.Path[i] = b.ID
	}
	return path, nil
}

// GetBranchTree returns the hierarchical branch structure.
func (s *CockroachBranchStore) GetBranchTree(ctx context.Context, sessionID string) (*models.BranchTree, error) {
	branches, err := s.ListBranches(ctx, sessionID, BranchListOptions{IncludeArchived: true, Limit: 1000})
	if err != nil {
		return nil, err
	}
	if len(branches) == 0 {
		return nil, ErrBranchNotFound
	}

	// Build tree from flat list
	nodeMap := make(map[string]*models.BranchTree)
	var root *models.BranchTree

	for _, b := range branches {
		nodeMap[b.ID] = &models.BranchTree{Branch: b, Children: []*models.BranchTree{}}
	}

	for _, b := range branches {
		node := nodeMap[b.ID]
		if b.ParentBranchID == nil {
			root = node
			node.Depth = 0
		} else if parent, ok := nodeMap[*b.ParentBranchID]; ok {
			parent.Children = append(parent.Children, node)
			node.Depth = parent.Depth + 1
		}
	}
	return root, nil
}

// GetBranchStats returns statistics for a branch.
func (s *CockroachBranchStore) GetBranchStats(ctx context.Context, branchID string) (*models.BranchStats, error) {
	stats := &models.BranchStats{BranchID: branchID}

	// Get own messages count
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM messages WHERE branch_id = $1", branchID).Scan(&stats.OwnMessages)
	if err != nil {
		return nil, fmt.Errorf("failed to count own messages: %w", err)
	}

	// Get child branch count
	err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM branches WHERE parent_branch_id = $1", branchID).Scan(&stats.ChildBranchCount)
	if err != nil {
		return nil, fmt.Errorf("failed to count child branches: %w", err)
	}

	// Get total messages (inherited + own) using recursive CTE
	query := `
		WITH RECURSIVE branch_path AS (
			SELECT id, parent_branch_id, branch_point FROM branches WHERE id = $1
			UNION ALL
			SELECT b.id, b.parent_branch_id, b.branch_point
			FROM branches b INNER JOIN branch_path bp ON b.id = bp.parent_branch_id
		)
		SELECT COALESCE(SUM(
			CASE WHEN bp.parent_branch_id IS NULL THEN (SELECT COUNT(*) FROM messages WHERE branch_id = bp.id)
			ELSE (SELECT COUNT(*) FROM messages WHERE branch_id = bp.id AND sequence_num <= bp.branch_point)
			END
		), 0) + $2 FROM branch_path bp WHERE bp.id != $1
	`
	var inherited int
	err = s.db.QueryRowContext(ctx, query, branchID, stats.OwnMessages).Scan(&stats.TotalMessages)
	if err != nil {
		stats.TotalMessages = stats.OwnMessages + inherited
	}

	// Get last message timestamp
	var lastMsg sql.NullTime
	err = s.db.QueryRowContext(ctx, "SELECT MAX(created_at) FROM messages WHERE branch_id = $1", branchID).Scan(&lastMsg)
	if err == nil && lastMsg.Valid {
		stats.LastMessageAt = &lastMsg.Time
	}

	return stats, nil
}

func (s *CockroachBranchStore) scanBranch(row *sql.Row) (*models.Branch, error) {
	b := &models.Branch{}
	var metadataJSON []byte
	var mergedAt sql.NullTime

	err := row.Scan(&b.ID, &b.SessionID, &b.ParentBranchID, &b.Name, &b.Description,
		&b.BranchPoint, &b.Status, &b.IsPrimary, &metadataJSON, &b.CreatedAt, &b.UpdatedAt, &mergedAt)
	if err == sql.ErrNoRows {
		return nil, ErrBranchNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to scan branch: %w", err)
	}

	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &b.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal branch metadata: %w", err)
		}
	}
	if mergedAt.Valid {
		b.MergedAt = &mergedAt.Time
	}
	return b, nil
}

func (s *CockroachBranchStore) scanBranches(rows *sql.Rows) ([]*models.Branch, error) {
	var branches []*models.Branch
	for rows.Next() {
		b := &models.Branch{}
		var metadataJSON []byte
		var mergedAt sql.NullTime

		err := rows.Scan(&b.ID, &b.SessionID, &b.ParentBranchID, &b.Name, &b.Description,
			&b.BranchPoint, &b.Status, &b.IsPrimary, &metadataJSON, &b.CreatedAt, &b.UpdatedAt, &mergedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan branch: %w", err)
		}

		if len(metadataJSON) > 0 {
			if err := json.Unmarshal(metadataJSON, &b.Metadata); err != nil {
				return nil, fmt.Errorf("failed to unmarshal branch metadata: %w", err)
			}
		}
		if mergedAt.Valid {
			b.MergedAt = &mergedAt.Time
		}
		branches = append(branches, b)
	}
	return branches, rows.Err()
}

// ForkBranch creates a new branch from an existing branch at the specified sequence.
func (s *CockroachBranchStore) ForkBranch(ctx context.Context, parentBranchID string, branchPoint int64, name string) (*models.Branch, error) {
	parent, err := s.GetBranch(ctx, parentBranchID)
	if err != nil {
		return nil, err
	}

	branch := models.NewBranch(parent.SessionID, name)
	branch.ParentBranchID = &parentBranchID
	branch.BranchPoint = branchPoint

	if err := s.CreateBranch(ctx, branch); err != nil {
		return nil, err
	}
	return branch, nil
}

// MergeBranch merges a source branch into a target branch.
func (s *CockroachBranchStore) MergeBranch(ctx context.Context, sourceBranchID, targetBranchID string, strategy models.MergeStrategy) (*models.BranchMerge, error) {
	source, err := s.GetBranch(ctx, sourceBranchID)
	if err != nil {
		return nil, err
	}
	if source.IsPrimary {
		return nil, ErrCannotMergePrimary
	}
	if source.Status != models.BranchStatusActive {
		return nil, ErrBranchMerged
	}

	_, err = s.GetBranch(ctx, targetBranchID)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			_ = err
		}
	}()

	// Get source messages to merge
	var maxSeq int64
	err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(sequence_num), 0) FROM messages WHERE branch_id = $1", targetBranchID).Scan(&maxSeq)
	if err != nil {
		return nil, fmt.Errorf("failed to get max sequence: %w", err)
	}

	// Copy messages from source to target based on strategy
	var msgCount int
	switch strategy {
	case models.MergeStrategyReplace, models.MergeStrategyContinue:
		query := `
			INSERT INTO messages (id, session_id, branch_id, sequence_num, channel, channel_id, direction, role, content, attachments, tool_calls, tool_results, metadata, created_at)
			SELECT gen_random_uuid(), session_id, $1, sequence_num + $2, channel, channel_id, direction, role, content, attachments, tool_calls, tool_results, metadata, created_at
			FROM messages WHERE branch_id = $3 AND sequence_num > $4
		`
		result, err := tx.ExecContext(ctx, query, targetBranchID, maxSeq, sourceBranchID, source.BranchPoint)
		if err != nil {
			return nil, fmt.Errorf("failed to copy messages: %w", err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("failed to count copied messages: %w", err)
		}
		msgCount = int(count)
	}

	// Update source branch status
	now := time.Now()
	_, err = tx.ExecContext(ctx, "UPDATE branches SET status = $1, merged_at = $2, updated_at = $2 WHERE id = $3",
		models.BranchStatusMerged, now, sourceBranchID)
	if err != nil {
		return nil, fmt.Errorf("failed to update source branch: %w", err)
	}

	// Create merge record
	merge := &models.BranchMerge{
		ID:                   uuid.NewString(),
		SourceBranchID:       sourceBranchID,
		TargetBranchID:       targetBranchID,
		Strategy:             strategy,
		SourceSequenceStart:  source.BranchPoint + 1,
		TargetSequenceInsert: maxSeq + 1,
		MessageCount:         msgCount,
		MergedAt:             now,
	}

	mergeQuery := `
		INSERT INTO branch_merges (id, source_branch_id, target_branch_id, strategy, source_sequence_start, source_sequence_end, target_sequence_insert, message_count, metadata, merged_at, merged_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err = tx.ExecContext(ctx, mergeQuery,
		merge.ID, merge.SourceBranchID, merge.TargetBranchID, merge.Strategy,
		merge.SourceSequenceStart, merge.SourceSequenceEnd, merge.TargetSequenceInsert,
		merge.MessageCount, "{}", merge.MergedAt, merge.MergedBy)
	if err != nil {
		return nil, fmt.Errorf("failed to create merge record: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit merge: %w", err)
	}
	return merge, nil
}

// ArchiveBranch marks a branch as archived.
func (s *CockroachBranchStore) ArchiveBranch(ctx context.Context, branchID string) error {
	branch, err := s.GetBranch(ctx, branchID)
	if err != nil {
		return err
	}
	if branch.IsPrimary {
		return ErrCannotDeletePrimary
	}
	branch.Status = models.BranchStatusArchived
	return s.UpdateBranch(ctx, branch)
}

// CompareBranches compares two branches and returns their differences.
func (s *CockroachBranchStore) CompareBranches(ctx context.Context, sourceBranchID, targetBranchID string) (*models.BranchCompare, error) {
	source, err := s.GetBranch(ctx, sourceBranchID)
	if err != nil {
		return nil, err
	}
	target, err := s.GetBranch(ctx, targetBranchID)
	if err != nil {
		return nil, err
	}

	compare := &models.BranchCompare{
		SourceBranch: source,
		TargetBranch: target,
	}

	// Find common ancestor using recursive CTE
	query := `
		WITH RECURSIVE source_path AS (
			SELECT id, parent_branch_id, 0 AS depth FROM branches WHERE id = $1
			UNION ALL
			SELECT b.id, b.parent_branch_id, sp.depth + 1
			FROM branches b INNER JOIN source_path sp ON b.id = sp.parent_branch_id
		),
		target_path AS (
			SELECT id, parent_branch_id, 0 AS depth FROM branches WHERE id = $2
			UNION ALL
			SELECT b.id, b.parent_branch_id, tp.depth + 1
			FROM branches b INNER JOIN target_path tp ON b.id = tp.parent_branch_id
		)
		SELECT sp.id FROM source_path sp INNER JOIN target_path tp ON sp.id = tp.id
		ORDER BY sp.depth LIMIT 1
	`
	var ancestorID string
	err = s.db.QueryRowContext(ctx, query, sourceBranchID, targetBranchID).Scan(&ancestorID)
	if err == nil {
		branch, branchErr := s.GetBranch(ctx, ancestorID)
		if branchErr != nil {
			return nil, branchErr
		}
		compare.CommonAncestor = branch
	}

	// Count messages ahead
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM messages WHERE branch_id = $1", sourceBranchID).Scan(&compare.SourceAhead); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM messages WHERE branch_id = $1", targetBranchID).Scan(&compare.TargetAhead); err != nil {
		return nil, err
	}

	return compare, nil
}

// AppendMessageToBranch adds a message to a specific branch.
func (s *CockroachBranchStore) AppendMessageToBranch(ctx context.Context, sessionID, branchID string, msg *models.Message) error {
	if branchID == "" {
		branch, err := s.GetPrimaryBranch(ctx, sessionID)
		if err != nil {
			return err
		}
		branchID = branch.ID
	}

	// Get next sequence number
	var maxSeq int64
	err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(sequence_num), 0) FROM messages WHERE branch_id = $1", branchID).Scan(&maxSeq)
	if err != nil {
		return fmt.Errorf("failed to get max sequence: %w", err)
	}

	msg.BranchID = branchID
	msg.SequenceNum = maxSeq + 1
	if msg.ID == "" {
		msg.ID = uuid.NewString()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}

	attachments, err := json.Marshal(msg.Attachments)
	if err != nil {
		return fmt.Errorf("failed to marshal attachments: %w", err)
	}
	toolCalls, err := json.Marshal(msg.ToolCalls)
	if err != nil {
		return fmt.Errorf("failed to marshal tool calls: %w", err)
	}
	toolResults, err := json.Marshal(msg.ToolResults)
	if err != nil {
		return fmt.Errorf("failed to marshal tool results: %w", err)
	}
	metadata, err := json.Marshal(msg.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		INSERT INTO messages (id, session_id, branch_id, sequence_num, channel, channel_id, direction, role, content, attachments, tool_calls, tool_results, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	_, err = s.db.ExecContext(ctx, query,
		msg.ID, sessionID, branchID, msg.SequenceNum, msg.Channel, msg.ChannelID, msg.Direction, msg.Role,
		msg.Content, attachments, toolCalls, toolResults, metadata, msg.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to append message: %w", err)
	}

	// Update branch timestamp
	if _, err := s.db.ExecContext(ctx, "UPDATE branches SET updated_at = $1 WHERE id = $2", time.Now(), branchID); err != nil {
		return fmt.Errorf("failed to update branch timestamp: %w", err)
	}
	return nil
}

// GetBranchHistory retrieves messages for a branch including inherited messages.
func (s *CockroachBranchStore) GetBranchHistory(ctx context.Context, branchID string, limit int) ([]*models.Message, error) {
	if limit <= 0 {
		limit = 100
	}

	// Use recursive CTE to get all messages in branch lineage
	query := `
		WITH RECURSIVE branch_path AS (
			SELECT id, parent_branch_id, branch_point, 0 AS depth FROM branches WHERE id = $1
			UNION ALL
			SELECT b.id, b.parent_branch_id, b.branch_point, bp.depth + 1
			FROM branches b INNER JOIN branch_path bp ON b.id = bp.parent_branch_id
		),
		branch_messages AS (
			SELECT m.*, bp.depth, bp.branch_point AS bp_branch_point
			FROM messages m
			INNER JOIN branch_path bp ON m.branch_id = bp.id
			WHERE bp.depth = 0 OR m.sequence_num <= bp.branch_point
		)
		SELECT id, session_id, branch_id, sequence_num, channel, channel_id, direction, role, content, attachments, tool_calls, tool_results, metadata, created_at
		FROM branch_messages
		ORDER BY depth DESC, sequence_num ASC
		LIMIT $2
	`
	rows, err := s.db.QueryContext(ctx, query, branchID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get branch history: %w", err)
	}
	defer rows.Close()

	return s.scanMessages(rows)
}

// GetBranchHistoryFromSequence retrieves messages from a specific sequence.
func (s *CockroachBranchStore) GetBranchHistoryFromSequence(ctx context.Context, branchID string, fromSequence int64, limit int) ([]*models.Message, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `
		SELECT id, session_id, branch_id, sequence_num, channel, channel_id, direction, role, content, attachments, tool_calls, tool_results, metadata, created_at
		FROM messages WHERE branch_id = $1 AND sequence_num >= $2
		ORDER BY sequence_num ASC LIMIT $3
	`
	rows, err := s.db.QueryContext(ctx, query, branchID, fromSequence, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get history from sequence: %w", err)
	}
	defer rows.Close()
	return s.scanMessages(rows)
}

// GetBranchOwnMessages retrieves only messages directly belonging to this branch.
func (s *CockroachBranchStore) GetBranchOwnMessages(ctx context.Context, branchID string, limit int) ([]*models.Message, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `
		SELECT id, session_id, branch_id, sequence_num, channel, channel_id, direction, role, content, attachments, tool_calls, tool_results, metadata, created_at
		FROM messages WHERE branch_id = $1
		ORDER BY sequence_num ASC LIMIT $2
	`
	rows, err := s.db.QueryContext(ctx, query, branchID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get own messages: %w", err)
	}
	defer rows.Close()
	return s.scanMessages(rows)
}

// EnsurePrimaryBranch creates a primary branch if one doesn't exist.
func (s *CockroachBranchStore) EnsurePrimaryBranch(ctx context.Context, sessionID string) (*models.Branch, error) {
	branch, err := s.GetPrimaryBranch(ctx, sessionID)
	if err == nil {
		return branch, nil
	}

	branch = models.NewPrimaryBranch(sessionID)
	if err := s.CreateBranch(ctx, branch); err != nil {
		return nil, err
	}
	return branch, nil
}

// MigrateSessionToBranches migrates existing session messages to the primary branch.
func (s *CockroachBranchStore) MigrateSessionToBranches(ctx context.Context, sessionID string) error {
	branch, err := s.EnsurePrimaryBranch(ctx, sessionID)
	if err != nil {
		return err
	}

	// Update messages without branch_id to use primary branch
	query := `
		UPDATE messages SET branch_id = $1, sequence_num = (
			SELECT COUNT(*) FROM messages m2 WHERE m2.session_id = messages.session_id
			AND m2.created_at <= messages.created_at AND (m2.branch_id IS NULL OR m2.branch_id = '')
		)
		WHERE session_id = $2 AND (branch_id IS NULL OR branch_id = '')
	`
	_, err = s.db.ExecContext(ctx, query, branch.ID, sessionID)
	return err
}

func (s *CockroachBranchStore) scanMessages(rows *sql.Rows) ([]*models.Message, error) {
	var messages []*models.Message
	for rows.Next() {
		msg := &models.Message{}
		var attachments, toolCalls, toolResults, metadata []byte

		err := rows.Scan(&msg.ID, &msg.SessionID, &msg.BranchID, &msg.SequenceNum,
			&msg.Channel, &msg.ChannelID, &msg.Direction, &msg.Role, &msg.Content,
			&attachments, &toolCalls, &toolResults, &metadata, &msg.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}

		if len(attachments) > 0 && string(attachments) != "null" {
			if err := json.Unmarshal(attachments, &msg.Attachments); err != nil {
				return nil, fmt.Errorf("failed to unmarshal attachments: %w", err)
			}
		}
		if len(toolCalls) > 0 && string(toolCalls) != "null" {
			if err := json.Unmarshal(toolCalls, &msg.ToolCalls); err != nil {
				return nil, fmt.Errorf("failed to unmarshal tool calls: %w", err)
			}
		}
		if len(toolResults) > 0 && string(toolResults) != "null" {
			if err := json.Unmarshal(toolResults, &msg.ToolResults); err != nil {
				return nil, fmt.Errorf("failed to unmarshal tool results: %w", err)
			}
		}
		if len(metadata) > 0 && string(metadata) != "null" {
			if err := json.Unmarshal(metadata, &msg.Metadata); err != nil {
				return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
			}
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}
