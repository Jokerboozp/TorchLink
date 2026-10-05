# 参与开发

协作规则、业务约束和源码入口统一维护在 [AGENTS.md](AGENTS.md)，开发环境与测试细节见 [开发与测试](docs/DEVELOPMENT.md)。提交前请：

1. 只修改与任务相关的文件，Go 代码运行 `gofmt`，接口或配置行为变化同步更新 `docs/`；
2. 在仓库根目录运行 `go test ./cmd/... ./internal/... ./deploy/toolaccounts`，改动独立协议 module 时在其目录运行 `go test ./...`；
3. 在 `iot_front` 运行 `npm run lint`、`npm run format:check`、`npm test` 和 `npm run build`；
4. 改动部署脚本时运行 `scripts/tests/deployment-smoke.sh` 与 `.ps1`（需传入独立的 Compose 可执行文件路径）；
5. 新增或修改 HTTP 路由后在仓库根目录运行 `IOT_UPDATE_ROUTES=1 go test ./internal/httpapi -run TestRegisteredRoutesMatchSnapshot`，更新路由快照与 [接口清单](docs/API.md)；
6. 运行 `git diff --check`，在说明中写清实际改动、已运行的检查和未验证的范围。

本软件为专有软件（见 [LICENSE](LICENSE)），提交即表示同意按该许可将贡献归入本软件。安全问题按 [SECURITY.md](SECURITY.md) 私密报告。
