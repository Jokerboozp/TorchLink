package model /* 声明 model 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"errors"  /* 执行当前语句并推进处理流程。 */
	"strings" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func ProtocolPlatforms() []string { /* 定义 ProtocolPlatforms 函数。 */
	return []string{"linux-amd64", "linux-arm64", "windows-amd64", "windows-arm64", "darwin-amd64", "darwin-arm64"} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// Accept the historical slash notation used by early Edge configurations.
func ProtocolPlatform(value string) string { /* 定义 ProtocolPlatform 函数。 */
	value = strings.ReplaceAll(value, "/", "-")     /* 更新 value 的值。 */
	for _, candidate := range ProtocolPlatforms() { /* 循环处理当前数据。 */
		if value == candidate { /* 判断条件并选择处理分支。 */
			return value /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return "" /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// SelectProtocolArtifact returns a copy; version metadata remains immutable.
func SelectProtocolArtifact(artifact map[string]any, platform string) (map[string]any, error) { /* 定义 SelectProtocolArtifact 函数。 */
	platform = ProtocolPlatform(platform)      /* 更新 platform 的值。 */
	native, _ := artifact["platform"].(string) /* 更新 _ 的值。 */
	if platform == "" {                        /* 判断条件并选择处理分支。 */
		return nil, errors.New("unsupported worker platform") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	out := map[string]any{}            /* 更新 out 的值。 */
	for key, value := range artifact { /* 循环处理当前数据。 */
		out[key] = value /* 更新 out[key] 的值。 */
	} /* 结束当前表达式或代码块。 */
	if ProtocolPlatform(native) != platform { /* 判断条件并选择处理分支。 */
		variants, _ := artifact["variants"].(map[string]any) /* 更新 _ 的值。 */
		variant, ok := variants[platform].(map[string]any)   /* 更新 ok 的值。 */
		if !ok {                                             /* 判断条件并选择处理分支。 */
			return nil, errors.New("published worker has no artifact for this node platform") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for _, key := range []string{"path", "sha256", "size", "platform", "validation", "testCases"} { /* 循环处理当前数据。 */
			out[key] = variant[key] /* 更新 out[key] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	out["platform"] = platform /* 执行当前语句并推进处理流程。 */
	return out, nil            /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
