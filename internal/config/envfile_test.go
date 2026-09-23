package config /* 声明 config 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"os"            /* 执行当前语句并推进处理流程。 */
	"path/filepath" /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"testing"       /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestLoadEnvFileLocalConfiguration(t *testing.T) { /* 定义 TestLoadEnvFileLocalConfiguration 函数。 */
	for _, key := range []string{"IOT_TEST_DSN", "IOT_TEST_LITERAL", "IOT_TEST_EMPTY", "IOT_TEST_OVERRIDE", "IOT_TEST_LAST"} { /* 循环处理当前数据。 */
		t.Setenv(key, "")                        /* 执行当前语句并推进处理流程。 */
		if err := os.Unsetenv(key); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	t.Setenv("IOT_TEST_OVERRIDE", "from-process")                                                                   /* 执行当前语句并推进处理流程。 */
	t.Setenv("IOT_TEST_EMPTY", "")                                                                                  /* 执行当前语句并推进处理流程。 */
	path := filepath.Join(t.TempDir(), "local.env")                                                                 /* 更新 path 的值。 */
	contents := "\ufeff# local config\r\nIOT_TEST_DSN=postgres://iot:abc@127.0.0.1:15432/iot?sslmode=disable\r\n" + /* 更新 contents 的值。 */
		"IOT_TEST_LITERAL='a$HOME#b=c' # literal secret\nIOT_TEST_EMPTY=file-value\nIOT_TEST_OVERRIDE=file-value\n" + /* 执行当前语句并推进处理流程。 */
		"IOT_TEST_LAST=old\nIOT_TEST_LAST=\"new value\"\n" /* 执行当前语句并推进处理流程。 */
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := LoadEnvFile(path); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for key, expected := range map[string]string{ /* 循环处理当前数据。 */
		"IOT_TEST_DSN":     "postgres://iot:abc@127.0.0.1:15432/iot?sslmode=disable",                                              /* 执行当前语句并推进处理流程。 */
		"IOT_TEST_LITERAL": "a$HOME#b=c", "IOT_TEST_EMPTY": "", "IOT_TEST_OVERRIDE": "from-process", "IOT_TEST_LAST": "new value", /* 执行当前语句并推进处理流程。 */
	} { /* 结束当前表达式或代码块。 */
		if os.Getenv(key) != expected { /* 判断条件并选择处理分支。 */
			t.Errorf("unexpected value for %s", key) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestLoadEnvFileRejectsInvalidInputWithoutLeakingValues(t *testing.T) { /* 定义 TestLoadEnvFileRejectsInvalidInputWithoutLeakingValues 函数。 */
	for _, invalid := range []string{"bad line secret-value", "9KEY=secret-value", "IOT_TEST_BAD='secret-value", "IOT_TEST_BAD=\"secret-value\" trailing", "IOT_TEST_BAD=secret-value\x00"} { /* 循环处理当前数据。 */
		t.Run(invalid[:4], func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			t.Setenv("IOT_TEST_ATOMIC", "")                        /* 执行当前语句并推进处理流程。 */
			if err := os.Unsetenv("IOT_TEST_ATOMIC"); err != nil { /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			path := filepath.Join(t.TempDir(), "bad.env")                                                       /* 更新 path 的值。 */
			if err := os.WriteFile(path, []byte("IOT_TEST_ATOMIC=must-not-load\n"+invalid), 0600); err != nil { /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			err := LoadEnvFile(path)                                    /* 更新 err 的值。 */
			if err == nil || !strings.Contains(err.Error(), "line 2") { /* 判断条件并选择处理分支。 */
				t.Fatalf("expected line error, got %v", err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if strings.Contains(err.Error(), "secret-value") { /* 判断条件并选择处理分支。 */
				t.Fatal("error leaked a secret") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if _, exists := os.LookupEnv("IOT_TEST_ATOMIC"); exists { /* 判断条件并选择处理分支。 */
				t.Fatal("invalid file was partially applied") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestLoadEnvFileRequiresExistingFile(t *testing.T) { /* 定义 TestLoadEnvFileRequiresExistingFile 函数。 */
	if err := LoadEnvFile(filepath.Join(t.TempDir(), "missing.env")); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("missing file accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
