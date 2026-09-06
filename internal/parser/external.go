package parser

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/model"
)

// GoProtocolParserName is the runtime adapter for a compiled Go protocol
// package. The package is an executable that implements the line-oriented JSON
// contract documented in docs/GO_PROTOCOL_PACKAGES.md. It is intentionally
// executed out of process so a parser cannot corrupt the API process or block
// the parser goroutine forever.
const GoProtocolParserName = "go_protocol_parser"

const GoProtocolParserVersion = "1.0.0"

const (
	defaultExternalTimeout = 5 * time.Second
	maxExternalTimeout     = 10 * time.Second
	maxExternalArtifact    = 64 << 20
	maxExternalOutput      = 1 << 20
)

// ExternalParser runs a tenant-provided compiled protocol worker. Root is the
// platform data directory; artifact paths are always relative to it.
type ExternalParser struct {
	Root string
}

func (ExternalParser) Name() string    { return GoProtocolParserName }
func (ExternalParser) Version() string { return GoProtocolParserVersion }
func (ExternalParser) Match(Meta) bool { return false }

func (p ExternalParser) Parse(raw model.RawMessage) (*model.StandardMessage, error) {
	return nil, errors.New("go protocol parser requires an uploaded artifact")
}

func (p ExternalParser) ParseWithConfig(raw model.RawMessage, config map[string]any) (*model.StandardMessage, error) {
	return p.ParseWithContext(context.Background(), raw, config)
}

// ParseWithContext lets publication cancel a sample worker when its request or
// the overall validation budget expires.
func (p ExternalParser) ParseWithContext(parent context.Context, raw model.RawMessage, config map[string]any) (*model.StandardMessage, error) {
	var input any = raw
	if artifact, ok := config["artifact"].(map[string]any); ok && artifact["runtime"] == "go-protocol-v2" {
		input = map[string]any{"version": 2, "operation": "decode", "raw": raw, "state": raw.Metadata["protocolState"], "now": raw.ReceivedAt}
	}
	output, err := p.Invoke(parent, config, input)
	if err != nil {
		return nil, err
	}
	message, err := decodeExternalMessage(output)
	if err != nil {
		return nil, err
	}
	switch message.MessageType {
	case model.PropertyReport, model.EventReport, model.StateChange, model.AlarmReport, model.CommandReply, model.LogReport:
	default:
		return nil, fmt.Errorf("unsupported external parser messageType %q", message.MessageType)
	}
	if message.MessageID == "" {
		message.MessageID = "msg_" + strings.TrimPrefix(raw.MessageID, "raw_")
	}
	message.RawMessageID = raw.MessageID
	message.TenantID, message.ProductID, message.DeviceID = raw.TenantID, raw.ProductID, raw.DeviceID
	if message.Timestamp == 0 {
		message.Timestamp = raw.ReceivedAt
	}
	if message.Properties == nil {
		message.Properties = map[string]any{}
	}
	if message.Event == nil {
		message.Event = map[string]any{}
	}
	if message.Tags == nil {
		message.Tags = map[string]string{}
	}
	return &message, nil
}

// Invoke executes one protocol operation using the release's verified artifact.
// All operations share the same process, timeout, environment and output limits.
func (p ExternalParser) Invoke(parent context.Context, config map[string]any, request any) ([]byte, error) {
	artifact, err := externalArtifact(config)
	if err != nil {
		return nil, err
	}
	path, err := p.artifactPath(artifact)
	if err != nil {
		return nil, err
	}
	if err := verifyExternalArtifact(path, artifact); err != nil {
		return nil, err
	}

	timeout := externalTimeout(config)
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path)
	cmd.WaitDelay = time.Second
	cmd.Dir = filepath.Dir(path)
	cmd.Env = externalEnvironment()

	input, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal external parser input: %w", err)
	}
	input = append(input, '\n')
	cmd.Stdin = bytes.NewReader(input)
	stdout := &boundedBuffer{limit: maxExternalOutput}
	stderr := &boundedBuffer{limit: 64 << 10}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("external parser timed out after %s", timeout)
	}
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("external parser failed: %s", message)
	}
	if stdout.truncated {
		return nil, fmt.Errorf("external parser output exceeds %d bytes", maxExternalOutput)
	}
	return stdout.Bytes(), nil
}

func externalArtifact(config map[string]any) (map[string]any, error) {
	if config == nil {
		return nil, errors.New("go protocol parser artifact configuration is missing")
	}
	artifact, ok := config["artifact"].(map[string]any)
	if !ok || strings.TrimSpace(fmt.Sprint(artifact["path"])) == "" {
		return nil, errors.New("go protocol parser artifact.path is required")
	}
	return artifact, nil
}

func (p ExternalParser) artifactPath(artifact map[string]any) (string, error) {
	rel, ok := artifact["path"].(string)
	if !ok || strings.TrimSpace(rel) == "" {
		return "", errors.New("go protocol parser artifact.path is required")
	}
	rel = filepath.Clean(rel)
	if filepath.IsAbs(rel) || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("go protocol parser artifact.path must stay under the data directory")
	}
	root := p.Root
	if strings.TrimSpace(root) == "" {
		return "", errors.New("go protocol parser data directory is not configured")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve protocol artifact root: %w", err)
	}
	path, err := filepath.Abs(filepath.Join(root, rel))
	if err != nil {
		return "", fmt.Errorf("resolve protocol artifact: %w", err)
	}
	inside, err := filepath.Rel(root, path)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return "", errors.New("go protocol parser artifact.path must stay under the data directory")
	}
	return path, nil
}

func verifyExternalArtifact(path string, artifact map[string]any) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat protocol artifact: %w", err)
	}
	if info.IsDir() {
		return errors.New("protocol artifact must be an executable file")
	}
	if info.Size() <= 0 || info.Size() > maxExternalArtifact {
		return fmt.Errorf("protocol artifact size must be between 1 and %d bytes", maxExternalArtifact)
	}
	if expected, ok := artifact["sha256"].(string); ok && strings.TrimSpace(expected) != "" {
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open protocol artifact: %w", err)
		}
		hash := sha256.New()
		_, copyErr := io.CopyN(hash, file, maxExternalArtifact+1)
		closeErr := file.Close()
		if copyErr != nil && !errors.Is(copyErr, io.EOF) {
			return fmt.Errorf("hash protocol artifact: %w", copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close protocol artifact: %w", closeErr)
		}
		actual := hex.EncodeToString(hash.Sum(nil))
		if !strings.EqualFold(actual, strings.TrimSpace(expected)) {
			return fmt.Errorf("protocol artifact checksum mismatch: got %s, want %s", actual, expected)
		}
	}
	return nil
}

func externalTimeout(config map[string]any) time.Duration {
	if config == nil {
		return defaultExternalTimeout
	}
	value := config["timeoutMs"]
	var milliseconds int64
	switch v := value.(type) {
	case float64:
		milliseconds = int64(v)
	case int:
		milliseconds = int64(v)
	case int64:
		milliseconds = v
	case json.Number:
		milliseconds, _ = strconv.ParseInt(string(v), 10, 64)
	case string:
		milliseconds, _ = strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	}
	if milliseconds <= 0 {
		return defaultExternalTimeout
	}
	if milliseconds > maxExternalTimeout.Milliseconds() {
		milliseconds = maxExternalTimeout.Milliseconds()
	}
	return time.Duration(milliseconds) * time.Millisecond
}

func decodeExternalMessage(output []byte) (model.StandardMessage, error) {
	var envelope struct {
		StandardMessage *model.StandardMessage `json:"standardMessage"`
		Error           string                 `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output), &envelope); err != nil {
		return model.StandardMessage{}, fmt.Errorf("decode external parser output: %w", err)
	}
	if envelope.Error != "" {
		return model.StandardMessage{}, fmt.Errorf("external parser returned error: %s", envelope.Error)
	}
	if envelope.StandardMessage != nil {
		if envelope.StandardMessage.MessageType == "" {
			return model.StandardMessage{}, errors.New("external parser output messageType is required")
		}
		return *envelope.StandardMessage, nil
	}
	var message model.StandardMessage
	if err := json.Unmarshal(bytes.TrimSpace(output), &message); err != nil {
		return model.StandardMessage{}, fmt.Errorf("decode external standard message: %w", err)
	}
	if message.MessageType == "" {
		return model.StandardMessage{}, errors.New("external parser output messageType is required")
	}
	return message, nil
}

func externalEnvironment() []string {
	// Do not pass application secrets or database credentials to tenant code.
	values := []string{"LANG=C"}
	for _, name := range []string{"PATH", "SystemRoot", "WINDIR", "TEMP", "TMP"} {
		if value := os.Getenv(name); value != "" {
			values = append(values, name+"="+value)
		}
	}
	return values
}

type boundedBuffer struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		b.truncated = true
		return 0, errors.New("output limit exceeded")
	}
	if len(p) > remaining {
		_, _ = b.Buffer.Write(p[:remaining])
		b.truncated = true
		return remaining, errors.New("output limit exceeded")
	}
	return b.Buffer.Write(p)
}
