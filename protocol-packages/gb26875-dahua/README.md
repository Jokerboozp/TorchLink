# GB26875 大华协议包

这是可独立维护的纯 Go module，无平台源码依赖。将整个目录复制到外部协议仓库即可管理。

- `go test ./...` 验证样例、分片和损坏帧。
- 修改 `protocol.json` 的 version 后，将根目录内容 ZIP 上传到平台 Go 源码接入；包内有 decode 及 ingress/encode 样例。
- `go run ./cmd/simulator --address 127.0.0.1:26875 --network tcp --hold 1m` 验证注册、火警上报和主动校时应答。
- 接口为 `go-protocol-v2`，能力为 decode、ingress、encode，网络 TCP_UDP。stdin 一个 JSON 请求，stdout 一个 JSON 结果，每次调用后退出。
- `encode` 当前支持 `{"type":"time-sync"}`，可选 timestamp 为 Unix 毫秒。BCD 时间按 UTC+08:00。

完整使用步骤见平台 [GB26875 接入文档](../../docs/GB26875_DAHUA_V103.md)；这里的源码与模拟器本身不依赖该文档或平台目录。
