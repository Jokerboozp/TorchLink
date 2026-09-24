# 2026-09-24 本机容量压测原始记录

主报告：[平台承压能力评估](../../CAPACITY_ASSESSMENT_2026-09-24.md)。同目录的六个 JSONL 文件按测试阶段保存生成器输出，时间字段为 UTC；测试脚本留在本机忽略目录 `data/capacity-run/`。JSONL 不包含登录令牌或设备密钥。

持续上报试验中的 `cap-ing-muf0u3kh` 阶段需结合下列人工核验解读：

- 128 并发持续 60 秒，13,395/13,395 请求收到 202，均值 222 条/秒，P95 1,012 ms；其完整阶段结果在 `ingest-results.jsonl` 末尾。
- 后续 256 并发阶段开始后，发现原文处理明显落后，主动中止了生成器；因此没有完整的 256 并发阶段结果，不从这段推算级别成功率或延迟。
- 停止发送后，数据库中该前缀共有 23,485 条原文。清理前读数为：`rawAccepted=23485`、`rawParseAttempted=23485`、`rawParseFailed=0`、`standardStored=23485`、`standardProcessed=23485`、`registered=0`、`alarms=0`。与 128 并发阶段之差 10,090 条来自中止前的下一阶段。
- 清理脚本在以上计数追平后执行；ClickHouse `iot_raw_message`/`iot_telemetry` 查询余量均为 0；PostgreSQL 删除 `device_state_event=27700`、`device_state=1000`、`standard_message=23485`、`raw_message_log=1000`、`raw_archive_index=23485`。随后按 `cap-ing-%` 查询各项余量均为 0，API 设备总数为 14、事件响应为 12 条状态和 2 条活动告警。

这里的 202 仅代表入口接受。短时级别、持续上报和单次数据库快照的结果应分别使用；本机观测值不代表目标环境的稳态容量。
