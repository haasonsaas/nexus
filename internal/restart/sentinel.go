package restart

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SentinelFilename is the name of the restart sentinel file.
const SentinelFilename = "restart-sentinel.json"

// RestartKind represents the type of restart operation.
type RestartKind string

const (
	KindConfigApply RestartKind = "config-apply"
	KindUpdate      RestartKind = "update"
	KindRestart     RestartKind = "restart"
)

// RestartStatus represents the outcome of a restart operation.
type RestartStatus string

const (
	StatusOK      RestartStatus = "ok"
	StatusError   RestartStatus = "error"
	StatusSkipped RestartStatus = "skipped"
)

// SentinelLog captures stdout/stderr tails and exit code from a command.
type SentinelLog struct {
	StdoutTail *string `json:"stdoutTail,omitempty"`
	StderrTail *string `json:"stderrTail,omitempty"`
	ExitCode   *int    `json:"exitCode,omitempty"`
}

// SentinelStep captures details of a single step executed during restart.
type SentinelStep struct {
	Name       string       `json:"name"`
	Command    string       `json:"command"`
	Cwd        *string      `json:"cwd,omitempty"`
	DurationMs *int64       `json:"durationMs,omitempty"`
	Log        *SentinelLog `json:"log,omitempty"`
}

// SentinelStats captures statistics about the restart operation.
type SentinelStats struct {
	Mode       string                 `json:"mode,omitempty"`
	Root       string                 `json:"root,omitempty"`
	Before     map[string]interface{} `json:"before,omitempty"`
	After      map[string]interface{} `json:"after,omitempty"`
	Steps      []SentinelStep         `json:"steps,omitempty"`
	Reason     *string                `json:"reason,omitempty"`
	DurationMs *int64                 `json:"durationMs,omitempty"`
}

// DeliveryContext captures routing information for message delivery.
type DeliveryContext struct {
	Channel   string `json:"channel,omitempty"`
	To        string `json:"to,omitempty"`
	AccountID string `json:"accountId,omitempty"`
}

// SentinelPayload contains the main restart event data.
type SentinelPayload struct {
	Kind            RestartKind      `json:"kind"`
	Status          RestartStatus    `json:"status"`
	Ts              int64            `json:"ts"`
	SessionKey      string           `json:"sessionKey,omitempty"`
	DeliveryContext *DeliveryContext `json:"deliveryContext,omitempty"`
	ThreadID        string           `json:"threadId,omitempty"`
	Message         *string          `json:"message,omitempty"`
	DoctorHint      *string          `json:"doctorHint,omitempty"`
	Stats           *SentinelStats   `json:"stats,omitempty"`
}

// Sentinel is the versioned wrapper for restart sentinel data.
type Sentinel struct {
	Version int             `json:"version"`
	Payload SentinelPayload `json:"payload"`
}

// ResolveSentinelPath returns the full path to the restart sentinel file.
func ResolveSentinelPath(stateDir string) string {
	return filepath.Join(stateDir, SentinelFilename)
}

// WriteSentinel writes a restart sentinel to the state directory.
func WriteSentinel(stateDir string, payload SentinelPayload) error {
	sentinelPath := ResolveSentinelPath(stateDir)

	if err := os.MkdirAll(filepath.Dir(sentinelPath), 0755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	sentinel := Sentinel{
		Version: 1,
		Payload: payload,
	}

	data, err := json.MarshalIndent(sentinel, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sentinel: %w", err)
	}

	data = append(data, '\n')
	if err := os.WriteFile(sentinelPath, data, 0644); err != nil {
		return fmt.Errorf("write sentinel: %w", err)
	}

	return nil
}

// ReadSentinel reads and validates a restart sentinel from the state directory.
// Returns nil if the file doesn't exist or is invalid. Invalid files are deleted.
func ReadSentinel(stateDir string) (*Sentinel, error) {
	sentinelPath := ResolveSentinelPath(stateDir)

	data, err := os.ReadFile(sentinelPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sentinel: %w", err)
	}

	var sentinel Sentinel
	if err := json.Unmarshal(data, &sentinel); err != nil {
		// Invalid JSON - delete and return nil
		_ = os.Remove(sentinelPath)
		return nil, nil
	}

	// Validate structure
	if sentinel.Version != 1 {
		_ = os.Remove(sentinelPath)
		return nil, nil
	}

	return &sentinel, nil
}

// ConsumeSentinel reads and then deletes the restart sentinel.
// Returns nil if the file doesn't exist or is invalid.
func ConsumeSentinel(stateDir string) (*Sentinel, error) {
	sentinel, err := ReadSentinel(stateDir)
	if err != nil {
		return nil, err
	}
	if sentinel == nil {
		return nil, nil
	}

	sentinelPath := ResolveSentinelPath(stateDir)
	_ = os.Remove(sentinelPath)

	return sentinel, nil
}

// FormatMessage formats a sentinel payload as a detailed message.
func FormatMessage(payload SentinelPayload) string {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Sprintf("GatewayRestart:\n{\"error\": \"marshal failed\"}")
	}
	return fmt.Sprintf("GatewayRestart:\n%s", string(data))
}

// Summarize creates a short summary of the restart sentinel payload.
func Summarize(payload SentinelPayload) string {
	mode := ""
	if payload.Stats != nil && payload.Stats.Mode != "" {
		mode = fmt.Sprintf(" (%s)", payload.Stats.Mode)
	}
	return fmt.Sprintf("Gateway restart %s %s%s", payload.Kind, payload.Status, mode)
}

// TrimLogTail trims a log string to a maximum number of characters.
// If truncated, an ellipsis prefix is added.
func TrimLogTail(input string, maxChars int) string {
	if input == "" {
		return ""
	}

	// Trim trailing whitespace
	text := input
	for len(text) > 0 && (text[len(text)-1] == ' ' || text[len(text)-1] == '\n' || text[len(text)-1] == '\r' || text[len(text)-1] == '\t') {
		text = text[:len(text)-1]
	}

	if len(text) <= maxChars {
		return text
	}

	return "..." + text[len(text)-maxChars:]
}
