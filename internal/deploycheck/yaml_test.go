package deploycheck /* 声明 deploycheck 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"os"            /* 执行当前语句并推进处理流程。 */
	"path/filepath" /* 执行当前语句并推进处理流程。 */
	"regexp"        /* 执行当前语句并推进处理流程。 */
	"runtime"       /* 执行当前语句并推进处理流程。 */
	"testing"       /* 执行当前语句并推进处理流程。 */

	"gopkg.in/yaml.v3" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestLocalOneShotServicesHaveCompletionDependencies(t *testing.T) { /* 定义 TestLocalOneShotServicesHaveCompletionDependencies 函数。 */
	_, file, _, _ := runtime.Caller(0)                                                               /* 更新 _ 的值。 */
	content, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "compose.local.yaml")) /* 更新 err 的值。 */
	if err != nil {                                                                                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var doc struct { /* 声明 doc。 */
		Services map[string]struct { /* 执行当前语句并推进处理流程。 */
			Restart   string              `yaml:"restart"` /* 执行当前语句并推进处理流程。 */
			DependsOn map[string]struct { /* 执行当前语句并推进处理流程。 */
				Condition string `yaml:"condition"` /* 执行当前语句并推进处理流程。 */
			} `yaml:"depends_on"` /* 结束当前表达式或代码块。 */
		} `yaml:"services"` /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err = yaml.Unmarshal(content, &doc); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	completed := map[string]bool{}         /* 更新 completed 的值。 */
	for _, service := range doc.Services { /* 循环处理当前数据。 */
		for name, dependency := range service.DependsOn { /* 循环处理当前数据。 */
			if dependency.Condition == "service_completed_successfully" { /* 判断条件并选择处理分支。 */
				completed[name] = true /* 更新 completed[name] 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for name, service := range doc.Services { /* 循环处理当前数据。 */
		if service.Restart == "no" && !completed[name] { /* 判断条件并选择处理分支。 */
			t.Errorf("%s needs a completion dependency: Compose --wait otherwise treats its successful exit as a failed service", name) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestDeploymentYAMLParses(t *testing.T) { /* 定义 TestDeploymentYAMLParses 函数。 */
	_, file, _, _ := runtime.Caller(0)                                                                                                                                                                                                                                                                                  /* 更新 _ 的值。 */
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))                                                                                                                                                                                                                                               /* 更新 root 的值。 */
	files := []string{"compose.yaml", "compose.offline.yaml", filepath.Join("deploy", "k8s", "platform.yaml"), filepath.Join("deploy", "k8s", "backup-cronjob.yaml"), filepath.Join("ops", "prometheus", "prometheus.yml"), filepath.Join("ops", "prometheus", "alerts.yml"), filepath.Join("ops", "loki", "loki.yml")} /* 更新 files 的值。 */
	for _, name := range files {                                                                                                                                                                                                                                                                                        /* 循环处理当前数据。 */
		t.Run(name, func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			f, err := os.Open(filepath.Join(root, name)) /* 更新 err 的值。 */
			if err != nil {                              /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			defer f.Close()               /* 安排函数结束时执行清理。 */
			decoder := yaml.NewDecoder(f) /* 更新 decoder 的值。 */
			documents := 0                /* 更新 documents 的值。 */
			for {                         /* 循环处理当前数据。 */
				var doc any                /* 声明 doc。 */
				err = decoder.Decode(&doc) /* 更新 err 的值。 */
				if err == io.EOF {         /* 判断条件并选择处理分支。 */
					break /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
				if err != nil { /* 判断条件并选择处理分支。 */
					t.Fatal(err) /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
				documents++ /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			if documents == 0 { /* 判断条件并选择处理分支。 */
				t.Fatal("no YAML documents") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestProductionComposeDoesNotInjectAuthenticationFallbacks(t *testing.T) { /* 定义 TestProductionComposeDoesNotInjectAuthenticationFallbacks 函数。 */
	_, file, _, _ := runtime.Caller(0)                                    /* 更新 _ 的值。 */
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..")) /* 更新 root 的值。 */
	content, err := os.ReadFile(filepath.Join(root, "compose.yaml"))      /* 更新 err 的值。 */
	if err != nil {                                                       /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	pattern := regexp.MustCompile(`\$\{IOT_(?:JWT_SECRET|ADMIN_PASSWORD):-([^}]*)\}`) /* 更新 pattern 的值。 */
	matches := pattern.FindAllSubmatch(content, -1)                                   /* 更新 matches 的值。 */
	for _, match := range matches {                                                   /* 循环处理当前数据。 */
		if len(match) == 2 && len(match[1]) != 0 { /* 判断条件并选择处理分支。 */
			t.Fatal("production compose injects a fixed authentication fallback") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for _, variable := range [][]byte{[]byte("${IOT_JWT_SECRET:?"), []byte("${IOT_ADMIN_PASSWORD:?")} { /* 循环处理当前数据。 */
		if !bytes.Contains(content, variable) { /* 判断条件并选择处理分支。 */
			t.Fatal("production compose must require authentication variables") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestBackupServiceFollowsComposeLifecycle(t *testing.T) { /* 定义 TestBackupServiceFollowsComposeLifecycle 函数。 */
	_, file, _, _ := runtime.Caller(0)                                    /* 更新 _ 的值。 */
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..")) /* 更新 root 的值。 */
	content, err := os.ReadFile(filepath.Join(root, "compose.yaml"))      /* 更新 err 的值。 */
	if err != nil {                                                       /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	var document struct { /* 声明 document。 */
		Services map[string]struct { /* 执行当前语句并推进处理流程。 */
			Profiles []string `yaml:"profiles"` /* 执行当前语句并推进处理流程。 */
			Restart  string   `yaml:"restart"`  /* 执行当前语句并推进处理流程。 */
		} `yaml:"services"` /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err := yaml.Unmarshal(content, &document); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	backup, ok := document.Services["backup-service"] /* 更新 ok 的值。 */
	if !ok {                                          /* 判断条件并选择处理分支。 */
		t.Fatal("compose.yaml must define backup-service") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(backup.Profiles) != 0 { /* 判断条件并选择处理分支。 */
		t.Fatalf("backup-service must start with the main system, got profiles %v", backup.Profiles) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if backup.Restart != "unless-stopped" { /* 判断条件并选择处理分支。 */
		t.Fatalf("backup-service must follow the main system restart policy, got %q", backup.Restart) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestLocalComposeKeepsBackupServiceInExplicitProfile(t *testing.T) { /* 定义 TestLocalComposeKeepsBackupServiceInExplicitProfile 函数。 */
	_, file, _, _ := runtime.Caller(0)                                     /* 更新 _ 的值。 */
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))  /* 更新 root 的值。 */
	content, err := os.ReadFile(filepath.Join(root, "compose.local.yaml")) /* 更新 err 的值。 */
	if err != nil {                                                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var document struct { /* 声明 document。 */
		Services map[string]struct { /* 执行当前语句并推进处理流程。 */
			Profiles []string `yaml:"profiles"` /* 执行当前语句并推进处理流程。 */
		} `yaml:"services"` /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err := yaml.Unmarshal(content, &document); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	backup, ok := document.Services["backup-service"] /* 更新 ok 的值。 */
	if !ok {                                          /* 判断条件并选择处理分支。 */
		t.Fatal("compose.local.yaml must define backup-service") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(backup.Profiles) != 1 || backup.Profiles[0] != "backup" { /* 判断条件并选择处理分支。 */
		t.Fatalf("local backup-service must require the backup profile, got %v", backup.Profiles) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
