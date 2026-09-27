// cluster-render validates a cluster inventory and writes one Compose project
// per node plus shared configuration. It never contacts the nodes; deploy
// with scripts/cluster-deploy.sh (or .ps1).
//
//	go run ./cmd/cluster-render -inventory deploy/cluster/inventory.example.yaml -check
//	go run ./cmd/cluster-render -inventory <清单> -secrets <0600 秘密文件> -out dist/cluster
//	go run ./cmd/cluster-render -inventory <清单> -secrets <文件> -init-secrets   # 缺失的秘密随机生成
//	go run ./cmd/cluster-render -inventory <清单> -print-images                   # 每行“键 镜像”
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"iot-platform/internal/clusterplan"
)

func main() {
	inventory := flag.String("inventory", "", "集群清单 YAML")
	secretsPath := flag.String("secrets", "", "私有秘密文件（0600）")
	out := flag.String("out", "", "输出目录（默认 dist/cluster/<清单名>）")
	check := flag.Bool("check", false, "只校验清单（故障域、端口、连接预算），不需要秘密文件")
	initSecrets := flag.Bool("init-secrets", false, "秘密文件不存在或有缺项时随机生成（已有值不变），再渲染")
	printImages := flag.Bool("print-images", false, "按“键 镜像”逐行输出清单中的镜像后退出")
	noModeCheck := flag.Bool("no-mode-check", false, "不检查秘密文件的 POSIX 权限（Windows 绑定挂载显示为 0777，由脚本改用 ACL 保护）")
	flag.Parse()
	clusterplan.CheckSecretsMode = !*noModeCheck
	if *printImages {
		if err := listImages(*inventory); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}
	if *initSecrets {
		generated, err := clusterplan.EnsureSecrets(*secretsPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		if len(generated) > 0 {
			fmt.Printf("generated secrets in %s: %s (back this file up)\n", *secretsPath, strings.Join(generated, ", "))
		}
	}
	if err := run(*inventory, *secretsPath, *out, *check); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func listImages(inventory string) error {
	inv, err := clusterplan.Load(inventory)
	if err != nil {
		return err
	}
	v := reflect.ValueOf(inv.Images)
	for i := 0; i < v.NumField(); i++ {
		if img := v.Field(i).String(); img != "" {
			key := strings.Split(v.Type().Field(i).Tag.Get("yaml"), ",")[0]
			fmt.Printf("%s %s\n", key, img)
		}
	}
	return nil
}

func run(inventory, secretsPath, out string, check bool) error {
	inv, err := clusterplan.Load(inventory)
	if err != nil {
		return err
	}
	if check {
		budget, err := inv.Validate()
		if err != nil {
			return fmt.Errorf("inventory is invalid:\n%w", err)
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"valid": true, "placement": inv.Placement(), "postgresConnectionBudget": budget})
	}
	secrets, err := clusterplan.LoadSecrets(secretsPath)
	if err != nil {
		return err
	}
	files, err := clusterplan.Render(inv, secrets)
	if err != nil {
		return err
	}
	if out == "" {
		out = filepath.Join("dist", "cluster", inv.Name)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(out, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, ".env") || name == "init.env" {
			mode = 0o600
		}
		if err = os.WriteFile(path, files[name], mode); err != nil {
			return err
		}
		_ = os.Chmod(path, mode)
	}
	fmt.Printf("rendered %d files for %d nodes into %s\n", len(files), len(inv.Nodes), out)
	if secrets, _ := clusterplan.LoadSecrets(secretsPath); secrets.DeepSeekAPIKey == "" {
		fmt.Println("note: deepseekApiKey is empty; AI features stay unavailable until it is set (secrets file or 模型管理)")
	}
	return nil
}
