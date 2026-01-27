package models

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCatalog_Get(t *testing.T) {
	c := NewCatalog()

	// Get by ID
	model, ok := c.Get("claude-opus-4")
	if !ok {
		t.Fatal("expected to find claude-opus-4")
	}
	if model.Name != "Claude Opus 4" {
		t.Errorf("Name = %s, want Claude Opus 4", model.Name)
	}

	// Get by alias
	model, ok = c.Get("sonnet")
	if !ok {
		t.Fatal("expected to find sonnet alias")
	}
	if model.ID != "claude-3-5-sonnet-latest" {
		t.Errorf("ID = %s, want claude-3-5-sonnet-latest", model.ID)
	}

	// Get unknown
	_, ok = c.Get("unknown-model")
	if ok {
		t.Error("should not find unknown-model")
	}
}

func TestModel_Capabilities(t *testing.T) {
	model := &Model{
		ID:           "test",
		Capabilities: []Capability{CapVision, CapTools, CapStreaming},
	}

	if !model.HasCapability(CapVision) {
		t.Error("should have vision capability")
	}
	if !model.SupportsVision() {
		t.Error("should support vision")
	}
	if !model.SupportsTools() {
		t.Error("should support tools")
	}
	if !model.SupportsStreaming() {
		t.Error("should support streaming")
	}
	if model.HasCapability(CapReasoning) {
		t.Error("should not have reasoning capability")
	}
}

func TestCatalog_List(t *testing.T) {
	c := NewCatalog()

	// List all
	all := c.List(nil)
	if len(all) == 0 {
		t.Error("expected some models")
	}

	// List by provider
	anthropic := c.ListByProvider(ProviderAnthropic)
	for _, m := range anthropic {
		if m.Provider != ProviderAnthropic {
			t.Errorf("expected anthropic provider, got %s", m.Provider)
		}
	}

	// List by capability
	vision := c.ListByCapability(CapVision)
	for _, m := range vision {
		if !m.HasCapability(CapVision) {
			t.Errorf("model %s should have vision capability", m.ID)
		}
	}
}

func TestFilter_Matches(t *testing.T) {
	model := &Model{
		ID:            "test",
		Provider:      ProviderAnthropic,
		Tier:          TierStandard,
		ContextWindow: 200000,
		Capabilities:  []Capability{CapVision, CapTools},
		Deprecated:    false,
	}

	tests := []struct {
		name   string
		filter *Filter
		want   bool
	}{
		{
			name:   "nil filter matches all",
			filter: nil,
			want:   true,
		},
		{
			name:   "empty filter matches all",
			filter: &Filter{},
			want:   true,
		},
		{
			name: "provider match",
			filter: &Filter{
				Providers: []Provider{ProviderAnthropic},
			},
			want: true,
		},
		{
			name: "provider no match",
			filter: &Filter{
				Providers: []Provider{ProviderOpenAI},
			},
			want: false,
		},
		{
			name: "tier match",
			filter: &Filter{
				Tiers: []Tier{TierStandard, TierFast},
			},
			want: true,
		},
		{
			name: "tier no match",
			filter: &Filter{
				Tiers: []Tier{TierFlagship},
			},
			want: false,
		},
		{
			name: "capability match",
			filter: &Filter{
				RequiredCapabilities: []Capability{CapVision, CapTools},
			},
			want: true,
		},
		{
			name: "capability no match",
			filter: &Filter{
				RequiredCapabilities: []Capability{CapVision, CapReasoning},
			},
			want: false,
		},
		{
			name: "context window match",
			filter: &Filter{
				MinContextWindow: 100000,
			},
			want: true,
		},
		{
			name: "context window no match",
			filter: &Filter{
				MinContextWindow: 500000,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.filter.Matches(model)
			if got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilter_Deprecated(t *testing.T) {
	deprecated := &Model{
		ID:         "old-model",
		Deprecated: true,
	}

	// Default excludes retired models
	filter := &Filter{}
	if filter.Matches(deprecated) {
		t.Error("should not match deprecated by default")
	}

	// Explicitly include retired models
	filter = &Filter{IncludeDeprecated: true}
	if !filter.Matches(deprecated) {
		t.Error("should match when IncludeDeprecated is true")
	}
}

func TestDefaultCatalog(t *testing.T) {
	// Test global functions
	model, ok := Get("gpt-4o")
	if !ok {
		t.Fatal("expected to find gpt-4o in default catalog")
	}
	if model.Provider != ProviderOpenAI {
		t.Errorf("provider = %s, want openai", model.Provider)
	}

	// List all
	all := List(nil)
	if len(all) < 5 {
		t.Errorf("expected at least 5 models, got %d", len(all))
	}
}

// ==============================================================================
// Dynamic ModelCatalog Tests (clawdbot-style)
// ==============================================================================

// mockDiscoverer is a test discoverer that returns predefined models.
type mockDiscoverer struct {
	models []ModelCatalogEntry
	err    error
	calls  int32
	delay  time.Duration
}

func (m *mockDiscoverer) DiscoverModels() ([]ModelCatalogEntry, error) {
	atomic.AddInt32(&m.calls, 1)
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	if m.err != nil {
		return nil, m.err
	}
	return m.models, nil
}

func (m *mockDiscoverer) CallCount() int {
	return int(atomic.LoadInt32(&m.calls))
}

func TestModelCatalog_NewModelCatalog(t *testing.T) {
	mc := NewModelCatalog()
	if mc == nil {
		t.Fatal("expected non-nil catalog")
	}
	if mc.IsCached() {
		t.Error("new catalog should not have cached entries")
	}
}

func TestModelCatalog_LoadCatalog_DefaultPresets(t *testing.T) {
	// Without a discoverer, should return common presets
	mc := NewModelCatalog()
	entries, err := mc.LoadCatalog(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected some preset models")
	}

	// Verify we got some expected models
	found := false
	for _, e := range entries {
		if e.Id == "gpt-4" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find gpt-4 in presets")
	}
}

func TestModelCatalog_LoadCatalog_WithDiscoverer(t *testing.T) {
	models := []ModelCatalogEntry{
		{Id: "test-model-1", Name: "Test Model 1", Provider: "test"},
		{Id: "test-model-2", Name: "Test Model 2", Provider: "test"},
	}
	discoverer := &mockDiscoverer{models: models}

	mc := NewModelCatalogWithDiscoverer(discoverer)
	entries, err := mc.LoadCatalog(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 models, got %d", len(entries))
	}
	if discoverer.CallCount() != 1 {
		t.Errorf("expected 1 call, got %d", discoverer.CallCount())
	}
}

func TestModelCatalog_LoadCatalog_Caching(t *testing.T) {
	models := []ModelCatalogEntry{
		{Id: "cached-model", Name: "Cached Model", Provider: "test"},
	}
	discoverer := &mockDiscoverer{models: models}

	mc := NewModelCatalogWithDiscoverer(discoverer)

	// First load
	entries1, err := mc.LoadCatalog(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Second load should return cached
	entries2, err := mc.LoadCatalog(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only have called discoverer once
	if discoverer.CallCount() != 1 {
		t.Errorf("expected 1 call (cached), got %d", discoverer.CallCount())
	}

	// Both results should be equal
	if len(entries1) != len(entries2) {
		t.Error("cached entries should match")
	}
}

func TestModelCatalog_LoadCatalog_BypassCache(t *testing.T) {
	models := []ModelCatalogEntry{
		{Id: "model-v1", Name: "Model V1", Provider: "test"},
	}
	discoverer := &mockDiscoverer{models: models}

	mc := NewModelCatalogWithDiscoverer(discoverer)

	// First load
	_, err := mc.LoadCatalog(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Change the models
	discoverer.models = []ModelCatalogEntry{
		{Id: "model-v2", Name: "Model V2", Provider: "test"},
	}

	// Load with cache bypass
	entries, err := mc.LoadCatalog(false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have called discoverer twice
	if discoverer.CallCount() != 2 {
		t.Errorf("expected 2 calls, got %d", discoverer.CallCount())
	}

	// Should have new model
	if len(entries) != 1 || entries[0].Id != "model-v2" {
		t.Error("expected new model after cache bypass")
	}
}

func TestModelCatalog_LoadCatalog_Sorting(t *testing.T) {
	models := []ModelCatalogEntry{
		{Id: "z-model", Name: "Z Model", Provider: "z-provider"},
		{Id: "a-model", Name: "A Model", Provider: "a-provider"},
		{Id: "m-model", Name: "M Model", Provider: "a-provider"},
	}
	discoverer := &mockDiscoverer{models: models}

	mc := NewModelCatalogWithDiscoverer(discoverer)
	entries, err := mc.LoadCatalog(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be sorted by provider, then name
	if entries[0].Provider != "a-provider" {
		t.Errorf("expected first provider to be a-provider, got %s", entries[0].Provider)
	}
	if entries[0].Name != "A Model" {
		t.Errorf("expected first model to be A Model, got %s", entries[0].Name)
	}
	if entries[1].Name != "M Model" {
		t.Errorf("expected second model to be M Model, got %s", entries[1].Name)
	}
	if entries[2].Provider != "z-provider" {
		t.Errorf("expected third provider to be z-provider, got %s", entries[2].Provider)
	}
}

func TestModelCatalog_LoadCatalog_ErrorHandling_TransientError(t *testing.T) {
	discoverer := &mockDiscoverer{err: errors.New("transient error")}

	mc := NewModelCatalogWithDiscoverer(discoverer)

	// First call should fail
	_, err := mc.LoadCatalog(true)
	if err == nil {
		t.Error("expected error")
	}

	// Cache should NOT be poisoned
	if mc.IsCached() {
		t.Error("cache should not be poisoned on error")
	}

	// Fix the error and try again
	discoverer.err = nil
	discoverer.models = []ModelCatalogEntry{
		{Id: "recovered-model", Name: "Recovered", Provider: "test"},
	}

	entries, err := mc.LoadCatalog(true)
	if err != nil {
		t.Fatalf("unexpected error on retry: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 model after recovery, got %d", len(entries))
	}
}

func TestModelCatalog_LoadCatalog_EmptyResult(t *testing.T) {
	discoverer := &mockDiscoverer{models: []ModelCatalogEntry{}}

	mc := NewModelCatalogWithDiscoverer(discoverer)

	// Empty results should not be cached
	entries, err := mc.LoadCatalog(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 models, got %d", len(entries))
	}

	// Cache should still be empty (not caching empty results)
	if mc.IsCached() {
		t.Error("should not cache empty results")
	}

	// Next call should try again
	discoverer.models = []ModelCatalogEntry{
		{Id: "new-model", Name: "New", Provider: "test"},
	}

	entries, err = mc.LoadCatalog(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 model after retry, got %d", len(entries))
	}
}

func TestModelCatalog_LoadCatalog_ConcurrentAccess(t *testing.T) {
	models := []ModelCatalogEntry{
		{Id: "concurrent-model", Name: "Concurrent", Provider: "test"},
	}
	discoverer := &mockDiscoverer{
		models: models,
		delay:  50 * time.Millisecond, // Simulate slow discovery
	}

	mc := NewModelCatalogWithDiscoverer(discoverer)

	// Start multiple goroutines that all call LoadCatalog
	var wg sync.WaitGroup
	results := make(chan []ModelCatalogEntry, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entries, err := mc.LoadCatalog(true)
			if err != nil {
				t.Errorf("unexpected error in goroutine: %v", err)
				return
			}
			results <- entries
		}()
	}

	wg.Wait()
	close(results)

	// All goroutines should get the same result
	count := 0
	for entries := range results {
		count++
		if len(entries) != 1 {
			t.Errorf("goroutine got %d entries, want 1", len(entries))
		}
	}

	if count != 10 {
		t.Errorf("expected 10 results, got %d", count)
	}

	// Should only have called discoverer once (promise-based deduplication)
	if discoverer.CallCount() != 1 {
		t.Errorf("expected 1 call (deduplicated), got %d", discoverer.CallCount())
	}
}

func TestModelCatalog_GetModel(t *testing.T) {
	models := []ModelCatalogEntry{
		{Id: "model-1", Name: "Model One", Provider: "test"},
		{Id: "model-2", Name: "Model Two", Provider: "test"},
	}
	discoverer := &mockDiscoverer{models: models}

	mc := NewModelCatalogWithDiscoverer(discoverer)

	// Before loading, should return nil
	if mc.GetModel("model-1") != nil {
		t.Error("should return nil before loading")
	}

	// Load catalog
	_, err := mc.LoadCatalog(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Now should find model
	entry := mc.GetModel("model-1")
	if entry == nil {
		t.Fatal("expected to find model-1")
	}
	if entry.Name != "Model One" {
		t.Errorf("Name = %s, want Model One", entry.Name)
	}

	// Should not find unknown model
	if mc.GetModel("unknown") != nil {
		t.Error("should not find unknown model")
	}

	// Test with whitespace
	entry = mc.GetModel("  model-2  ")
	if entry == nil {
		t.Fatal("expected to find model-2 with whitespace")
	}
}

func TestModelCatalog_GetModelsByProvider(t *testing.T) {
	models := []ModelCatalogEntry{
		{Id: "anthropic-1", Name: "Anthropic 1", Provider: "anthropic"},
		{Id: "anthropic-2", Name: "Anthropic 2", Provider: "anthropic"},
		{Id: "openai-1", Name: "OpenAI 1", Provider: "openai"},
	}
	discoverer := &mockDiscoverer{models: models}

	mc := NewModelCatalogWithDiscoverer(discoverer)

	// Before loading
	if mc.GetModelsByProvider("anthropic") != nil {
		t.Error("should return nil before loading")
	}

	// Load catalog
	_, err := mc.LoadCatalog(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Get anthropic models
	anthropicModels := mc.GetModelsByProvider("anthropic")
	if len(anthropicModels) != 2 {
		t.Errorf("expected 2 anthropic models, got %d", len(anthropicModels))
	}

	// Get openai models
	openaiModels := mc.GetModelsByProvider("openai")
	if len(openaiModels) != 1 {
		t.Errorf("expected 1 openai model, got %d", len(openaiModels))
	}

	// Case insensitive
	upperModels := mc.GetModelsByProvider("ANTHROPIC")
	if len(upperModels) != 2 {
		t.Errorf("expected case-insensitive match, got %d", len(upperModels))
	}

	// Unknown provider
	unknownModels := mc.GetModelsByProvider("unknown")
	if len(unknownModels) != 0 {
		t.Errorf("expected 0 models for unknown provider, got %d", len(unknownModels))
	}
}

func TestModelCatalog_ListAllModels(t *testing.T) {
	models := []ModelCatalogEntry{
		{Id: "model-a", Name: "Model A", Provider: "provider-b"},
		{Id: "model-b", Name: "Model B", Provider: "provider-a"},
	}
	discoverer := &mockDiscoverer{models: models}

	mc := NewModelCatalogWithDiscoverer(discoverer)

	// Before loading
	if mc.ListAllModels() != nil {
		t.Error("should return nil before loading")
	}

	// Load catalog
	_, err := mc.LoadCatalog(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// List all - should be sorted
	all := mc.ListAllModels()
	if len(all) != 2 {
		t.Errorf("expected 2 models, got %d", len(all))
	}

	// Should be sorted by provider
	if all[0].Provider != "provider-a" {
		t.Errorf("expected first model from provider-a, got %s", all[0].Provider)
	}
}

func TestModelCatalog_ResetCache(t *testing.T) {
	models := []ModelCatalogEntry{
		{Id: "cached-model", Name: "Cached", Provider: "test"},
	}
	discoverer := &mockDiscoverer{models: models}

	mc := NewModelCatalogWithDiscoverer(discoverer)

	// Load catalog
	_, err := mc.LoadCatalog(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !mc.IsCached() {
		t.Error("should be cached after load")
	}

	// Reset cache
	mc.ResetCache()

	if mc.IsCached() {
		t.Error("should not be cached after reset")
	}

	// GetModel should return nil
	if mc.GetModel("cached-model") != nil {
		t.Error("GetModel should return nil after reset")
	}

	// Next load should call discoverer again
	_, err = mc.LoadCatalog(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if discoverer.CallCount() != 2 {
		t.Errorf("expected 2 calls after reset, got %d", discoverer.CallCount())
	}
}

func TestModelCatalog_ValidationSkipsInvalidEntries(t *testing.T) {
	models := []ModelCatalogEntry{
		{Id: "", Name: "No ID", Provider: "test"},           // Invalid: no ID
		{Id: "valid-1", Name: "Valid 1", Provider: ""},      // Invalid: no provider
		{Id: "valid-2", Name: "", Provider: "test"},         // Valid: name defaults to ID
		{Id: "  ", Name: "Whitespace ID", Provider: "test"}, // Invalid: whitespace ID
		{Id: "valid-3", Name: "Valid 3", Provider: "test"},  // Valid
	}
	discoverer := &mockDiscoverer{models: models}

	mc := NewModelCatalogWithDiscoverer(discoverer)
	entries, err := mc.LoadCatalog(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only have valid entries
	if len(entries) != 2 {
		t.Errorf("expected 2 valid entries, got %d", len(entries))
	}

	// valid-2 should have name = id
	for _, e := range entries {
		if e.Id == "valid-2" && e.Name != "valid-2" {
			t.Errorf("expected name to default to ID, got %s", e.Name)
		}
	}
}

func TestModelCatalog_SetLogger(t *testing.T) {
	discoverer := &mockDiscoverer{err: errors.New("test error")}

	var logMessages []string
	logger := func(format string, args ...interface{}) {
		logMessages = append(logMessages, format)
	}

	mc := NewModelCatalogWithDiscoverer(discoverer)
	mc.SetLogger(logger)

	// First error should log
	_, _ = mc.LoadCatalog(true)
	if len(logMessages) != 1 {
		t.Errorf("expected 1 log message, got %d", len(logMessages))
	}

	// Second error should not log (hasLoggedError)
	_, _ = mc.LoadCatalog(false)
	if len(logMessages) != 1 {
		t.Errorf("expected still 1 log message (no duplicate), got %d", len(logMessages))
	}

	// Reset should clear hasLoggedError
	mc.ResetCache()
	_, _ = mc.LoadCatalog(true)
	if len(logMessages) != 2 {
		t.Errorf("expected 2 log messages after reset, got %d", len(logMessages))
	}
}

func TestModelCatalog_SetDiscoverer(t *testing.T) {
	mc := NewModelCatalog()

	// Load with default (presets)
	entries1, err := mc.LoadCatalog(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Clear and set custom discoverer
	mc.ResetCache()
	customModels := []ModelCatalogEntry{
		{Id: "custom-model", Name: "Custom", Provider: "custom"},
	}
	mc.SetDiscoverer(&mockDiscoverer{models: customModels})

	entries2, err := mc.LoadCatalog(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries1) == len(entries2) {
		t.Error("expected different results after setting discoverer")
	}
	if len(entries2) != 1 || entries2[0].Id != "custom-model" {
		t.Error("expected custom model")
	}
}

func TestGetCommonModelPresets(t *testing.T) {
	presets := GetCommonModelPresets()

	if len(presets) == 0 {
		t.Error("expected some presets")
	}

	// Check for expected models
	expectedModels := map[string]bool{
		"claude-3-opus-20240229": false,
		"gpt-4":                  false,
		"gemini-1.5-pro":         false,
		"o1":                     false,
	}

	for _, p := range presets {
		if _, ok := expectedModels[p.Id]; ok {
			expectedModels[p.Id] = true
		}
	}

	for id, found := range expectedModels {
		if !found {
			t.Errorf("expected to find %s in presets", id)
		}
	}

	// Check reasoning flag is set correctly
	for _, p := range presets {
		if p.Id == "o1" && !p.Reasoning {
			t.Error("o1 should have Reasoning=true")
		}
		if p.Id == "o3-mini" && !p.Reasoning {
			t.Error("o3-mini should have Reasoning=true")
		}
		if p.Id == "gpt-4" && p.Reasoning {
			t.Error("gpt-4 should have Reasoning=false")
		}
	}
}

func TestModelCatalog_ConcurrentLoadAndReset(t *testing.T) {
	models := []ModelCatalogEntry{
		{Id: "test-model", Name: "Test", Provider: "test"},
	}
	discoverer := &mockDiscoverer{
		models: models,
		delay:  10 * time.Millisecond,
	}

	mc := NewModelCatalogWithDiscoverer(discoverer)

	var wg sync.WaitGroup

	// Start multiple goroutines doing loads
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_, _ = mc.LoadCatalog(true)
				time.Sleep(time.Millisecond)
			}
		}()
	}

	// Start goroutines doing resets
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				mc.ResetCache()
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	// Start goroutines doing reads
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = mc.GetModel("test-model")
				_ = mc.ListAllModels()
				_ = mc.GetModelsByProvider("test")
				time.Sleep(time.Millisecond)
			}
		}()
	}

	// This should complete without deadlocks or panics
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(10 * time.Second):
		t.Fatal("test timed out, possible deadlock")
	}
}

func TestModelCatalog_ReturnsCopyNotReference(t *testing.T) {
	models := []ModelCatalogEntry{
		{Id: "original", Name: "Original Name", Provider: "test"},
	}
	discoverer := &mockDiscoverer{models: models}

	mc := NewModelCatalogWithDiscoverer(discoverer)
	entries, err := mc.LoadCatalog(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Modify returned entries
	entries[0].Name = "Modified Name"

	// Cached entries should be unchanged
	cached := mc.ListAllModels()
	if cached[0].Name != "Original Name" {
		t.Error("modifying returned entries should not affect cache")
	}

	// GetModel should also return a copy
	entry := mc.GetModel("original")
	entry.Name = "Another Modification"

	cached2 := mc.ListAllModels()
	if cached2[0].Name != "Original Name" {
		t.Error("modifying GetModel result should not affect cache")
	}
}
