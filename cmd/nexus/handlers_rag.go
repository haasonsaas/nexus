package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/agent/providers"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/memory/embeddings"
	"github.com/haasonsaas/nexus/internal/memory/embeddings/ollama"
	"github.com/haasonsaas/nexus/internal/memory/embeddings/openai"
	"github.com/haasonsaas/nexus/internal/rag/eval"
	"github.com/haasonsaas/nexus/internal/rag/index"
	"github.com/haasonsaas/nexus/internal/rag/packs"
	"github.com/haasonsaas/nexus/internal/rag/store/pgvector"
	"github.com/spf13/cobra"
)

// =============================================================================
// RAG Command Handlers
// =============================================================================

func runRagEval(cmd *cobra.Command, configPath, testSetPath, output string, limit int, threshold float32, judge bool, judgeModel, judgeProvider string, judgeMaxTokens int) error {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	manager, closer, err := buildRAGIndexManager(cfg)
	if err != nil {
		return err
	}
	if closer != nil {
		defer closer.Close()
	}

	set, err := eval.LoadTestSet(testSetPath)
	if err != nil {
		return err
	}

	if judgeModel != "" || judgeProvider != "" {
		judge = true
	}

	evaluator := eval.NewEvaluator(manager, &eval.Options{
		Limit:     limit,
		Threshold: threshold,
		Judge:     judge,
		Model:     judgeModel,
		MaxTokens: judgeMaxTokens,
	})
	if judge {
		provider, defaultModel, err := buildLLMProvider(cfg, judgeProvider)
		if err != nil {
			return err
		}
		if strings.TrimSpace(judgeModel) == "" {
			judgeModel = defaultModel
		}
		judgeLLM := eval.NewLLMJudge(provider, judgeModel)
		if judgeMaxTokens > 0 {
			judgeLLM.SetAnswerMaxTokens(judgeMaxTokens)
		}
		evaluator.WithJudge(judgeLLM)
	}
	report, err := evaluator.Evaluate(cmd.Context(), set)
	if err != nil {
		return err
	}

	if output != "" {
		payload, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal report: %w", err)
		}
		if err := os.WriteFile(output, payload, 0o644); err != nil {
			return fmt.Errorf("write report: %w", err)
		}
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "RAG Evaluation: %s\n", report.TestSetName)
	fmt.Fprintf(out, "Cases: %d\n", report.Summary.Cases)
	fmt.Fprintf(out, "Precision: %.3f\n", report.Summary.AvgPrecision)
	fmt.Fprintf(out, "Recall: %.3f\n", report.Summary.AvgRecall)
	fmt.Fprintf(out, "MRR: %.3f\n", report.Summary.AvgMRR)
	fmt.Fprintf(out, "NDCG: %.3f\n", report.Summary.AvgNDCG)
	if report.Summary.JudgeCases > 0 {
		fmt.Fprintf(out, "Judged Cases: %d\n", report.Summary.JudgeCases)
		fmt.Fprintf(out, "Answer Relevance: %.3f\n", report.Summary.AvgRelevance)
		fmt.Fprintf(out, "Answer Faithfulness: %.3f\n", report.Summary.AvgFaithfulness)
		fmt.Fprintf(out, "Context Recall: %.3f\n", report.Summary.AvgContextRecall)
	}
	if report.Summary.AnswerCases > 0 {
		fmt.Fprintf(out, "Answer Cases: %d\n", report.Summary.AnswerCases)
		fmt.Fprintf(out, "Answer Coverage: %.3f\n", report.Summary.AvgAnswerCoverage)
	}
	if output != "" {
		fmt.Fprintf(out, "Report written to %s\n", output)
	}
	return nil
}

func buildRAGIndexManager(cfg *config.Config) (*index.Manager, io.Closer, error) {
	if cfg == nil {
		return nil, nil, fmt.Errorf("config is required")
	}
	storeCfg := cfg.RAG.Store
	backend := strings.ToLower(strings.TrimSpace(storeCfg.Backend))
	if backend == "" {
		backend = "pgvector"
	}
	if backend != "pgvector" && backend != "postgres" && backend != "postgresql" {
		return nil, nil, fmt.Errorf("unsupported RAG backend %q", backend)
	}

	var embProvider embeddings.Provider
	var err error
	switch strings.ToLower(strings.TrimSpace(cfg.RAG.Embeddings.Provider)) {
	case "openai", "":
		embProvider, err = openai.New(openai.Config{
			APIKey:  cfg.RAG.Embeddings.APIKey,
			BaseURL: cfg.RAG.Embeddings.BaseURL,
			Model:   cfg.RAG.Embeddings.Model,
		})
	case "ollama":
		embProvider, err = ollama.New(ollama.Config{
			BaseURL: cfg.RAG.Embeddings.BaseURL,
			Model:   cfg.RAG.Embeddings.Model,
		})
	default:
		return nil, nil, fmt.Errorf("unknown RAG embedding provider %q", cfg.RAG.Embeddings.Provider)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("init embedder: %w", err)
	}

	dimension := storeCfg.Dimension
	if dimension == 0 {
		dimension = embProvider.Dimension()
	}
	if embProvider.Dimension() != dimension {
		return nil, nil, fmt.Errorf("embedding dimension mismatch: store=%d embedder=%d", dimension, embProvider.Dimension())
	}

	dsn := strings.TrimSpace(storeCfg.DSN)
	if dsn == "" && storeCfg.UseDatabaseURL {
		dsn = strings.TrimSpace(cfg.Database.URL)
	}
	if dsn == "" {
		return nil, nil, fmt.Errorf("rag.store.dsn is required or set rag.store.use_database_url with database.url")
	}

	runMigrations := true
	if storeCfg.RunMigrations != nil {
		runMigrations = *storeCfg.RunMigrations
	}
	store, err := pgvector.New(pgvector.Config{
		DSN:           dsn,
		Dimension:     dimension,
		RunMigrations: runMigrations,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("init rag store: %w", err)
	}

	idx := index.NewManager(store, embProvider, &index.Config{
		ChunkSize:          cfg.RAG.Chunking.ChunkSize,
		ChunkOverlap:       cfg.RAG.Chunking.ChunkOverlap,
		EmbeddingBatchSize: cfg.RAG.Embeddings.BatchSize,
		DefaultSource:      "rag_eval",
	})
	return idx, store, nil
}

func buildLLMProvider(cfg *config.Config, providerID string) (agent.LLMProvider, string, error) {
	if cfg == nil {
		return nil, "", fmt.Errorf("config is required")
	}
	if strings.TrimSpace(providerID) == "" {
		providerID = cfg.LLM.DefaultProvider
	}
	baseID, profileID := splitProviderProfileID(providerID)
	providerKey := strings.ToLower(strings.TrimSpace(baseID))
	providerCfg, ok := cfg.LLM.Providers[providerKey]
	if !ok {
		providerCfg, ok = cfg.LLM.Providers[baseID]
	}
	if !ok {
		return nil, "", fmt.Errorf("provider config missing for %q", providerID)
	}
	effectiveCfg, err := resolveProviderProfile(providerCfg, profileID)
	if err != nil {
		return nil, "", fmt.Errorf("provider %q: %w", providerID, err)
	}

	switch providerKey {
	case "anthropic":
		if effectiveCfg.APIKey == "" {
			return nil, "", errors.New("anthropic api key is required (set llm.providers.anthropic.api_key)")
		}
		provider, err := providers.NewAnthropicProvider(providers.AnthropicConfig{
			APIKey:       effectiveCfg.APIKey,
			DefaultModel: effectiveCfg.DefaultModel,
			BaseURL:      effectiveCfg.BaseURL,
		})
		if err != nil {
			return nil, "", err
		}
		return provider, resolveDefaultModel(effectiveCfg.DefaultModel, provider), nil
	case "openai":
		if effectiveCfg.APIKey == "" {
			return nil, "", errors.New("openai api key is required (set llm.providers.openai.api_key)")
		}
		provider := providers.NewOpenAIProviderWithConfig(providers.OpenAIConfig{
			APIKey:  effectiveCfg.APIKey,
			BaseURL: effectiveCfg.BaseURL,
		})
		return provider, resolveDefaultModel(effectiveCfg.DefaultModel, provider), nil
	case "openrouter":
		if effectiveCfg.APIKey == "" {
			return nil, "", errors.New("openrouter api key is required (set llm.providers.openrouter.api_key)")
		}
		provider, err := providers.NewOpenRouterProvider(providers.OpenRouterConfig{
			APIKey:       effectiveCfg.APIKey,
			DefaultModel: effectiveCfg.DefaultModel,
		})
		if err != nil {
			return nil, "", err
		}
		return provider, resolveDefaultModel(effectiveCfg.DefaultModel, provider), nil
	case "google", "gemini":
		if effectiveCfg.APIKey == "" {
			return nil, "", fmt.Errorf("%s api key is required (set llm.providers.%s.api_key)", providerKey, providerKey)
		}
		provider, err := providers.NewGoogleProvider(providers.GoogleConfig{
			APIKey:       effectiveCfg.APIKey,
			DefaultModel: effectiveCfg.DefaultModel,
		})
		if err != nil {
			return nil, "", err
		}
		return provider, resolveDefaultModel(effectiveCfg.DefaultModel, provider), nil
	case "azure":
		if effectiveCfg.APIKey == "" {
			return nil, "", errors.New("azure api key is required (set llm.providers.azure.api_key)")
		}
		endpoint := strings.TrimSpace(effectiveCfg.BaseURL)
		if endpoint == "" {
			return nil, "", errors.New("azure base_url is required (set llm.providers.azure.base_url)")
		}
		apiVersion := strings.TrimSpace(effectiveCfg.APIVersion)
		if apiVersion == "" {
			apiVersion = strings.TrimSpace(os.Getenv("AZURE_OPENAI_API_VERSION"))
		}
		provider, err := providers.NewAzureOpenAIProvider(providers.AzureOpenAIConfig{
			Endpoint:     endpoint,
			APIKey:       effectiveCfg.APIKey,
			APIVersion:   apiVersion,
			DefaultModel: effectiveCfg.DefaultModel,
		})
		if err != nil {
			return nil, "", err
		}
		return provider, resolveDefaultModel(effectiveCfg.DefaultModel, provider), nil
	case "bedrock":
		region := strings.TrimSpace(cfg.LLM.Bedrock.Region)
		provider, err := providers.NewBedrockProvider(providers.BedrockConfig{
			Region:       region,
			DefaultModel: effectiveCfg.DefaultModel,
		})
		if err != nil {
			return nil, "", err
		}
		return provider, resolveDefaultModel(effectiveCfg.DefaultModel, provider), nil
	case "ollama":
		defaultModel := strings.TrimSpace(effectiveCfg.DefaultModel)
		if defaultModel == "" {
			defaultModel = "llama3"
		}
		provider := providers.NewOllamaProvider(providers.OllamaConfig{
			BaseURL:      effectiveCfg.BaseURL,
			DefaultModel: defaultModel,
		})
		return provider, resolveDefaultModel(defaultModel, provider), nil
	case "copilot-proxy":
		models := []string{}
		if strings.TrimSpace(effectiveCfg.DefaultModel) != "" {
			models = []string{strings.TrimSpace(effectiveCfg.DefaultModel)}
		}
		provider, err := providers.NewCopilotProxyProvider(providers.CopilotProxyConfig{
			BaseURL: effectiveCfg.BaseURL,
			Models:  models,
		})
		if err != nil {
			return nil, "", err
		}
		return provider, resolveDefaultModel(effectiveCfg.DefaultModel, provider), nil
	default:
		return nil, "", fmt.Errorf("unsupported provider %q", providerKey)
	}
}

func resolveDefaultModel(configured string, provider agent.LLMProvider) string {
	if strings.TrimSpace(configured) != "" {
		return strings.TrimSpace(configured)
	}
	models := provider.Models()
	if len(models) > 0 {
		return models[0].ID
	}
	return ""
}

func splitProviderProfileID(value string) (string, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	for _, sep := range []string{":", "@", "/"} {
		if parts := strings.SplitN(value, sep, 2); len(parts) == 2 {
			return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		}
	}
	return value, ""
}

func resolveProviderProfile(cfg config.LLMProviderConfig, profileID string) (config.LLMProviderConfig, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return cfg, nil
	}
	if cfg.Profiles == nil {
		return cfg, fmt.Errorf("provider profile %q not configured (define under llm.providers.<provider>.profiles)", profileID)
	}
	profile, ok := cfg.Profiles[profileID]
	if !ok {
		return cfg, fmt.Errorf("provider profile %q not configured (define under llm.providers.<provider>.profiles)", profileID)
	}
	effective := cfg
	if profile.APIKey != "" {
		effective.APIKey = profile.APIKey
	}
	if profile.DefaultModel != "" {
		effective.DefaultModel = profile.DefaultModel
	}
	if profile.BaseURL != "" {
		effective.BaseURL = profile.BaseURL
	}
	return effective, nil
}

func runRagPackInstall(cmd *cobra.Command, configPath, packDir string) error {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	manager, closer, err := buildRAGIndexManager(cfg)
	if err != nil {
		return err
	}
	if closer != nil {
		defer closer.Close()
	}

	report, err := packs.Install(cmd.Context(), packDir, manager)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Installed pack: %s\n", report.PackName)
	fmt.Fprintf(out, "Documents: %d\n", report.Documents)
	fmt.Fprintf(out, "Chunks: %d\n", report.Chunks)
	fmt.Fprintf(out, "Duration: %v\n", report.Duration)
	if len(report.Errors) > 0 {
		fmt.Fprintf(out, "Errors: %d\n", len(report.Errors))
		for _, e := range report.Errors {
			fmt.Fprintf(out, "  - %s\n", e)
		}
	}
	return nil
}

func runRagPackList(cmd *cobra.Command, configPath, root string) error {
	return runRagPackQuery(cmd, configPath, root, "")
}

func runRagPackSearch(cmd *cobra.Command, configPath, root, query string) error {
	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("query is required")
	}
	return runRagPackQuery(cmd, configPath, root, query)
}

func runRagPackQuery(cmd *cobra.Command, configPath, root, query string) error {
	configPath = resolveConfigPath(configPath)
	roots, err := resolvePackRoots(configPath, root)
	if err != nil {
		return err
	}

	found, warnings := discoverPacks(roots)
	if query != "" {
		found = packs.FilterPacks(found, query)
	}

	out := cmd.OutOrStdout()
	if len(found) == 0 {
		if len(warnings) > 0 {
			return errors.Join(warnings...)
		}
		fmt.Fprintln(out, "No packs found.")
		return nil
	}

	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tDOCS\tPATH\tDESCRIPTION")
	for _, pack := range found {
		description := strings.TrimSpace(pack.Pack.Description)
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n",
			pack.Pack.Name,
			pack.Pack.Version,
			len(pack.Pack.Documents),
			pack.Path,
			description,
		)
	}
	if err := w.Flush(); err != nil {
		return err
	}

	if len(warnings) > 0 {
		for _, warn := range warnings {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", warn)
		}
	}
	return nil
}

func resolvePackRoots(configPath, root string) ([]string, error) {
	if strings.TrimSpace(root) != "" {
		return []string{root}, nil
	}

	workspacePath := "."
	if strings.TrimSpace(configPath) != "" {
		cfg, err := config.Load(configPath)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		} else if strings.TrimSpace(cfg.Workspace.Path) != "" {
			workspacePath = cfg.Workspace.Path
		}
	}

	roots := []string{filepath.Join(workspacePath, "packs")}
	if homeDir, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(homeDir, ".nexus", "packs"))
	}
	return roots, nil
}

func discoverPacks(roots []string) ([]packs.DiscoveredPack, []error) {
	var discovered []packs.DiscoveredPack
	var warnings []error

	seen := map[string]struct{}{}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			warnings = append(warnings, fmt.Errorf("resolve pack root %q: %w", root, err))
			continue
		}
		if _, ok := seen[absRoot]; ok {
			continue
		}
		seen[absRoot] = struct{}{}

		info, err := os.Stat(absRoot)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			warnings = append(warnings, fmt.Errorf("stat pack root %q: %w", absRoot, err))
			continue
		}
		if !info.IsDir() {
			warnings = append(warnings, fmt.Errorf("pack root is not a directory: %s", absRoot))
			continue
		}

		packsFound, err := packs.Discover(absRoot)
		if err != nil {
			warnings = append(warnings, err)
		}
		discovered = append(discovered, packsFound...)
	}

	sort.Slice(discovered, func(i, j int) bool {
		return strings.ToLower(discovered[i].Pack.Name) < strings.ToLower(discovered[j].Pack.Name)
	})
	return discovered, warnings
}
