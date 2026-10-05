# 在线部署

有网目标机从源码仓库部署单机平台。模块开关、端口、配置与维护见 [部署总览](DEPLOYMENT.md)。

## 在线部署

在有网目标机的仓库根目录执行：

```bash
# Linux；已使用 root 登录可省略 sudo
sudo bash ./scripts/deploy-online.sh
# macOS；先启动 Docker Desktop
bash ./scripts/deploy-online.sh
```

Windows PowerShell：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\deploy-online.ps1
```

脚本生成 `.env.online`，构建镜像，启动并检查服务；不下载 AI 模型权重。首次需要访问镜像、Go/npm 依赖、Harness 源码及 pgvector 源码；服务器无需预装 Go 或 Node.js。更新源码后重跑同一脚本，沿用原配置、Compose 项目和数据卷。使用自定义旧环境时，先按 [配置与数据归属](DEPLOYMENT.md#配置与维护) 指定原参数。
