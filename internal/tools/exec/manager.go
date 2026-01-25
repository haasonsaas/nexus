package exec

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/haasonsaas/nexus/internal/tools/files"
)

// Manager tracks background processes started via the exec tool.
type Manager struct {
	mu        sync.Mutex
	processes map[string]*process
	resolver  files.Resolver
	maxOutput int
}

// NewManager creates a new process manager scoped to the workspace.
func NewManager(workspace string) *Manager {
	return &Manager{
		processes: map[string]*process{},
		resolver:  files.Resolver{Root: workspace},
		maxOutput: 64000,
	}
}

// RunCommand executes a command synchronously using the manager's workspace resolver.
func (m *Manager) RunCommand(ctx context.Context, command string, cwd string, env map[string]string, input string, timeout time.Duration) (ExecResult, error) {
	return m.runSync(ctx, command, cwd, env, input, timeout)
}

type process struct {
	id       string
	command  string
	cmd      *exec.Cmd
	stdout   *limitedBuffer
	stderr   *limitedBuffer
	stdin    io.WriteCloser
	started  time.Time
	done     chan struct{}
	exitCode int
	err      error
}

func (p *process) status() string {
	select {
	case <-p.done:
		return "exited"
	default:
		return "running"
	}
}

func (m *Manager) runSync(ctx context.Context, command string, cwd string, env map[string]string, input string, timeout time.Duration) (result ExecResult, err error) {
	runCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd, stdout, stderr, err := m.buildCommand(runCtx, command, cwd, env, input)
	if err != nil {
		return ExecResult{}, err
	}
	start := time.Now()
	err = cmd.Run()
	result = ExecResult{
		Command:  command,
		Cwd:      cmd.Dir,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: time.Since(start),
		ExitCode: exitCode(err),
		Finished: true,
	}
	if err != nil {
		result.Error = err.Error()
	}
	return result, nil
}

func (m *Manager) startBackground(ctx context.Context, command string, cwd string, env map[string]string, input string, timeout time.Duration) (*process, error) {
	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	cancelOnErr := func() {
		if cancel != nil {
			cancel()
		}
	}

	cmd, stdout, stderr, err := m.buildCommand(runCtx, command, cwd, env, "")
	if err != nil {
		cancelOnErr()
		return nil, err
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancelOnErr()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	proc := &process{
		id:      uuid.NewString(),
		command: command,
		cmd:     cmd,
		stdout:  stdout,
		stderr:  stderr,
		stdin:   stdin,
		started: time.Now(),
		done:    make(chan struct{}),
	}

	if err := cmd.Start(); err != nil {
		cancelOnErr()
		_ = stdin.Close()
		return nil, fmt.Errorf("start command: %w", err)
	}

	if input != "" {
		if _, err := io.WriteString(stdin, input); err != nil {
			_ = stdin.Close()
		}
	}

	go func() {
		err := cmd.Wait()
		proc.exitCode = exitCode(err)
		proc.err = err
		close(proc.done)
		if cancel != nil {
			cancel()
		}
		_ = stdin.Close()
	}()

	m.mu.Lock()
	m.processes[proc.id] = proc
	m.mu.Unlock()

	return proc, nil
}

func (m *Manager) buildCommand(ctx context.Context, command string, cwd string, env map[string]string, input string) (*exec.Cmd, *limitedBuffer, *limitedBuffer, error) {
	if command == "" {
		return nil, nil, nil, fmt.Errorf("command is required")
	}

	dir := ""
	if cwd != "" {
		resolved, err := m.resolver.Resolve(cwd)
		if err != nil {
			return nil, nil, nil, err
		}
		dir = resolved
	}
	if dir == "" {
		resolved, err := m.resolver.Resolve(".")
		if err == nil {
			dir = resolved
		}
	}

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	if dir != "" {
		cmd.Dir = dir
	}
	if env != nil {
		base := os.Environ()
		for k, v := range env {
			base = append(base, k+"="+v)
		}
		cmd.Env = base
	}

	stdout := newLimitedBuffer(m.maxOutput)
	stderr := newLimitedBuffer(m.maxOutput)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}

	return cmd, stdout, stderr, nil
}

func (m *Manager) list() []ProcessInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ProcessInfo, 0, len(m.processes))
	for _, proc := range m.processes {
		out = append(out, proc.info())
	}
	return out
}

func (m *Manager) get(id string) (*process, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	proc, ok := m.processes[id]
	return proc, ok
}

func (m *Manager) remove(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.processes[id]; ok {
		delete(m.processes, id)
		return true
	}
	return false
}

func (p *process) info() ProcessInfo {
	return ProcessInfo{
		ID:        p.id,
		Command:   p.command,
		Status:    p.status(),
		StartedAt: p.started,
		ExitCode:  p.exitCode,
		Error:     errorString(p.err),
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

type limitedBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func newLimitedBuffer(max int) *limitedBuffer {
	return &limitedBuffer{max: max}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.max > 0 && len(b.buf) >= b.max {
		return len(p), nil
	}
	remaining := b.max - len(b.buf)
	if b.max > 0 && len(p) > remaining {
		b.buf = append(b.buf, p[:remaining]...)
		return len(p), nil
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}

// ExecResult summarizes a synchronous exec call.
type ExecResult struct {
	Command  string        `json:"command"`
	Cwd      string        `json:"cwd"`
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
	ExitCode int           `json:"exit_code"`
	Duration time.Duration `json:"duration"`
	Finished bool          `json:"finished"`
	Error    string        `json:"error,omitempty"`
}

// ProcessInfo summarizes a managed process.
type ProcessInfo struct {
	ID        string    `json:"id"`
	Command   string    `json:"command"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
	ExitCode  int       `json:"exit_code"`
	Error     string    `json:"error,omitempty"`
}
