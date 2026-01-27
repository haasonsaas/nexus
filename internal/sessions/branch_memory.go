package sessions

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/pkg/models"
)

// MemoryBranchStore implements BranchStore using in-memory storage for testing.
type MemoryBranchStore struct {
	mu       sync.RWMutex
	branches map[string]*models.Branch
	messages map[string][]*models.Message // branchID -> messages
	merges   map[string]*models.BranchMerge
}

// NewMemoryBranchStore creates a new in-memory branch store.
func NewMemoryBranchStore() *MemoryBranchStore {
	return &MemoryBranchStore{
		branches: make(map[string]*models.Branch),
		messages: make(map[string][]*models.Message),
		merges:   make(map[string]*models.BranchMerge),
	}
}

func (s *MemoryBranchStore) CreateBranch(ctx context.Context, branch *models.Branch) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if branch.ID == "" {
		branch.ID = uuid.NewString()
	}
	if branch.CreatedAt.IsZero() {
		branch.CreatedAt = time.Now()
	}
	branch.UpdatedAt = branch.CreatedAt

	s.branches[branch.ID] = cloneBranch(branch)
	return nil
}

func (s *MemoryBranchStore) GetBranch(ctx context.Context, branchID string) (*models.Branch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	branch, ok := s.branches[branchID]
	if !ok {
		return nil, ErrBranchNotFound
	}
	return cloneBranch(branch), nil
}

func (s *MemoryBranchStore) UpdateBranch(ctx context.Context, branch *models.Branch) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.branches[branch.ID]; !ok {
		return ErrBranchNotFound
	}
	branch.UpdatedAt = time.Now()
	s.branches[branch.ID] = cloneBranch(branch)
	return nil
}

func (s *MemoryBranchStore) DeleteBranch(ctx context.Context, branchID string, deleteMessages bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	branch, ok := s.branches[branchID]
	if !ok {
		return ErrBranchNotFound
	}
	if branch.IsPrimary {
		return ErrCannotDeletePrimary
	}

	delete(s.branches, branchID)
	if deleteMessages {
		delete(s.messages, branchID)
	}
	return nil
}

func (s *MemoryBranchStore) GetPrimaryBranch(ctx context.Context, sessionID string) (*models.Branch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, branch := range s.branches {
		if branch.SessionID == sessionID && branch.IsPrimary {
			return cloneBranch(branch), nil
		}
	}
	return nil, ErrBranchNotFound
}

func (s *MemoryBranchStore) ListBranches(ctx context.Context, sessionID string, opts BranchListOptions) ([]*models.Branch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*models.Branch
	for _, branch := range s.branches {
		if branch.SessionID != sessionID {
			continue
		}
		if opts.Status != nil && branch.Status != *opts.Status {
			continue
		}
		if !opts.IncludeArchived && branch.Status == models.BranchStatusArchived {
			continue
		}
		result = append(result, cloneBranch(branch))
	}

	// Apply pagination
	start := opts.Offset
	if start > len(result) {
		return []*models.Branch{}, nil
	}
	end := len(result)
	if opts.Limit > 0 && start+opts.Limit < end {
		end = start + opts.Limit
	}
	return result[start:end], nil
}

func (s *MemoryBranchStore) GetBranchTree(ctx context.Context, sessionID string) (*models.BranchTree, error) {
	branches, err := s.ListBranches(ctx, sessionID, BranchListOptions{IncludeArchived: true, Limit: 1000})
	if err != nil {
		return nil, err
	}
	if len(branches) == 0 {
		return nil, ErrBranchNotFound
	}

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

func (s *MemoryBranchStore) GetFullBranchPath(ctx context.Context, branchID string) (*models.BranchPath, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.getFullBranchPathLocked(branchID)
	if path == nil {
		return nil, ErrBranchNotFound
	}
	return path, nil
}

func (s *MemoryBranchStore) GetBranchStats(ctx context.Context, branchID string) (*models.BranchStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.branches[branchID]; !ok {
		return nil, ErrBranchNotFound
	}

	stats := &models.BranchStats{BranchID: branchID}
	if msgs, ok := s.messages[branchID]; ok {
		stats.OwnMessages = len(msgs)
		if len(msgs) > 0 {
			lastTime := msgs[len(msgs)-1].CreatedAt
			stats.LastMessageAt = &lastTime
		}
	}

	for _, branch := range s.branches {
		if branch.ParentBranchID != nil && *branch.ParentBranchID == branchID {
			stats.ChildBranchCount++
		}
	}

	// Calculate total messages including inherited
	stats.TotalMessages = s.countTotalMessages(branchID)
	return stats, nil
}

func (s *MemoryBranchStore) countTotalMessages(branchID string) int {
	branch, ok := s.branches[branchID]
	if !ok {
		return 0
	}

	count := len(s.messages[branchID])
	if branch.ParentBranchID != nil {
		parentMsgs := s.messages[*branch.ParentBranchID]
		for _, msg := range parentMsgs {
			if msg.SequenceNum <= branch.BranchPoint {
				count++
			}
		}
	}
	return count
}

func (s *MemoryBranchStore) ForkBranch(ctx context.Context, parentBranchID string, branchPoint int64, name string) (*models.Branch, error) {
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

func (s *MemoryBranchStore) MergeBranch(ctx context.Context, sourceBranchID, targetBranchID string, strategy models.MergeStrategy) (*models.BranchMerge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	source, ok := s.branches[sourceBranchID]
	if !ok {
		return nil, ErrBranchNotFound
	}
	if source.IsPrimary {
		return nil, ErrCannotMergePrimary
	}
	if source.Status != models.BranchStatusActive {
		return nil, ErrBranchMerged
	}

	if _, ok := s.branches[targetBranchID]; !ok {
		return nil, ErrBranchNotFound
	}

	// Get max sequence in target
	var maxSeq int64
	for _, msg := range s.messages[targetBranchID] {
		if msg.SequenceNum > maxSeq {
			maxSeq = msg.SequenceNum
		}
	}

	// Copy messages from source to target
	var msgCount int
	for _, msg := range s.messages[sourceBranchID] {
		if msg.SequenceNum > source.BranchPoint {
			newMsg := cloneMessage(msg)
			newMsg.ID = uuid.NewString()
			newMsg.BranchID = targetBranchID
			newMsg.SequenceNum = maxSeq + int64(msgCount) + 1
			s.messages[targetBranchID] = append(s.messages[targetBranchID], newMsg)
			msgCount++
		}
	}

	// Update source branch status
	now := time.Now()
	source.Status = models.BranchStatusMerged
	source.MergedAt = &now
	source.UpdatedAt = now

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
	s.merges[merge.ID] = merge
	return merge, nil
}

func (s *MemoryBranchStore) ArchiveBranch(ctx context.Context, branchID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	branch, ok := s.branches[branchID]
	if !ok {
		return ErrBranchNotFound
	}
	if branch.IsPrimary {
		return ErrCannotDeletePrimary
	}
	branch.Status = models.BranchStatusArchived
	branch.UpdatedAt = time.Now()
	return nil
}

func (s *MemoryBranchStore) CompareBranches(ctx context.Context, sourceBranchID, targetBranchID string) (*models.BranchCompare, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Use internal methods that don't acquire locks (we already hold the lock)
	source, ok := s.branches[sourceBranchID]
	if !ok {
		return nil, ErrBranchNotFound
	}
	target, ok := s.branches[targetBranchID]
	if !ok {
		return nil, ErrBranchNotFound
	}

	compare := &models.BranchCompare{
		SourceBranch: cloneBranch(source),
		TargetBranch: cloneBranch(target),
		SourceAhead:  len(s.messages[sourceBranchID]),
		TargetAhead:  len(s.messages[targetBranchID]),
	}

	// Find common ancestor using internal path computation (no lock acquisition)
	sourcePath := s.getFullBranchPathLocked(sourceBranchID)
	targetPath := s.getFullBranchPathLocked(targetBranchID)

	if sourcePath != nil && targetPath != nil {
		sourceSet := make(map[string]bool)
		for _, id := range sourcePath.Path {
			sourceSet[id] = true
		}
		for _, id := range targetPath.Path {
			if sourceSet[id] {
				if ancestor, ok := s.branches[id]; ok {
					compare.CommonAncestor = cloneBranch(ancestor)
				}
				break
			}
		}
	}

	return compare, nil
}

// getFullBranchPathLocked computes the branch path without acquiring locks.
// Caller must hold at least an RLock.
func (s *MemoryBranchStore) getFullBranchPathLocked(branchID string) *models.BranchPath {
	var path []*models.Branch
	currentID := branchID
	visited := make(map[string]bool) // Cycle detection to prevent infinite loops

	for currentID != "" {
		// Check for cycle before processing
		if visited[currentID] {
			// Cycle detected - break to prevent infinite loop
			break
		}
		visited[currentID] = true

		branch, ok := s.branches[currentID]
		if !ok {
			if len(path) == 0 {
				return nil
			}
			break
		}
		path = append([]*models.Branch{cloneBranch(branch)}, path...)
		if branch.ParentBranchID == nil {
			break
		}
		currentID = *branch.ParentBranchID
	}

	result := &models.BranchPath{
		BranchID: branchID,
		Path:     make([]string, len(path)),
		Branches: path,
	}
	for i, b := range path {
		result.Path[i] = b.ID
	}
	return result
}

func (s *MemoryBranchStore) AppendMessageToBranch(ctx context.Context, sessionID, branchID string, msg *models.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if branchID == "" {
		for _, branch := range s.branches {
			if branch.SessionID == sessionID && branch.IsPrimary {
				branchID = branch.ID
				break
			}
		}
		if branchID == "" {
			return ErrBranchNotFound
		}
	}

	if _, ok := s.branches[branchID]; !ok {
		return ErrBranchNotFound
	}

	// Get next sequence number
	var maxSeq int64
	for _, m := range s.messages[branchID] {
		if m.SequenceNum > maxSeq {
			maxSeq = m.SequenceNum
		}
	}

	clone := cloneMessage(msg)
	if clone.ID == "" {
		clone.ID = uuid.NewString()
	}
	if clone.CreatedAt.IsZero() {
		clone.CreatedAt = time.Now()
	}
	clone.BranchID = branchID
	clone.SequenceNum = maxSeq + 1

	s.messages[branchID] = append(s.messages[branchID], clone)

	// Update branch timestamp
	if branch, ok := s.branches[branchID]; ok {
		branch.UpdatedAt = time.Now()
	}

	return nil
}

func (s *MemoryBranchStore) GetBranchHistory(ctx context.Context, branchID string, limit int) ([]*models.Message, error) {
	if limit <= 0 {
		limit = 100
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	branch, ok := s.branches[branchID]
	if !ok {
		return nil, ErrBranchNotFound
	}

	var result []*models.Message

	// Get inherited messages from ancestors with cycle detection
	visited := make(map[string]bool)
	currentBranch := branch
	for currentBranch.ParentBranchID != nil {
		parentID := *currentBranch.ParentBranchID
		if visited[parentID] {
			// Circular reference detected, stop traversal
			break
		}
		visited[currentBranch.ID] = true

		parentMsgs := s.messages[parentID]
		for _, msg := range parentMsgs {
			if msg.SequenceNum <= currentBranch.BranchPoint {
				result = append(result, cloneMessage(msg))
			}
		}
		var ok bool
		currentBranch, ok = s.branches[parentID]
		if !ok {
			break
		}
	}

	// Add own messages
	for _, msg := range s.messages[branchID] {
		result = append(result, cloneMessage(msg))
	}

	// Apply limit
	if len(result) > limit {
		result = result[len(result)-limit:]
	}

	return result, nil
}

func (s *MemoryBranchStore) GetBranchHistoryFromSequence(ctx context.Context, branchID string, fromSequence int64, limit int) ([]*models.Message, error) {
	if limit <= 0 {
		limit = 100
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	msgs := s.messages[branchID]
	var result []*models.Message
	for _, msg := range msgs {
		if msg.SequenceNum >= fromSequence {
			result = append(result, cloneMessage(msg))
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (s *MemoryBranchStore) GetBranchOwnMessages(ctx context.Context, branchID string, limit int) ([]*models.Message, error) {
	if limit <= 0 {
		limit = 100
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	msgs := s.messages[branchID]
	var result []*models.Message
	for i, msg := range msgs {
		if i >= limit {
			break
		}
		result = append(result, cloneMessage(msg))
	}
	return result, nil
}

func (s *MemoryBranchStore) EnsurePrimaryBranch(ctx context.Context, sessionID string) (*models.Branch, error) {
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

func (s *MemoryBranchStore) MigrateSessionToBranches(ctx context.Context, sessionID string) error {
	// In memory store, this is a no-op since we don't have previous data
	_, err := s.EnsurePrimaryBranch(ctx, sessionID)
	return err
}

func cloneBranch(b *models.Branch) *models.Branch {
	if b == nil {
		return nil
	}
	clone := *b
	if b.ParentBranchID != nil {
		parentID := *b.ParentBranchID
		clone.ParentBranchID = &parentID
	}
	if b.MergedAt != nil {
		mergedAt := *b.MergedAt
		clone.MergedAt = &mergedAt
	}
	if b.Metadata != nil {
		clone.Metadata = make(map[string]any)
		for k, v := range b.Metadata {
			clone.Metadata[k] = v
		}
	}
	return &clone
}
