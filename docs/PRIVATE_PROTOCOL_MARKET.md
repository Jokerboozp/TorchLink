# 企业私有协议市场

设备接入页的“组织发布”提供完整 Go 源码版本的提交、独立审核、上架、撤回和审核记录。消费平台继续使用“协议目录”安装，实际完成 HTTPS 认证、签名/源码哈希校验、关闭在线依赖的编译及样例试跑，再保存为 VALIDATED。发布者的审核不能代替消费方执行校验，也不自动切换消费方产品。

分发源可为组织内网服务，每组织最多 1000 个不可变版本。当前范围为组织内协议审核与分发，不提供公共社区账号、评论或交易。

## 管理流程

1. 在“设备接入 → 源码接入”上传完整 ZIP，实际通过编译和样例。源码内 `protocol.json` 的 id/version 必须等于最终保存版本；单文件 `.go` 须整理成完整源码包后提交市场。原文件、哈希和版本保留，不原地修改。
2. operator/admin 在“组织发布”选择已校验版本，填写名称、适用范围、组织许可和标签，明确提交审核。提交保存原始源码和整个制品的 SHA-256、大小、操作者与时间。提交后元数据不可改写，修订使用新版本。
3. 另一名 admin 下载并核实源码、许可和样例记录，填写意见，确认上架或拒绝。提交者不能自审，UI 和 API 均检查；并发审核按 generation 比较更新，重复/过期决定返回 409。拒绝和撤回均保留记录，不清除证据。
4. 上架前重新核实源包/制品摘要和签名私钥可用性。只有 APPROVED 条目进入组织签名目录，分发每次重新认证读取凭据。撤回后目录立即排除该版本，旧目录中的源码 URL 也拒绝下载；已安装版本继续保留，可在消费平台按既有流程停用或回滚。

审核只影响市场分发，不改变发布平台自身的产品绑定或历史版本状态。版本表中的 VALIDATED/PUBLISHED 与市场上架是两个不同流程。

## 发布方配置

API 设置 `IOT_PROTOCOL_MARKET_POLICY=/private/path/market-policy.json`，留空关闭。配置由部署管理员在服务主机维护，界面不能更改签名密钥、组织归属或读取凭据。

```json
{
  "publicOrigin": "https://market.example.com",
  "organizations": {
    "tenant_001": {
      "publisher": "组织名称",
      "keyId": "organization-2026",
      "privateKeyFile": "/private/path/catalog.key",
      "readerTokenHashes": ["读取令牌原文的64位小写SHA256"]
    }
  }
}
```

`publicOrigin` 必须是 HTTPS origin，不接受用户名、路径、查询参数或从请求 Host 推导。API 可位于组织现有 HTTPS 反向代理后；平台不自动部署域名、证书或反向代理。Compose 与离线包模板已传入该变量，配置和私钥路径须在容器现有挂载范围内。

签名密钥使用现有 `iot-protocol-catalog --generate-key --key /private/path/catalog.key` 创建，构建和公钥配置见 [可信协议目录](PROTOCOL_CATALOG.md)。私钥为 Base64 Ed25519 完整私钥，Unix 文件权限须为 0600，Windows 由部署者配置专有 ACL。私钥、原始读取令牌和真实环境文件均不得提交 Git。组织读取令牌应以密码学随机数生成并单独交付消费方；服务端仅保存其 SHA-256，每组织可配置 1–64 个摘要供轮换。

目录读取、源码分发、提交和审核复用同一进程最多 4 个并发操作的限制，过载返回 429。组织配置最多 100 项。组织身份对应平台既有 tenant，不新增另一套账号体系。管理操作使用原有用户 JWT 和角色；分发使用独立机器读取令牌，两者不能互换。

## 消费方配置

消费平台的 `IOT_PROTOCOL_CATALOG_POLICY` 指向以下本机配置：

```json
{
  "url": "https://market.example.com/api/v2/market-distribution/tenant_001/catalog",
  "publicKeys": {"organization-2026": "BASE64_ED25519_PUBLIC_KEY"},
  "caFile": "/private/path/organization-ca.pem",
  "tokenFile": "/private/path/market-reader.token"
}
```

`tokenFile` 包含原始组织读取令牌，32–4096 字符，不含内部空白；Unix 权限须为 0600。该字段可省略以兼容无需读取认证的既有静态目录。CA 留空使用系统信任，始终验证 HTTPS 证书。读取令牌仅发送给配置中同一 HTTPS origin，不跟随重定向或外部源码地址，不返回浏览器。

发布方目录按小时生成稳定签发时间，有效期两小时；条目、审核状态或签名密钥变化会改变摘要，整点也会更新。消费方安装前再次获取并比较摘要，变化返回 409，刷新后重新确认即可。撤回与令牌撤销在服务端每次源码请求生效，不依赖等待签名过期；已开始的下载与已安装的源码不被自动删除。

## API 与存储

- `GET /api/v2/protocol-market`：viewer，当前租户提交记录、组织显示名和目录地址，不返回私钥路径/读取摘要。
- `POST /api/v2/protocol-market/{id}/{version}`：operator，名称、说明、license、tags 和 `confirmed:true`；必须是当前租户已实际校验的完整源码版本。
- `POST /api/v2/protocol-market/{id}/{version}/review`：admin，`decision` 为 APPROVED/REJECTED/WITHDRAWN，附 reason、generation 和 `confirmed:true`。APPROVED/REJECTED 仅接受另一名用户对 SUBMITTED 的决定；撤回为终态。
- `GET /api/v2/market-distribution/{tenant}/catalog`、`.../protocols/{id}/{version}/source.zip`：专用 `Authorization: Bearer <组织读取令牌>`。目录还经过 Ed25519 签名；源码只允许已审核版本，沿用规范制品路径、目录边界与双重摘要校验。

启动迁移新增 `protocol_market_entry`，主键为租户/协议/版本，JSONB 保存不可变提交资料和审核记录。PostgreSQL 事务锁与内存仓储提供相同的并发唯一提交、独立审核及版本比较语义，业务审计同时记录提交和决定。备份时仍须保留平台数据库与协议制品目录；私钥、读取凭据及信任配置通过部署方自己的秘密备份流程管理。

## 验证

```bash
go test -race ./internal/protocolmarket ./internal/protocolcatalog ./internal/adapters/memory -count=1
go test -race ./internal/httpapi -run '^TestPrivateProtocolMarketReviewAndRemoteInstall$' -count=1 -v
```

API 集成测试启动真实 TLS 分发服务和独立消费平台，完成原始 ZIP 上传/编译/样例、双用户审核、独立令牌、签名下载、消费方再次编译/样例，检查错误凭据、跨组织、错误证书、重复审核、撤回和读取凭据轮换。配置 `IOT_TEST_BROWSER` 后实际跑 Chrome 的提交、作者不可自审、切换到另一认证账号审核及窄屏。PostgreSQL 用例入口为 `TestDeviceOperationsMigrationAndAtomicity`，须显式配置临时测试数据库。未配置的外部环境明确跳过。
