// The root package accepts both legacy RawMessage input and protocol v2 calls.
package main /* 声明 main 包。 */

import ( /* 引入当前代码需要的依赖。 */
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

func main() { /* 定义 main 函数。 */
	if err := run(os.Stdin, os.Stdout); err != nil { /* 判断条件并选择处理分支。 */
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"error": err.Error()}) /* 更新 _ 的值。 */
		fmt.Fprintln(os.Stderr, err)                                                   /* 执行当前语句并推进处理流程。 */
		os.Exit(1)                                                                     /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
