package parser /* 声明 parser 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"crypto/sha256" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"os"            /* 执行当前语句并推进处理流程。 */
	"os/exec"       /* 执行当前语句并推进处理流程。 */
	"path/filepath" /* 执行当前语句并推进处理流程。 */
	"strconv"       /* 执行当前语句并推进处理流程。 */
	"strings"
	"sync" /* 执行当前语句并推进处理流程。 */
	"time" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// GoProtocolParserName is the runtime adapter for a compiled Go protocol
// package. The package is an executable that implements the line-oriented JSON
// contract documented in docs/GO_PROTOCOL_PACKAGES.md. It is intentionally
// executed out of process so a parser cannot corrupt the API process or block
// the parser goroutine forever.
const GoProtocolParserName = "go_protocol_parser" /* 声明 GoProtocolParserName。 */

const GoProtocolParserVersion = "1.0.0" /* 声明 GoProtocolParserVersion。 */

const ( /* 执行当前语句并推进处理流程。 */
	defaultExternalTimeout = 5 * time.Second  /* 更新 defaultExternalTimeout 的值。 */
	maxExternalTimeout     = 10 * time.Second /* 更新 maxExternalTimeout 的值。 */
	maxExternalArtifact    = 64 << 20         /* 更新 maxExternalArtifact 的值。 */
	maxExternalOutput      = 1 << 20          /* 更新 maxExternalOutput 的值。 */
) /* 结束当前表达式或代码块。 */

// ExternalParser runs a tenant-provided compiled protocol worker. Root is the
// platform data directory; artifact paths are always relative to it.
type ExternalParser struct { /* 定义 ExternalParser 类型。 */
	Root string /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (ExternalParser) Name() string    { return GoProtocolParserName }    /* 定义 Name 函数。 */
func (ExternalParser) Version() string { return GoProtocolParserVersion } /* 定义 Version 函数。 */
func (ExternalParser) Match(Meta) bool { return false }                   /* 定义 Match 函数。 */

func (p ExternalParser) Parse(raw model.RawMessage) (*model.StandardMessage, error) { /* 定义 Parse 函数。 */
	return nil, errors.New("go protocol parser requires an uploaded artifact") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (p ExternalParser) ParseWithConfig(raw model.RawMessage, config map[string]any) (*model.StandardMessage, error) { /* 定义 ParseWithConfig 函数。 */
	return p.ParseWithContext(context.Background(), raw, config) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// ParseWithContext lets publication cancel a sample worker when its request or
// the overall validation budget expires.
func (p ExternalParser) ParseWithContext(parent context.Context, raw model.RawMessage, config map[string]any) (*model.StandardMessage, error) { /* 定义 ParseWithContext 函数。 */
	artifact, err := externalArtifact(config) /* 更新 err 的值。 */
	if err != nil {                           /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if artifact["runtime"] != "go-protocol-v2" { /* 判断条件并选择处理分支。 */
		return nil, errors.New("protocol runtime must be go-protocol-v2") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	input := map[string]any{"version": 2, "operation": "decode", "raw": raw, "state": raw.Metadata["protocolState"], "now": raw.ReceivedAt} /* 更新 input 的值。 */
	output, err := p.Invoke(parent, config, input)                                                                                          /* 更新 err 的值。 */
	if err != nil {                                                                                                                         /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	message, err := decodeExternalMessage(output) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	switch message.MessageType { /* 根据条件选择处理路径。 */
	case model.PropertyReport, model.EventReport, model.StateChange, model.AlarmReport, model.CommandReply, model.LogReport: /* 处理当前分支。 */
	default: /* 处理当前分支。 */
		return nil, fmt.Errorf("unsupported external parser messageType %q", message.MessageType) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if message.MessageID == "" { /* 判断条件并选择处理分支。 */
		message.MessageID = "msg_" + strings.TrimPrefix(raw.MessageID, "raw_") /* 更新 message.MessageID 的值。 */
	} /* 结束当前表达式或代码块。 */
	message.RawMessageID = raw.MessageID                                                              /* 更新 message.RawMessageID 的值。 */
	message.TenantID, message.ProductID, message.DeviceID = raw.TenantID, raw.ProductID, raw.DeviceID /* 更新 message.DeviceID 的值。 */
	if message.Timestamp == 0 {                                                                       /* 判断条件并选择处理分支。 */
		message.Timestamp = raw.ReceivedAt /* 更新 message.Timestamp 的值。 */
	} /* 结束当前表达式或代码块。 */
	if message.Properties == nil { /* 判断条件并选择处理分支。 */
		message.Properties = map[string]any{} /* 更新 message.Properties 的值。 */
	} /* 结束当前表达式或代码块。 */
	if message.Event == nil { /* 判断条件并选择处理分支。 */
		message.Event = map[string]any{} /* 更新 message.Event 的值。 */
	} /* 结束当前表达式或代码块。 */
	if message.Tags == nil { /* 判断条件并选择处理分支。 */
		message.Tags = map[string]string{} /* 更新 message.Tags 的值。 */
	} /* 结束当前表达式或代码块。 */
	if _, err := model.MessageComponents(message); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return &message, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// Invoke executes one protocol operation using the release's verified artifact.
// All operations share the same process, timeout, environment and output limits.
func (p ExternalParser) Invoke(parent context.Context, config map[string]any, request any) ([]byte, error) { /* 定义 Invoke 函数。 */
	artifact, err := externalArtifact(config) /* 更新 err 的值。 */
	if err != nil {                           /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	path, err := p.artifactPath(artifact) /* 更新 err 的值。 */
	if err != nil {                       /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := verifyExternalArtifact(path, artifact); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */

	timeout := externalTimeout(config)                  /* 更新 timeout 的值。 */
	ctx, cancel := context.WithTimeout(parent, timeout) /* 更新 cancel 的值。 */
	defer cancel()                                      /* 安排函数结束时执行清理。 */
	cmd := exec.CommandContext(ctx, path)               /* 更新 cmd 的值。 */
	cmd.WaitDelay = time.Second                         /* 更新 cmd.WaitDelay 的值。 */
	cmd.Dir = filepath.Dir(path)                        /* 更新 cmd.Dir 的值。 */
	cmd.Env = externalEnvironment()                     /* 更新 cmd.Env 的值。 */

	input, err := json.Marshal(request) /* 更新 err 的值。 */
	if err != nil {                     /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("marshal external parser input: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	input = append(input, '\n')                        /* 更新 input 的值。 */
	cmd.Stdin = bytes.NewReader(input)                 /* 更新 cmd.Stdin 的值。 */
	stdout := &boundedBuffer{limit: maxExternalOutput} /* 更新 stdout 的值。 */
	stderr := &boundedBuffer{limit: 64 << 10}          /* 更新 stderr 的值。 */
	cmd.Stdout = stdout                                /* 更新 cmd.Stdout 的值。 */
	cmd.Stderr = stderr                                /* 更新 cmd.Stderr 的值。 */
	err = cmd.Run()                                    /* 更新 err 的值。 */
	if ctx.Err() == context.DeadlineExceeded {         /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("external parser timed out after %s", timeout) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		message := strings.TrimSpace(stderr.String()) /* 更新 message 的值。 */
		if message == "" {                            /* 判断条件并选择处理分支。 */
			message = err.Error() /* 更新 message 的值。 */
		} /* 结束当前表达式或代码块。 */
		return nil, fmt.Errorf("external parser failed: %s", message) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if stdout.truncated { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("external parser output exceeds %d bytes", maxExternalOutput) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return stdout.Bytes(), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func externalArtifact(config map[string]any) (map[string]any, error) { /* 定义 externalArtifact 函数。 */
	if config == nil { /* 判断条件并选择处理分支。 */
		return nil, errors.New("go protocol parser artifact configuration is missing") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	artifact, ok := config["artifact"].(map[string]any)               /* 更新 ok 的值。 */
	if !ok || strings.TrimSpace(fmt.Sprint(artifact["path"])) == "" { /* 判断条件并选择处理分支。 */
		return nil, errors.New("go protocol parser artifact.path is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return artifact, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (p ExternalParser) artifactPath(artifact map[string]any) (string, error) { /* 定义 artifactPath 函数。 */
	rel, ok := artifact["path"].(string)     /* 更新 ok 的值。 */
	if !ok || strings.TrimSpace(rel) == "" { /* 判断条件并选择处理分支。 */
		return "", errors.New("go protocol parser artifact.path is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rel = filepath.Clean(rel)                                                                                        /* 更新 rel 的值。 */
	if filepath.IsAbs(rel) || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) { /* 判断条件并选择处理分支。 */
		return "", errors.New("go protocol parser artifact.path must stay under the data directory") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	root := p.Root                     /* 更新 root 的值。 */
	if strings.TrimSpace(root) == "" { /* 判断条件并选择处理分支。 */
		return "", errors.New("go protocol parser data directory is not configured") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	root, err := filepath.Abs(root) /* 更新 err 的值。 */
	if err != nil {                 /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("resolve protocol artifact root: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	path, err := filepath.Abs(filepath.Join(root, rel)) /* 更新 err 的值。 */
	if err != nil {                                     /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("resolve protocol artifact: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	inside, err := filepath.Rel(root, path)                                                         /* 更新 err 的值。 */
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) { /* 判断条件并选择处理分支。 */
		return "", errors.New("go protocol parser artifact.path must stay under the data directory") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return path, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func verifyExternalArtifact(path string, artifact map[string]any) error { /* 定义 verifyExternalArtifact 函数。 */
	info, err := os.Stat(path) /* 更新 err 的值。 */
	if err != nil {            /* 判断条件并选择处理分支。 */
		return fmt.Errorf("stat protocol artifact: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if info.IsDir() { /* 判断条件并选择处理分支。 */
		return errors.New("protocol artifact must be an executable file") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if info.Size() <= 0 || info.Size() > maxExternalArtifact { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("protocol artifact size must be between 1 and %d bytes", maxExternalArtifact) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if expected, ok := artifact["sha256"].(string); ok && strings.TrimSpace(expected) != "" { /* 判断条件并选择处理分支。 */
		if verifiedArtifacts.fresh(path, expected, info) {
			return nil
		}
		file, err := os.Open(path) /* 更新 err 的值。 */
		if err != nil {            /* 判断条件并选择处理分支。 */
			return fmt.Errorf("open protocol artifact: %w", err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		hash := sha256.New()                                      /* 更新 hash 的值。 */
		_, copyErr := io.CopyN(hash, file, maxExternalArtifact+1) /* 更新 copyErr 的值。 */
		closeErr := file.Close()                                  /* 更新 closeErr 的值。 */
		if copyErr != nil && !errors.Is(copyErr, io.EOF) {        /* 判断条件并选择处理分支。 */
			return fmt.Errorf("hash protocol artifact: %w", copyErr) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if closeErr != nil { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("close protocol artifact: %w", closeErr) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		actual := hex.EncodeToString(hash.Sum(nil))                  /* 更新 actual 的值。 */
		if !strings.EqualFold(actual, strings.TrimSpace(expected)) { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("protocol artifact checksum mismatch: got %s, want %s", actual, expected) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		verifiedArtifacts.remember(path, expected, info)
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// artifactVerificationTTL bounds how long a verified hash is trusted without
// re-reading the file, even when size and modification time are unchanged.
const artifactVerificationTTL = 5 * time.Minute

// artifactVerifications remembers binaries whose SHA-256 already matched, so
// every protocol call does not re-read and hash a multi-megabyte Worker. Any
// change of expected hash, size or modification time forces a full re-check.
type artifactVerifications struct {
	mu      sync.Mutex
	entries map[string]artifactVerification
}

type artifactVerification struct {
	sha256     string
	size       int64
	modTime    time.Time
	verifiedAt time.Time
}

var verifiedArtifacts = &artifactVerifications{entries: map[string]artifactVerification{}}

func (v *artifactVerifications) fresh(path, expected string, info os.FileInfo) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	entry, ok := v.entries[path]
	return ok && strings.EqualFold(entry.sha256, strings.TrimSpace(expected)) && entry.size == info.Size() && entry.modTime.Equal(info.ModTime()) && time.Since(entry.verifiedAt) < artifactVerificationTTL
}

func (v *artifactVerifications) remember(path, expected string, info os.FileInfo) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.entries) >= 1024 {
		v.entries = map[string]artifactVerification{}
	}
	v.entries[path] = artifactVerification{sha256: strings.TrimSpace(expected), size: info.Size(), modTime: info.ModTime(), verifiedAt: time.Now()}
}

func externalTimeout(config map[string]any) time.Duration { /* 定义 externalTimeout 函数。 */
	if config == nil { /* 判断条件并选择处理分支。 */
		return defaultExternalTimeout /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	value := config["timeoutMs"] /* 更新 value 的值。 */
	var milliseconds int64       /* 声明 milliseconds。 */
	switch v := value.(type) {   /* 根据条件选择处理路径。 */
	case float64: /* 处理当前分支。 */
		milliseconds = int64(v) /* 更新 milliseconds 的值。 */
	case int: /* 处理当前分支。 */
		milliseconds = int64(v) /* 更新 milliseconds 的值。 */
	case int64: /* 处理当前分支。 */
		milliseconds = v /* 更新 milliseconds 的值。 */
	case json.Number: /* 处理当前分支。 */
		milliseconds, _ = strconv.ParseInt(string(v), 10, 64) /* 更新 _ 的值。 */
	case string: /* 处理当前分支。 */
		milliseconds, _ = strconv.ParseInt(strings.TrimSpace(v), 10, 64) /* 更新 _ 的值。 */
	} /* 结束当前表达式或代码块。 */
	if milliseconds <= 0 { /* 判断条件并选择处理分支。 */
		return defaultExternalTimeout /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if milliseconds > maxExternalTimeout.Milliseconds() { /* 判断条件并选择处理分支。 */
		milliseconds = maxExternalTimeout.Milliseconds() /* 更新 milliseconds 的值。 */
	} /* 结束当前表达式或代码块。 */
	return time.Duration(milliseconds) * time.Millisecond /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func decodeExternalMessage(output []byte) (model.StandardMessage, error) { /* 定义 decodeExternalMessage 函数。 */
	var envelope struct { /* 声明 envelope。 */
		StandardMessage *model.StandardMessage `json:"standardMessage"` /* 执行当前语句并推进处理流程。 */
		Error           string                 `json:"error"`           /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err := json.Unmarshal(bytes.TrimSpace(output), &envelope); err != nil { /* 判断条件并选择处理分支。 */
		return model.StandardMessage{}, fmt.Errorf("decode external parser output: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if envelope.Error != "" { /* 判断条件并选择处理分支。 */
		return model.StandardMessage{}, fmt.Errorf("external parser returned error: %s", envelope.Error) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if envelope.StandardMessage != nil { /* 判断条件并选择处理分支。 */
		if envelope.StandardMessage.MessageType == "" { /* 判断条件并选择处理分支。 */
			return model.StandardMessage{}, errors.New("external parser output messageType is required") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return *envelope.StandardMessage, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return model.StandardMessage{}, errors.New("external parser output standardMessage is required") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func externalEnvironment() []string { /* 定义 externalEnvironment 函数。 */
	// Do not pass application secrets or database credentials to tenant code.
	values := []string{"LANG=C"}                                                   /* 更新 values 的值。 */
	for _, name := range []string{"PATH", "SystemRoot", "WINDIR", "TEMP", "TMP"} { /* 循环处理当前数据。 */
		if value := os.Getenv(name); value != "" { /* 判断条件并选择处理分支。 */
			values = append(values, name+"="+value) /* 更新 values 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return values /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type boundedBuffer struct { /* 定义 boundedBuffer 类型。 */
	bytes.Buffer      /* 执行当前语句并推进处理流程。 */
	limit        int  /* 执行当前语句并推进处理流程。 */
	truncated    bool /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (b *boundedBuffer) Write(p []byte) (int, error) { /* 定义 Write 函数。 */
	remaining := b.limit - b.Len() /* 更新 remaining 的值。 */
	if remaining <= 0 {            /* 判断条件并选择处理分支。 */
		b.truncated = true                            /* 更新 b.truncated 的值。 */
		return 0, errors.New("output limit exceeded") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(p) > remaining { /* 判断条件并选择处理分支。 */
		_, _ = b.Buffer.Write(p[:remaining])                  /* 更新 _ 的值。 */
		b.truncated = true                                    /* 更新 b.truncated 的值。 */
		return remaining, errors.New("output limit exceeded") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return b.Buffer.Write(p) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
