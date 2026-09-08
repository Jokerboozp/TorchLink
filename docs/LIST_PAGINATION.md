# 列表分页与关联选择

采用 `internal/httpapi/pagination.go` 的列表接口支持 `page/pageSize`，兼容 `limit/offset`。每页默认 20 条，上限 100 条，返回 `items`、`total`、`page`、`pageSize` 等分页字段。

正数 `page` 优先于 `offset`。页码或偏移超出整数安全计算范围时先收敛到安全上界；越界页返回空 `items`，保留实际 `total`，不回绕到第一页，也不因乘法或加法溢出返回 500。响应中的页码/偏移反映收敛后的值。非数字参数沿用默认行为。

设备、产品、规则、摄像头页面的关联选择通过 `apiAll` 逐页加载选项，与表格当前页分开保存。增加 `pageSize` 不能绕过服务端上限。任一页请求失败时整个选项目录加载失败，不把半份目录作为成功结果。

这四个页面的列表刷新仅允许最新请求写入数据、总数及加载状态；旧请求晚返回或失败不覆盖当前结果，也不弹出过时错误。关联目录按请求逐页读取，不提供数据库快照一致性保证。

## 回归验证

在仓库根目录运行：

```powershell
go test ./internal/httpapi -run 'Test(OversizedPagination|PaginationArithmetic|PageItems|ParseListPagination)' -count=1
```

在 `iot_front` 运行：

```powershell
node --test tests/list-behavior.test.mjs
```

前端测试执行实际页面脚本和 Vue 响应式逻辑，用模拟 API 控制分页及响应顺序；后端测试经过真实 HTTP 路由、认证及内存仓库。这些检查不等于浏览器、中间件或生产环境验收。
