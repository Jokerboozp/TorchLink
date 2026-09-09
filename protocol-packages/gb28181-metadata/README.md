# GB28181 元数据节点

独立 Go module，负责 GB28181 的 SIP 注册、保活、设备信息与摄像头目录。密码验证成功后才接受来自该 Socket 对端的元数据；通过平台节点凭据上报目录。平台“摄像头映射 → GB28181 目录”可显式添加摄像头，已有编辑和设备关联保持不变。

本服务实现元数据子集，并非 GB/T 28181 全项符合性认证。当前不提供媒体、云台、设备配置、级联代理或视频流。平台继续仅管理摄像头元数据和单设备关联；认证采用传统 SIP Digest MD5/qop=auth，注册后的 MESSAGE 依赖已认证会话与来源地址检查，未实现 GB/T 28181-2022 的完整安全能力。

## 本机运行

1. 在平台登记一个专用于视频元数据的启用节点，生成节点凭据；不向这个节点分配一般采集 Profile，也不与另一个 Agent 共用节点身份。
2. 在本机创建私有配置文件（Unix 权限 `0600`）。以下值均为占位符，必须替换；SIP 密码仅留在该文件，不上传平台。`listen` 必须为本机具体 IP，设备应可直接访问该地址，不能填 `0.0.0.0`。

```json
{
  "sip": {
    "serverId": "34020000002000000001",
    "realm": "3402000000",
    "listen": "192.0.2.10:5060",
    "allowedCidrs": ["192.0.2.0/24"],
    "devices": {"34020000001320000001": "REPLACE_WITH_DEVICE_PASSWORD"}
  },
  "platform": {
    "url": "https://platform.example.com",
    "tenantId": "REPLACE_TENANT",
    "nodeId": "REPLACE_NODE_ID",
    "secret": "REPLACE_WITH_NODE_SECRET",
    "dataDir": "./private-catalog-data",
    "allowHTTP": false
  }
}
```

3. 在本 module 目录构建并运行：

```bash
go build -trimpath -o ./gb28181-metadata ./cmd/gb28181-metadata
./gb28181-metadata --config /absolute/path/private-video-node.json
```

4. 在设备上配置对应的平台 ID、域、IP/端口、设备 ID 和密码，选择 TCP 或 UDP。两种传输使用同一端口；服务先完成平台节点认证，再启动监听。
5. 等待平台节点心跳与目录，打开摄像头映射页面选择该节点，再点击“添加”。仅最近两分钟的完整目录且节点最近 30 秒有心跳、设备注册有效时可添加。目录分组不作为摄像头导入。

平台 URL 默认 HTTPS，校验系统信任证书且禁止重定向；隔离测试环境可显式打开 `allowHTTP`。本地配置、二进制和目录数据不应提交到 Git。Windows 配置文件应由部署者设置当前账户专有 ACL。

## 状态、故障与容量

- SIP 401 是真实 Digest 挑战；密码错误/新事务重放已消费 nonce 返回 403。同一报文事务重传缓存应答，不重复处理。
- 查询 Catalog 和 DeviceInfo 的事务独立关联 SN / Call-ID / CSeq，不依赖到达顺序。完整分批目录才替换旧目录；超时/冲突保留旧快照并显示错误。收到 SIP 200 仅代表查询受理，不能当作目录成功。
- 平台每 10 秒收到一次心跳，目录每分钟刷新。注册最长一小时；保活超过三分钟标记离线。平台凭据被禁用/撤销会停止服务；连续 24 小时无法刷新平台认证也停止。
- 本地 `catalog.json` 原子替换并同步文件，保留最新元数据。网络失败不会伪造上传成功，恢复后重发当前快照；进程重启恢复的目录标记未注册，必须重新认证。它不保存完整历史事件日志。
- 单节点最多 256 个注册系统、1000 个通道；XML/报文 64 KiB（TCP 正文 48 KiB），TCP 64 个连接、UDP 每秒 64 包，收发各 8 个 worker 与 128 项队列。过载丢包依赖 SIP 重试，不能据此推断设备离线。当前为直连局域网，不实现 NAT 穿透或跨层级级联。

## 验证

本 module 的测试不包含在仓库根 `go test ./...` 中，必须单独执行：

```bash
go test -race ./...
go test . -run '^$' -fuzz FuzzCatalogXML -fuzztime=3s
go test . -run '^$' -fuzz FuzzSIPFrame -fuzztime=3s
```

`TestSIPRegistrationCatalogAndKeepalive` 使用真实本机 TCP/UDP Socket 与独立计算 Digest 的模拟设备。平台根目录 `TestGB28181AuthenticatedCatalogImport` 调用此 module，验证真实 SIP 认证、节点认证、上报、权限与摄像头导入；设置 `IOT_TEST_BROWSER` 并构建前端可额外执行实际 Chrome 交互。厂商真机、国标符合性、安全扩展及现场网络未验收。
