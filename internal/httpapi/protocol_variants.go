package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"crypto/sha256" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"os"            /* 执行当前语句并推进处理流程。 */
	"path/filepath" /* 执行当前语句并推进处理流程。 */
	"runtime"       /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolworker" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Store only new files belonging to this attempted immutable release. Native
// validation has already run; foreign binaries are explicitly untested here.
func storeProtocolVariants(root, directory string, artifact map[string]any, entries map[string][]byte, manifest protocolPackageManifestV2) (created []string, err error) { /* 定义 storeProtocolVariants 函数。 */
	defer func() { /* 安排函数结束时执行清理。 */
		if err != nil { /* 判断条件并选择处理分支。 */
			for _, path := range created { /* 循环处理当前数据。 */
				_ = os.Remove(path) /* 更新 _ 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	}() /* 结束当前表达式或代码块。 */
	if len(manifest.Entrypoints) > 6 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("at most six protocol platforms are supported") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	native := runtime.GOOS + "-" + runtime.GOARCH     /* 更新 native 的值。 */
	variants := map[string]any{}                      /* 更新 variants 的值。 */
	var total int                                     /* 声明 total。 */
	for target, entry := range manifest.Entrypoints { /* 循环处理当前数据。 */
		platform := model.ProtocolPlatform(target) /* 更新 platform 的值。 */
		if platform == "" || platform != target {  /* 判断条件并选择处理分支。 */
			return created, errors.New("unsupported protocol artifact platform") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		data := entries[entry]                                       /* 更新 data 的值。 */
		total += len(data)                                           /* 更新 total 的值。 */
		if len(data) == 0 || len(data) > 64<<20 || total > 128<<20 { /* 判断条件并选择处理分支。 */
			return created, errors.New("protocol artifact exceeds per-platform or total size limits") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if platform == native { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		name := "artifact-" + platform                       /* 更新 name 的值。 */
		if len(platform) > 8 && platform[:8] == "windows-" { /* 判断条件并选择处理分支。 */
			name += ".exe" /* 更新 name 的值。 */
		} /* 结束当前表达式或代码块。 */
		path := filepath.Join(directory, name)                      /* 更新 path 的值。 */
		if err = writeExclusiveFile(path, data, 0700); err != nil { /* 判断条件并选择处理分支。 */
			return created, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		created = append(created, path)                                                          /* 更新 created 的值。 */
		relative, _ := filepath.Rel(root, path)                                                  /* 更新 _ 的值。 */
		digest := sha256.Sum256(data)                                                            /* 更新 digest 的值。 */
		validation := "UNTESTED"                                                                 /* 更新 validation 的值。 */
		if build, ok := artifact["build"].(map[string]any); ok && build["kind"] == "go-source" { /* 判断条件并选择处理分支。 */
			validation = "COMPILED" /* 更新 validation 的值。 */
		} /* 结束当前表达式或代码块。 */
		variants[platform] = map[string]any{"platform": platform, "path": filepath.ToSlash(relative), "sha256": hex.EncodeToString(digest[:]), "size": len(data), "validation": validation, "testCases": 0} /* 更新 variants[platform] 的值。 */
	} /* 结束当前表达式或代码块。 */
	bundle := protocolworker.SampleBundle{Cases: entries["samples/cases.json"], Operations: entries["samples/operations.json"]} /* 更新 bundle 的值。 */
	data, err := json.Marshal(bundle)                                                                                           /* 更新 err 的值。 */
	if err != nil || len(data) > (2<<20)+1024 {                                                                                 /* 判断条件并选择处理分支。 */
		return created, errors.New("invalid protocol sample bundle") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	path := filepath.Join(directory, "samples.json")            /* 更新 path 的值。 */
	if err = writeExclusiveFile(path, data, 0600); err != nil { /* 判断条件并选择处理分支。 */
		return created, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	created = append(created, path)                                                                                /* 更新 created 的值。 */
	relative, _ := filepath.Rel(root, path)                                                                        /* 更新 _ 的值。 */
	digest := sha256.Sum256(data)                                                                                  /* 更新 digest 的值。 */
	artifact["variants"], artifact["validation"] = variants, "PASSED"                                              /* 执行当前语句并推进处理流程。 */
	artifact["samplesPath"], artifact["samplesSha256"] = filepath.ToSlash(relative), hex.EncodeToString(digest[:]) /* 执行当前语句并推进处理流程。 */
	return created, nil                                                                                            /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
