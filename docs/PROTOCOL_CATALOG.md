# 可信协议目录与远程源码安装

目录仅发布协议源码；条目的 kind 可省略或设为 `protocol-source`。现场程序升级已移除，`kind=edge-agent` 条目会被拒绝，已有混合目录需先移除程序条目再重新签名。

设备接入页面的“协议目录”展示经过签名验证的协议版本，支持按名称、发布者和标签筛选。管理员明确确认后，从配置的 HTTPS 源下载 Go 项目 ZIP；源码校验、关闭在线下载的编译、实际样例试跑和不可变制品保存继续使用现有源码发布链路。安装结果为 `VALIDATED`，操作员随后在版本列表发布、绑定或回滚；安装不会直接切换产品。

这是平台的可信私有协议分发目录。组织内发布、独立审核、读取认证和签名分发已接通，见 [企业私有协议市场](PRIVATE_PROTOCOL_MARKET.md)。未连接第三方公共市场；支付、评论和公共账号仍未实现。签名证明来源与内容一致性，不保证代码安全；Worker 是具有 API 服务账户权限的子进程，管理员须信任签名发布方。

## 信任配置

API 进程设置 `IOT_PROTOCOL_CATALOG_POLICY=/absolute/path/catalog-policy.json`。默认留空，目录关闭。配置由部署管理员在服务器维护，不提供浏览器修改信任根或输入任意下载 URL 的接口。

```json
{
  "url": "https://protocols.example.com/catalog.json",
  "publicKeys": {"release-2026": "BASE64_ED25519_PUBLIC_KEY"},
  "caFile": ""
}
```

`caFile` 可为私有 CA 的本机 PEM 路径；留空使用系统信任。HTTPS 始终校验证书，不允许明文 HTTP、URL 凭据、查询参数或重定向。所有源码地址必须与目录地址同一 HTTPS origin。密钥轮换先加入新公钥，发布新签名目录后再移除旧公钥；下一次目录请求即重新读取信任配置。私有目录可配置 `tokenFile` 为仅服务器账户可读的原始读取令牌文件；令牌只发送至同一 HTTPS origin，不提供给浏览器。

Compose 已传入该变量。容器内路径须位于已有挂载中，例如 `/app/data/catalog-policy.json`；私有 CA 同样须可读。独立 API/Gateway 模式只需 API 读取目录。离线环境可使用局域网 HTTPS 目录，或沿用已有本地 ZIP 上传。

## 发布方操作

在仓库根目录构建签名工具（不依赖远端服务）：

```bash
go build -o /tmp/iot-protocol-catalog ./cmd/iot-protocol-catalog
/tmp/iot-protocol-catalog --generate-key --key /private/path/catalog.key
```

工具只输出公钥；私钥文件以 `0600` 创建，不覆盖现有密钥。Windows 须由部署者设置账户专有 ACL。将公钥加入消费平台的信任配置。私钥不得上传静态站点或提交 Git。

准备项目 ZIP，根目录须有 `protocol.json`（至少 ID、版本与协议元数据）、完整 Go 源码，以及 `samples/cases.json`；第三方依赖随 `vendor` 提供。结构及样例契约见 `GO_PROTOCOL_PACKAGES.md`。然后准备目录 payload JSON：

```json
{
  "issuedAt": 1788970000000,
  "expiresAt": 1788973600000,
  "entries": [{
    "id": "vendor-fire",
    "version": "1.0.0",
    "name": "厂商消防协议",
    "description": "适用型号和已验证功能",
    "publisher": "组织名称",
    "license": "组织许可名称",
    "sourceUrl": "https://protocols.example.com/sources/vendor-fire-1.0.0.zip",
    "sha256": "REPLACE_WITH_LOWERCASE_SHA256",
    "size": 12345,
    "tags": ["消防", "TCP"]
  }]
}
```

时间为 Unix 毫秒，应替换为当前签发时间及有效期，最长 24 小时。SHA-256 和 size 必须来自实际 ZIP 字节。协议 ID/版本在 ZIP 与目录内须完全一致。

```bash
/tmp/iot-protocol-catalog --key /private/path/catalog.key --key-id release-2026 --input /private/path/payload.json --output /private/path/catalog.json
```

工具签名原始 payload 字节，生成包含 `keyId`、Base64 `payload` 与 Ed25519 `signature` 的 envelope，不覆盖已有输出。通过组织既有发布流程将版本 ZIP 与签名目录部署到同一 HTTPS 源；本工具不自动上传或保存发布凭据。目录签名过期后须重新签发。移除条目会阻止新安装，不删除平台已安装的历史制品。

## API 与失败语义

- `GET /api/v2/protocol-catalog`：viewer 及以上，验证签名后返回目录和 payload SHA-256。配置关闭返回 `enabled:false`；TLS/签名/过期失败返回真实错误，不显示旧目录为已验证。
- `POST /api/v2/protocol-catalog/install`：仅 admin，JSON 包含 `id`、`version`、GET 返回的 `digest` 及 `confirmed:true`。安装重新获取并验证目录，摘要变更返回 409，须刷新后重新确认。条目不存在 404，源码哈希/大小错误 502，编译或样例失败 422；同租户已有版本返回 409，不覆盖。
- 校验通过保存现有 `ProtocolRelease`，构建信息增加已验证的目录摘要、签名 key ID、发布者与源码哈希。源包与版本仍遵守原有哈希、历史保留和回滚规则；签名身份由信任配置确定，条目里的发布者名称仅为签名元数据。
- 单目录最多 1000 项、原始 envelope 8 MiB；单源码 ZIP 32 MiB。请求限时 20 秒、同时最多 4 个目录操作；源码展开、编译和样例另受既有容量/超时约束。过载 429，不返回虚假安装成功。

消费平台不自动在后台安装更新，公共商业市场未实现。组织内审核发布与组织之间的授权分发见企业私有协议市场说明。目录源码可在 `protocol.json` 中声明 `targetPlatforms`，安装复用源码多平台编译；发布端实际跑样例，其他平台须另行执行验证，不能把编译状态当作样例通过。目录签名不能替代这些执行校验，详见 [Go 协议包](GO_PROTOCOL_PACKAGES.md#多平台协议制品)。

## 验证入口

```bash
go test -race ./internal/protocolcatalog -count=1
go test ./cmd/iot-protocol-catalog -count=1
go test -race ./internal/httpapi -run '^TestAuthenticatedCatalogSourceInstall$' -count=1 -v
```

客户端测试使用真实 TLS 服务，覆盖可信/不可信证书、错误签名、未知密钥、过期、跨源、重定向、源码损坏与服务失败。API 用例执行实际 ZIP 下载、编译、样例与版本发布，角色及确认检查均实际执行。构建前端并配置 `IOT_TEST_BROWSER` 后额外运行真实 Chrome 页面操作；环境未配置则该浏览器子用例明确跳过。
