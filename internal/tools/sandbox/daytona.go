package sandbox

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	apiclient "github.com/daytonaio/daytona/libs/api-client-go"
	toolbox "github.com/daytonaio/daytona/libs/toolbox-api-client-go"
	"github.com/google/uuid"
)

const (
	defaultDaytonaAPIURL = "https://app.daytona.io/api"
	daytonaSourceHeader  = "nexus"
)

// DaytonaConfig configures the Daytona sandbox backend.
type DaytonaConfig struct {
	APIKey         string
	JWTToken       string
	OrganizationID string
	APIURL         string
	Target         string
	Snapshot       string
	Image          string
	SandboxClass   string
	WorkspaceDir   string
	NetworkAllow   string
	ReuseSandbox   bool
	AutoStop       *time.Duration
	AutoArchive    *time.Duration
	AutoDelete     *time.Duration
}

type daytonaClient struct {
	apiKey         string
	jwtToken       string
	organizationID string
	apiURL         string
	target         string

	apiClient  *apiclient.APIClient
	httpClient *http.Client

	proxyMu    sync.Mutex
	proxyCache map[string]string
}

func resolveDaytonaConfig(cfg *DaytonaConfig) (*DaytonaConfig, error) {
	resolved := DaytonaConfig{}
	if cfg != nil {
		resolved = *cfg
	}

	resolved.APIKey = strings.TrimSpace(resolved.APIKey)
	resolved.JWTToken = strings.TrimSpace(resolved.JWTToken)
	resolved.OrganizationID = strings.TrimSpace(resolved.OrganizationID)
	resolved.APIURL = strings.TrimSpace(resolved.APIURL)
	resolved.Target = strings.TrimSpace(resolved.Target)
	resolved.Snapshot = strings.TrimSpace(resolved.Snapshot)
	resolved.Image = strings.TrimSpace(resolved.Image)
	resolved.SandboxClass = strings.TrimSpace(resolved.SandboxClass)
	resolved.WorkspaceDir = strings.TrimSpace(resolved.WorkspaceDir)
	resolved.NetworkAllow = strings.TrimSpace(resolved.NetworkAllow)

	if resolved.APIKey == "" {
		resolved.APIKey = strings.TrimSpace(os.Getenv("DAYTONA_API_KEY"))
	}
	if resolved.JWTToken == "" {
		resolved.JWTToken = strings.TrimSpace(os.Getenv("DAYTONA_JWT_TOKEN"))
	}
	if resolved.OrganizationID == "" {
		resolved.OrganizationID = strings.TrimSpace(os.Getenv("DAYTONA_ORGANIZATION_ID"))
	}
	if resolved.APIURL == "" {
		resolved.APIURL = strings.TrimSpace(os.Getenv("DAYTONA_API_URL"))
		if resolved.APIURL == "" {
			resolved.APIURL = strings.TrimSpace(os.Getenv("DAYTONA_SERVER_URL"))
		}
	}
	if resolved.APIURL == "" {
		resolved.APIURL = defaultDaytonaAPIURL
	}
	if resolved.Target == "" {
		resolved.Target = strings.TrimSpace(os.Getenv("DAYTONA_TARGET"))
	}

	if resolved.APIKey == "" && resolved.JWTToken == "" {
		return nil, errors.New("daytona api key or jwt token is required")
	}
	if resolved.JWTToken != "" && resolved.OrganizationID == "" {
		return nil, errors.New("daytona organization id is required when using a jwt token")
	}

	return &resolved, nil
}

func newDaytonaClient(cfg *DaytonaConfig) (*daytonaClient, error) {
	if cfg == nil {
		return nil, errors.New("daytona config is required")
	}

	scheme, host, basePath, err := parseBaseURL(cfg.APIURL)
	if err != nil {
		return nil, err
	}

	apiCfg := apiclient.NewConfiguration()
	apiCfg.Host = host
	apiCfg.Scheme = scheme
	apiCfg.HTTPClient = &http.Client{}
	apiCfg.AddDefaultHeader("X-Daytona-Source", daytonaSourceHeader)
	if cfg.JWTToken != "" && cfg.OrganizationID != "" {
		apiCfg.AddDefaultHeader("X-Daytona-Organization-ID", cfg.OrganizationID)
	}
	apiCfg.Servers = apiclient.ServerConfigurations{
		{URL: fmt.Sprintf("%s://%s%s", scheme, host, basePath)},
	}

	return &daytonaClient{
		apiKey:         cfg.APIKey,
		jwtToken:       cfg.JWTToken,
		organizationID: cfg.OrganizationID,
		apiURL:         cfg.APIURL,
		target:         cfg.Target,
		apiClient:      apiclient.NewAPIClient(apiCfg),
		httpClient:     apiCfg.HTTPClient,
		proxyCache:     make(map[string]string),
	}, nil
}

func (c *daytonaClient) authContext(ctx context.Context) context.Context {
	token := c.apiKey
	if token == "" {
		token = c.jwtToken
	}
	return context.WithValue(ctx, apiclient.ContextAccessToken, token)
}

func (c *daytonaClient) getToolboxProxyURL(ctx context.Context, sandboxID, target string) (string, error) {
	cacheKey := strings.TrimSpace(target)
	c.proxyMu.Lock()
	if cacheKey != "" {
		if cached, ok := c.proxyCache[cacheKey]; ok {
			c.proxyMu.Unlock()
			return cached, nil
		}
	}
	c.proxyMu.Unlock()

	result, httpResp, err := c.apiClient.SandboxAPI.GetToolboxProxyUrl(c.authContext(ctx), sandboxID).Execute()
	if err != nil {
		return "", fmt.Errorf("get toolbox proxy url: %w", formatAPIError(err, httpResp))
	}

	proxyURL := strings.TrimRight(result.GetUrl(), "/")
	if cacheKey != "" {
		c.proxyMu.Lock()
		c.proxyCache[cacheKey] = proxyURL
		c.proxyMu.Unlock()
	}

	return proxyURL, nil
}

func (c *daytonaClient) toolboxClient(ctx context.Context, sandboxID, target string) (*toolbox.APIClient, error) {
	proxyURL, err := c.getToolboxProxyURL(ctx, sandboxID, target)
	if err != nil {
		return nil, err
	}

	toolboxURL := fmt.Sprintf("%s/%s", strings.TrimRight(proxyURL, "/"), sandboxID)
	scheme, host, basePath, err := parseBaseURL(toolboxURL)
	if err != nil {
		return nil, err
	}

	cfg := toolbox.NewConfiguration()
	cfg.Host = host
	cfg.Scheme = scheme
	cfg.HTTPClient = c.httpClient
	cfg.AddDefaultHeader("Authorization", "Bearer "+c.authToken())
	cfg.AddDefaultHeader("X-Daytona-Source", daytonaSourceHeader)
	if c.jwtToken != "" && c.organizationID != "" {
		cfg.AddDefaultHeader("X-Daytona-Organization-ID", c.organizationID)
	}
	cfg.Servers = toolbox.ServerConfigurations{
		{URL: fmt.Sprintf("%s://%s%s", scheme, host, basePath)},
	}

	return toolbox.NewAPIClient(cfg), nil
}

func (c *daytonaClient) authToken() string {
	if c.apiKey != "" {
		return c.apiKey
	}
	return c.jwtToken
}

type daytonaExecutor struct {
	language string
	config   *Config
	client   *daytonaClient

	reuseSandbox  bool
	sandboxID     string
	sandboxCPU    int32
	sandboxMem    int32
	sandboxTarget string
	toolboxClient *toolbox.APIClient
	sandboxMu     sync.Mutex
}

func newDaytonaExecutor(language string, config *Config) (*daytonaExecutor, error) {
	if config == nil || config.daytonaClient == nil {
		return nil, errors.New("daytona client not initialized")
	}

	return &daytonaExecutor{
		language:     language,
		config:       config,
		client:       config.daytonaClient,
		reuseSandbox: config.Daytona != nil && config.Daytona.ReuseSandbox,
	}, nil
}

func (d *daytonaExecutor) Run(ctx context.Context, params *ExecuteParams, workspace string) (*ExecuteResult, error) {
	if params == nil {
		return nil, errors.New("missing execution params")
	}

	_, _, toolboxClient, cleanup, err := d.ensureSandbox(ctx, params)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	workDir, err := d.resolveWorkspaceDir(ctx, toolboxClient)
	if err != nil {
		return nil, err
	}

	runDir := path.Join(workDir, "nexus-"+uuid.NewString())
	if err := d.createFolder(ctx, toolboxClient, runDir); err != nil {
		return nil, err
	}
	defer d.cleanupRunDir(context.Background(), toolboxClient, runDir)

	uploadedFiles, err := d.uploadWorkspace(ctx, toolboxClient, workspace, runDir)
	if err != nil {
		return nil, err
	}
	if d.isReadOnlyAccess(params.WorkspaceAccess) {
		if err := d.applyReadOnlyAccess(ctx, toolboxClient, runDir, uploadedFiles); err != nil {
			return nil, err
		}
	}

	command := d.buildCommand(params)
	execReq := toolbox.NewExecuteRequest(command)
	execReq.SetCwd(runDir)
	if params.Timeout > 0 {
		execReq.SetTimeout(int32(params.Timeout))
	}

	resp, httpResp, err := toolboxClient.ProcessAPI.ExecuteCommand(ctx).Request(*execReq).Execute()
	if err != nil {
		return nil, fmt.Errorf("daytona execute command: %w", formatToolboxError(err, httpResp))
	}

	exitCode := 0
	if resp.ExitCode != nil {
		exitCode = int(*resp.ExitCode)
	}

	return &ExecuteResult{
		Stdout:   resp.Result,
		ExitCode: exitCode,
	}, nil
}

func (d *daytonaExecutor) Language() string {
	return d.language
}

func (d *daytonaExecutor) Close() error {
	if d.reuseSandbox {
		d.sandboxMu.Lock()
		sandboxID := d.sandboxID
		d.sandboxID = ""
		d.toolboxClient = nil
		d.sandboxTarget = ""
		d.sandboxMu.Unlock()
		if sandboxID != "" {
			_ = d.deleteSandbox(context.Background(), sandboxID)
		}
	}
	return nil
}

func (d *daytonaExecutor) ensureSandbox(ctx context.Context, params *ExecuteParams) (string, string, *toolbox.APIClient, func(), error) {
	if !d.reuseSandbox {
		sandbox, _, _, err := d.createSandbox(ctx, params)
		if err != nil {
			return "", "", nil, nil, err
		}
		if sandbox.GetState() != apiclient.SANDBOXSTATE_STARTED {
			if err := d.waitForSandbox(ctx, sandbox.GetId()); err != nil {
				return "", "", nil, nil, err
			}
		}
		toolboxClient, err := d.client.toolboxClient(ctx, sandbox.GetId(), sandbox.GetTarget())
		if err != nil {
			return "", "", nil, nil, err
		}
		cleanup := func() {
			if err := d.deleteSandbox(context.Background(), sandbox.GetId()); err != nil {
				_ = err
			}
		}
		return sandbox.GetId(), sandbox.GetTarget(), toolboxClient, cleanup, nil
	}

	requestedCPU := cpuToVCPU(params.CPULimit)
	requestedMem := memToGB(params.MemLimit)

	d.sandboxMu.Lock()
	sandboxID := d.sandboxID
	sandboxTarget := d.sandboxTarget
	toolboxClient := d.toolboxClient
	sandboxCPU := d.sandboxCPU
	sandboxMem := d.sandboxMem
	d.sandboxMu.Unlock()

	if sandboxID != "" && (sandboxCPU != requestedCPU || sandboxMem != requestedMem) {
		_ = d.deleteSandbox(context.Background(), sandboxID)
		sandboxID = ""
	}

	if sandboxID == "" {
		sandbox, cpu, mem, err := d.createSandbox(ctx, params)
		if err != nil {
			return "", "", nil, nil, err
		}
		if sandbox.GetState() != apiclient.SANDBOXSTATE_STARTED {
			if err := d.waitForSandbox(ctx, sandbox.GetId()); err != nil {
				return "", "", nil, nil, err
			}
		}
		toolboxClient, err = d.client.toolboxClient(ctx, sandbox.GetId(), sandbox.GetTarget())
		if err != nil {
			return "", "", nil, nil, err
		}
		d.sandboxMu.Lock()
		d.sandboxID = sandbox.GetId()
		d.toolboxClient = toolboxClient
		d.sandboxTarget = sandbox.GetTarget()
		d.sandboxCPU = cpu
		d.sandboxMem = mem
		d.sandboxMu.Unlock()
		sandboxID = sandbox.GetId()
		sandboxTarget = sandbox.GetTarget()
	} else {
		if err := d.ensureSandboxRunning(ctx, sandboxID); err != nil {
			d.sandboxMu.Lock()
			d.sandboxID = ""
			d.toolboxClient = nil
			d.sandboxTarget = ""
			d.sandboxCPU = 0
			d.sandboxMem = 0
			d.sandboxMu.Unlock()
			return d.ensureSandbox(ctx, params)
		}
		if toolboxClient == nil {
			var err error
			toolboxClient, err = d.client.toolboxClient(ctx, sandboxID, sandboxTarget)
			if err != nil {
				return "", "", nil, nil, err
			}
			d.sandboxMu.Lock()
			d.toolboxClient = toolboxClient
			d.sandboxMu.Unlock()
		}
	}

	return sandboxID, sandboxTarget, toolboxClient, func() {}, nil
}

func (d *daytonaExecutor) createSandbox(ctx context.Context, params *ExecuteParams) (*apiclient.Sandbox, int32, int32, error) {
	createReq := apiclient.NewCreateSandbox()
	name := fmt.Sprintf("nexus-%s", uuid.NewString())
	createReq.SetName(name)

	if d.client.target != "" {
		createReq.SetTarget(d.client.target)
	}

	if d.config.Daytona != nil {
		if d.config.Daytona.Snapshot != "" {
			createReq.SetSnapshot(d.config.Daytona.Snapshot)
		} else if d.config.Daytona.Image != "" {
			buildInfo := apiclient.CreateBuildInfo{
				DockerfileContent: fmt.Sprintf("FROM %s", d.config.Daytona.Image),
			}
			createReq.SetBuildInfo(buildInfo)
		}
		if d.config.Daytona.SandboxClass != "" {
			createReq.SetClass(d.config.Daytona.SandboxClass)
		}
		if d.config.Daytona.NetworkAllow != "" && d.config.NetworkEnabled {
			createReq.SetNetworkAllowList(d.config.Daytona.NetworkAllow)
		}
	}

	if !d.config.NetworkEnabled {
		createReq.SetNetworkBlockAll(true)
	}

	vcpus := cpuToVCPU(params.CPULimit)
	if vcpus > 0 {
		createReq.SetCpu(vcpus)
	}
	memGB := memToGB(params.MemLimit)
	if memGB > 0 {
		createReq.SetMemory(memGB)
	}

	if d.config.Daytona != nil {
		if minutes := durationToMinutes(d.config.Daytona.AutoStop); minutes != nil {
			createReq.SetAutoStopInterval(*minutes)
		}
		if minutes := durationToMinutes(d.config.Daytona.AutoArchive); minutes != nil {
			createReq.SetAutoArchiveInterval(*minutes)
		}
		if minutes := durationToMinutes(d.config.Daytona.AutoDelete); minutes != nil {
			createReq.SetAutoDeleteInterval(*minutes)
		}
	}

	sandbox, httpResp, err := d.client.apiClient.SandboxAPI.CreateSandbox(d.client.authContext(ctx)).CreateSandbox(*createReq).Execute()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("daytona create sandbox: %w", formatAPIError(err, httpResp))
	}

	state := sandbox.GetState()
	if state == apiclient.SANDBOXSTATE_ERROR || state == apiclient.SANDBOXSTATE_BUILD_FAILED {
		return nil, 0, 0, fmt.Errorf("daytona sandbox failed to start: %s", state)
	}

	return sandbox, vcpus, memGB, nil
}

func (d *daytonaExecutor) waitForSandbox(ctx context.Context, sandboxID string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		sandbox, httpResp, err := d.client.apiClient.SandboxAPI.GetSandbox(d.client.authContext(ctx), sandboxID).Execute()
		if err != nil {
			return fmt.Errorf("daytona sandbox status: %w", formatAPIError(err, httpResp))
		}

		switch sandbox.GetState() {
		case apiclient.SANDBOXSTATE_STARTED:
			return nil
		case apiclient.SANDBOXSTATE_ERROR, apiclient.SANDBOXSTATE_BUILD_FAILED, apiclient.SANDBOXSTATE_DESTROYED:
			return fmt.Errorf("daytona sandbox failed: %s", sandbox.GetState())
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (d *daytonaExecutor) ensureSandboxRunning(ctx context.Context, sandboxID string) error {
	sandbox, httpResp, err := d.client.apiClient.SandboxAPI.GetSandbox(d.client.authContext(ctx), sandboxID).Execute()
	if err != nil {
		return fmt.Errorf("daytona sandbox status: %w", formatAPIError(err, httpResp))
	}

	switch sandbox.GetState() {
	case apiclient.SANDBOXSTATE_STARTED:
		return nil
	case apiclient.SANDBOXSTATE_STOPPED:
		_, httpResp, err := d.client.apiClient.SandboxAPI.StartSandbox(d.client.authContext(ctx), sandboxID).Execute()
		if err != nil {
			return fmt.Errorf("daytona start sandbox: %w", formatAPIError(err, httpResp))
		}
		return d.waitForSandbox(ctx, sandboxID)
	default:
		return fmt.Errorf("daytona sandbox unavailable: %s", sandbox.GetState())
	}
}

func (d *daytonaExecutor) resolveWorkspaceDir(ctx context.Context, toolboxClient *toolbox.APIClient) (string, error) {
	if d.config.Daytona != nil && d.config.Daytona.WorkspaceDir != "" {
		if strings.HasPrefix(d.config.Daytona.WorkspaceDir, "/") {
			return d.config.Daytona.WorkspaceDir, nil
		}
		baseDir, err := d.fetchWorkDir(ctx, toolboxClient)
		if err != nil {
			return "", err
		}
		return path.Join(baseDir, d.config.Daytona.WorkspaceDir), nil
	}

	return d.fetchWorkDir(ctx, toolboxClient)
}

func (d *daytonaExecutor) fetchWorkDir(ctx context.Context, toolboxClient *toolbox.APIClient) (string, error) {
	resp, httpResp, err := toolboxClient.InfoAPI.GetWorkDir(ctx).Execute()
	if err == nil && resp != nil && resp.GetDir() != "" {
		return resp.GetDir(), nil
	}
	if err != nil {
		return "", fmt.Errorf("daytona get work dir: %w", formatToolboxError(err, httpResp))
	}

	return "/home/daytona", nil
}

func (d *daytonaExecutor) createFolder(ctx context.Context, toolboxClient *toolbox.APIClient, dir string) error {
	httpResp, err := toolboxClient.FileSystemAPI.CreateFolder(ctx).Path(dir).Mode("0755").Execute()
	if err == nil {
		return nil
	}
	if httpResp != nil && httpResp.StatusCode == http.StatusConflict {
		return nil
	}
	return fmt.Errorf("daytona create folder: %w", formatToolboxError(err, httpResp))
}

func (d *daytonaExecutor) uploadWorkspace(ctx context.Context, toolboxClient *toolbox.APIClient, localDir, remoteDir string) ([]string, error) {
	entries, err := os.ReadDir(localDir)
	if err != nil {
		return nil, fmt.Errorf("daytona read workspace: %w", err)
	}

	uploaded := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		localPath := filepath.Join(localDir, entry.Name())
		file, err := os.Open(localPath)
		if err != nil {
			return nil, fmt.Errorf("daytona open workspace file: %w", err)
		}

		remotePath := path.Join(remoteDir, entry.Name())
		_, httpResp, uploadErr := toolboxClient.FileSystemAPI.UploadFile(ctx).Path(remotePath).File(file).Execute()
		file.Close()
		if uploadErr != nil {
			return nil, fmt.Errorf("daytona upload file: %w", formatToolboxError(uploadErr, httpResp))
		}
		uploaded = append(uploaded, remotePath)
	}

	return uploaded, nil
}

func (d *daytonaExecutor) buildCommand(params *ExecuteParams) string {
	command := getShellCommand(d.language)
	if params.Stdin != "" {
		command = fmt.Sprintf("%s < stdin.txt", command)
	}
	return command
}

func (d *daytonaExecutor) isReadOnlyAccess(mode WorkspaceAccessMode) bool {
	switch mode {
	case WorkspaceReadWrite:
		return false
	case WorkspaceNone, WorkspaceReadOnly:
		return true
	case "":
		return true
	default:
		return true
	}
}

func (d *daytonaExecutor) applyReadOnlyAccess(ctx context.Context, toolboxClient *toolbox.APIClient, runDir string, files []string) error {
	for _, file := range files {
		httpResp, err := toolboxClient.FileSystemAPI.SetFilePermissions(ctx).Path(file).Mode("0444").Execute()
		if err != nil {
			return fmt.Errorf("daytona set file permissions: %w", formatToolboxError(err, httpResp))
		}
	}

	httpResp, err := toolboxClient.FileSystemAPI.SetFilePermissions(ctx).Path(runDir).Mode("0555").Execute()
	if err != nil {
		return fmt.Errorf("daytona set directory permissions: %w", formatToolboxError(err, httpResp))
	}

	return nil
}

func (d *daytonaExecutor) cleanupRunDir(ctx context.Context, toolboxClient *toolbox.APIClient, runDir string) {
	httpResp, err := toolboxClient.FileSystemAPI.DeleteFile(ctx).Path(runDir).Recursive(true).Execute()
	if err != nil && httpResp != nil && httpResp.StatusCode != http.StatusNotFound {
		_ = err
	}
}

func (d *daytonaExecutor) deleteSandbox(ctx context.Context, sandboxID string) error {
	_, _, err := d.client.apiClient.SandboxAPI.DeleteSandbox(d.client.authContext(ctx), sandboxID).Execute()
	return err
}

func getShellCommand(language string) string {
	switch language {
	case "python":
		return "python main.py"
	case "nodejs":
		return "node main.js"
	case "go":
		return "go run main.go"
	case "bash":
		return "bash main.sh"
	default:
		return "cat main.txt"
	}
}

func parseBaseURL(raw string) (string, string, string, error) {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		return "", "", "", errors.New("empty url")
	}
	if !strings.Contains(normalized, "://") {
		normalized = "https://" + normalized
	}

	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", "", "", err
	}

	scheme := parsed.Scheme
	host := parsed.Host
	basePath := strings.TrimRight(parsed.Path, "/")
	if scheme == "" || host == "" {
		return "", "", "", fmt.Errorf("invalid url: %s", raw)
	}

	return scheme, host, basePath, nil
}

func formatAPIError(err error, resp *http.Response) error {
	if resp == nil {
		return err
	}
	return fmt.Errorf("%s (status %s)", err.Error(), resp.Status)
}

func formatToolboxError(err error, resp *http.Response) error {
	if resp == nil {
		return err
	}
	return fmt.Errorf("%s (status %s)", err.Error(), resp.Status)
}

func cpuToVCPU(millicores int) int32 {
	if millicores <= 0 {
		return 0
	}
	vcpus := int32((millicores + 999) / 1000)
	if vcpus < 1 {
		return 1
	}
	return vcpus
}

func memToGB(memMB int) int32 {
	if memMB <= 0 {
		return 0
	}
	memGB := int32((memMB + 1023) / 1024)
	if memGB < 1 {
		return 1
	}
	return memGB
}

func durationToMinutes(value *time.Duration) *int32 {
	if value == nil {
		return nil
	}
	minutes := int32(*value / time.Minute)
	return &minutes
}
