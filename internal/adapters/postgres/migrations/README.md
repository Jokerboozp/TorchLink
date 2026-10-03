# PostgreSQL 迁移

`../schema.sql` 是幂等基线（版本 0），每次启动都会执行。之后的结构或数据变更在此目录新增 `NNNN_名称.sql`，或在 Go 中用 `registerMigration` 注册，版本号全局唯一且只增不改。

- 每个迁移只执行一次，结果记录在 `schema_migration`（版本、名称、校验和）；已执行的文件被修改时平台拒绝启动。
- `.sql` 默认在一个事务内执行。首行写 `-- migrate:no-transaction` 时逐条在事务外执行，语句之间用单独一行 `;` 分隔，用于大表上的 `CREATE INDEX CONCURRENTLY`。
- 迁移在会话级 advisory lock 下串行执行，多副本同时启动时其余进程等待后跳过。
- 大表的数据搬迁采用“先加新结构 → 后台分批回填 → 切换读写 → 下个版本删除旧结构”，不在启动迁移中长时间锁表。
