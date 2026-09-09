# 设备孪生与拓扑

设备连接详情的“设备孪生与拓扑”展示当前设备及相邻关系，可点击节点继续查看。孪生节点使用既有设备库存、连接/业务状态、产品物模型和影子；属性历史读取已成功解析的 StandardMessage，不复制另一套状态库，也不把拓扑关系当成设备控制规则。

## 关系与并发

支持 `contains`（包含）、`monitors`（监测）、`depends_on`（依赖）。包含关系每个目标只能有一个父节点，包含和依赖分别禁止有向环；监测允许多方关系。关系 ID 由源、类型和目标稳定生成，重复添加同一关系不增加版本。关系修改不改变旧 GatewayID 或摄像头的设备关联，这些既有业务归属仍由原接口维护。

所有端点限制当前租户，关系两端都必须为本租户已有设备。operator 可添加和解除关系，viewer 只读。全租户拓扑有独立版本；提交须带 expectedVersion，跨进程并发只有一个相同版本的写入成功，其他返回 409，刷新并核对后再提交。状态上报不改变拓扑版本。Memory 与 PostgreSQL 使用相同图约束；数据库写入在事务内验证设备归属，新增 `device_twin_topology` 表随启动迁移。

## 接口

- `GET /api/v1/device-twins/{id}?depth=1`：返回版本、设备节点、关系、当前焦点设备影子和产品物模型。depth 为 0–3，单次最多 200 个节点，超出时 `truncated:true`；图形最多同时画 16 个节点，当前查询的关系在下方列表展示。节点 DTO 只包含名称、产品 ID、启停、连接/业务状态与最近上报时间，不返回凭据和内部标签。
- `PATCH /api/v1/device-twin-topology`：`{"expectedVersion":0,"add":[{"source":"device-a","target":"device-b","kind":"contains"}],"remove":[]}`。每次最多 200 项变更，全租户最多 10,000 条关系；不存在/跨租户设备、循环、多父节点或无效关系返回 422。接口保留审计记录。
- `GET /api/v1/device-registry/{id}/history?kind=property`：已解析属性历史，沿用 page/pageSize 分页及租户权限。

关系版本、实时状态和影子是各自来源的当前读取，不是全业务跨表原子快照；各自时间和版本分别报告。没有上报的设备显示未知，不将“已登记”显示为已连接。影子 desired/reported/delta、可写物模型和真实设备凭据读取见 `DEVICE_SHADOW.md`。

## 验证与边界

共享仓储合约 `repositorytest.TwinTopology` 验证 16 并发写入、重复关系、包含与依赖环、重复父节点、跨租户引用、移除和未知状态。`TestDeviceShadowAuthenticatedReconciliation` 通过真实设备认证上报→Raw/Parser→影子收敛，随后验证孪生读取及属性历史；配置 Chrome 后增加图导航、关系添加/解除、版本及窄屏实际浏览器验收。

这是设备层面的状态与关系孪生，不包含三维场景、物理仿真、关系驱动自动控制或多种资产建模语言。未对厂商实体设备或生产系统验收。默认与命名影子均已支持使用设备身份进行 HTTP / MQTT 查询，具体状态、主题、命名和收敛契约见 [设备影子](DEVICE_SHADOW.md)；查询不会直接执行设备动作。
