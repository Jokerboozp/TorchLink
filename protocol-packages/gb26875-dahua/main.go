// The root package accepts both legacy RawMessage input and protocol v2 calls.
package main /* 声明 main 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bufio"
	"bytes"
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"os"            /* 执行当前语句并推进处理流程。 */

	"gb26875-dahua/gb26875" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func run(input io.Reader, output io.Writer) error { /* 定义 run 函数。 */
	var encoded json.RawMessage                                     /* 声明 encoded。 */
	if err := json.NewDecoder(input).Decode(&encoded); err != nil { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("decode worker input: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var request gb26875.Request                               /* 声明 request。 */
	if err := json.Unmarshal(encoded, &request); err != nil { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("decode worker request: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if request.Version == 0 && request.Operation == "" { /* 判断条件并选择处理分支。 */
		var raw gb26875.RawMessage                            /* 声明 raw。 */
		if err := json.Unmarshal(encoded, &raw); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		request = gb26875.Request{Version: 2, Operation: "decode", Raw: &raw} /* 更新 request 的值。 */
	} /* 结束当前表达式或代码块。 */
	return json.NewEncoder(output).Encode(gb26875.Handle(request)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// serve handles newline-delimited requests from one resident process and echoes
// each requestId; the platform enables it with IOT_PROTOCOL_WORKER_MODE=serve.
func serve(input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		var id struct {
			RequestID string `json:"requestId"`
		}
		_ = json.Unmarshal(scanner.Bytes(), &id)
		var response bytes.Buffer
		if err := run(bytes.NewReader(scanner.Bytes()), &response); err != nil {
			response.Reset()
			_ = json.NewEncoder(&response).Encode(map[string]string{"error": err.Error()})
		}
		// Prepend requestId without re-encoding, so numbers keep their exact form.
		body := bytes.TrimSpace(response.Bytes())
		requestID, _ := json.Marshal(id.RequestID)
		line := append([]byte(`{"requestId":`), requestID...)
		if len(body) > 2 {
			line = append(line, ',')
		}
		line = append(append(line, body[1:]...), '\n')
		if _, err := output.Write(line); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func main() { /* 定义 main 函数。 */
	if os.Getenv("IOT_PROTOCOL_WORKER_MODE") == "serve" {
		if err := serve(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(os.Stdin, os.Stdout); err != nil { /* 判断条件并选择处理分支。 */
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"error": err.Error()}) /* 更新 _ 的值。 */
		fmt.Fprintln(os.Stderr, err)                                                   /* 执行当前语句并推进处理流程。 */
		os.Exit(1)                                                                     /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
