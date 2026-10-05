# PostgreSQL 迁移

`../schema.sql` 是幂等基线（版本 0），每次启动都会执行。之后的结构或数据变更在此目录新增 `NNNN_名称.sql`，或在 Go 中用 `registerMigration` 注册，版本号全局唯一且只增不改。

- 每个迁移只执行一次，结果记录在 `schema_migration`（版本、名称、校验和）；已执行的文件被修改时平台拒绝启动。
- `.sql` 默认在一个事务内执行。首行写 `-- migrate:no-transaction` 时逐条在事务外执行，语句之间用单独一行 `;` 分隔，用于大表上的 `CREATE INDEX CONCURRENTLY IF NOT EXISTS`；同名索引若因上次中断而无效，会先自动删除再重建。
- 迁移在会话级 advisory lock 下串行执行，多副本同时启动时其余进程等待后跳过。
- 大表的数据搬迁采用“先加新结构 → 后台分批回填 → 切换读写 → 下个版本删除旧结构”，不在启动迁移中长时间锁表。

## 分区表

`raw_archive_index`、`raw_message_log`、`standard_message`、`device_state_event`、`audit_log` 自迁移 0011 起为按月分区表（父表保留原索引名，旧数据在 `<表>_legacy` 分区）。修改这些表时：

- 新增列用 `ALTER TABLE <父表> ADD COLUMN IF NOT EXISTS`，会同步到所有分区。
- 新增索引在父表上创建会逐个分区建索引并持有锁；大表请先在各分区 `CREATE INDEX CONCURRENTLY`，再在父表 `CREATE INDEX ... ON ONLY` 并 `ALTER INDEX ... ATTACH PARTITION`。分区数量随月份变化，用 `registerConnMigration` 注册事务外的 Go 迁移并调用 `createPartitionedIndex`（见 `partition_index.go`，迁移 0018），之后新建的月分区自动继承父表索引。
- 唯一约束必须包含分区键；按消息编号去重依赖写入检查（见 `partitions.go` 注释），不要新增依赖 `ON CONFLICT (tenant_id, message_id)` 的语句。
- 按 `ctid` 删除只能逐个分区执行（`leafTables`）。
