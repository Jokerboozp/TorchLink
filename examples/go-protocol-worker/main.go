// Command go-protocol-worker is a minimal example for the uploaded Go
// protocol-package contract. Replace the payload conversion with the vendor
// protocol implementation and keep the one-request/one-response JSON shape.
package main /* 声明 main 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bufio"         /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"os"            /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type rawMessage struct { /* 定义 rawMessage 类型。 */
	Payload       json.RawMessage `json:"payload"`       /* 执行当前语句并推进处理流程。 */
	PayloadFormat string          `json:"payloadFormat"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func main() { /* 定义 main 函数。 */
	scanner := bufio.NewScanner(os.Stdin)     /* 更新 scanner 的值。 */
	scanner.Buffer(make([]byte, 1024), 2<<20) /* 执行当前语句并推进处理流程。 */
	if !scanner.Scan() {                      /* 判断条件并选择处理分支。 */
		writeError("a go-protocol-v2 request is required") /* 执行当前语句并推进处理流程。 */
		return                                             /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var request struct { /* 声明 request。 */
		Version   int        `json:"version"`   /* 执行当前语句并推进处理流程。 */
		Operation string     `json:"operation"` /* 执行当前语句并推进处理流程。 */
		Raw       rawMessage `json:"raw"`       /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err := json.Unmarshal(scanner.Bytes(), &request); err != nil { /* 判断条件并选择处理分支。 */
		writeError("invalid request: " + err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if request.Version != 2 || request.Operation != "decode" { /* 判断条件并选择处理分支。 */
		writeError("expected go-protocol-v2 decode request") /* 执行当前语句并推进处理流程。 */
		return                                               /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	raw := request.Raw                               /* 更新 raw 的值。 */
	properties := map[string]any{}                   /* 更新 properties 的值。 */
	if strings.EqualFold(raw.PayloadFormat, "hex") { /* 判断条件并选择处理分支。 */
		text := strings.Trim(strings.TrimSpace(string(raw.Payload)), `"`)                                       /* 更新 text 的值。 */
		data, err := hex.DecodeString(strings.NewReplacer(" ", "", "\r", "", "\n", "", "\t", "").Replace(text)) /* 更新 err 的值。 */
		if err != nil {                                                                                         /* 判断条件并选择处理分支。 */
			writeError("invalid hex payload: " + err.Error()) /* 执行当前语句并推进处理流程。 */
			return                                            /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if len(data) > 0 { /* 判断条件并选择处理分支。 */
			properties["firstByte"] = int(data[0]) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if len(data) > 1 { /* 判断条件并选择处理分支。 */
			properties["secondByte"] = int(data[1]) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		// The protocol-v2 package example uses AA 01 2A, where byte 3 is
		// the temperature value. Replace this with the vendor protocol's
		// real frame validation and field decoding rules.
		if len(data) > 2 { /* 判断条件并选择处理分支。 */
			properties["temperature"] = int(data[2]) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} else { /* 结束当前表达式或代码块。 */
		var body any                                               /* 声明 body。 */
		if err := json.Unmarshal(raw.Payload, &body); err != nil { /* 判断条件并选择处理分支。 */
			writeError("invalid JSON payload: " + err.Error()) /* 执行当前语句并推进处理流程。 */
			return                                             /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		properties["body"] = body /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"standardMessage": map[string]any{ /* 更新 _ 的值。 */
		"messageType": "PROPERTY_REPORT",                         /* 执行当前语句并推进处理流程。 */
		"properties":  properties,                                /* 执行当前语句并推进处理流程。 */
		"tags":        map[string]string{"worker": "go-example"}, /* 执行当前语句并推进处理流程。 */
	}}) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func writeError(message string) { /* 定义 writeError 函数。 */
	_, _ = fmt.Fprintf(os.Stdout, `{"error":%q}`+"\n", message) /* 更新 _ 的值。 */
} /* 结束当前表达式或代码块。 */
