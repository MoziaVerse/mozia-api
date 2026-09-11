# 供应商管理：按记录保存

供应商、资源池、绑定、规则以 SQL 记录为事实源。页面每次只提交当前记录；后端在事务中编译完整调度快照，继续使用原有渠道优先级、共享容量账本、健康与分流算法。

## 接口

统一使用 `/api` 前缀、管理员认证及 `{success,message,data}` 响应。

| 资源 | 列表 / 创建 | 详情 / PATCH / DELETE |
| --- | --- | --- |
| 供应商 | `/suppliers` | `/suppliers/:id` |
| 资源池 | `/suppliers/:id/pools` | `/supplier-pools/:id` |
| 渠道模型绑定 | `/supplier-bindings` | `/supplier-bindings/:id` |
| 调度规则 | `/supplier-routing/rules` | `/supplier-routing/rules/:id` |

全局开关：`GET/PATCH /supplier-routing/settings`，仅 `enabled`、`shadow`、`canary_percent` 可编辑。

- `GET /supplier-routing/status`：目标版本、已确认版本、实际运行状态。
- `GET /supplier-routing/revisions[/:id]`：发布记录及不可变快照；支持 `resource_kind`、`resource_id` 筛选。
- `POST /supplier-routing/rules/:id/restore`、`POST /supplier-routing/settings/restore`：提交 `{ "revision": 123 }`，只恢复指定对象的策略，不回退资源池容量或绑定，并重新验证当前依赖。
- 原 `/channel/supplier-routing` 保留只读概览，以及 stats、attempts、reconcile、models 子接口。旧整体 PUT 返回 **410**；通用 option 接口拒绝写入快照、设置及内部应用状态。

供应商列表支持 `q`、`enabled=true|false`、`region`、`model`；绑定支持 `supplier_id`、`pool_id`、`channel_id`、`model`。列表使用 `page`、`page_size`，返回 `items/total/page/page_size`。当前保持原有资源数量上限，因此列表筛选在有界的资源集合中执行。

## 保存契约

详情返回 `version` 和 ETag，例如 `"supplier-12-v7"`。PATCH、DELETE、restore 必须携带 `If-Match`。

- 缺失前提条件：428 `precondition_required`。
- 同一记录版本冲突：412 `resource_version_conflict`，保留草稿并重新加载当前记录。
- 依赖占用或发布尚未生效：409；字段错误：422 `validation_failed`，`field_errors` 路径相对当前记录，例如 `limits.rpm`、`models.0.version`。
- PATCH 省略字段保持原值；显式 `0`、`false`、空字符串保留。嵌套对象合并；`models`、`targets` 数组整体替换。拒绝 null、未知字段及只读字段。
- POST 必须携带 `Idempotency-Key`（1–128 字节），按操作者、路径和键唯一。相同请求重试返回原记录；不同正文冲突；已删除记录不会被同一键复活。标记与记录保存在同一主库事务，不依赖日志库。
- 数字 ID 和规则 ID 由服务端生成，迁移保留原有 ID。资源池不能更换所属供应商。

写入返回 `data.resource/application/revision/replayed`：

| HTTP | application | 含义 |
| --- | --- | --- |
| 200 / 201 | `not_required` | 资料保存完成，无需更新运行时 |
| 200 / 201 | `applied` | SQL 保存及运行时应用已确认 |
| 202 | `pending` | SQL 已提交，后台正在恢复运行时应用；不得当成保存失败重新创建 |

仅修改供应商名称、联系人、合同无需 Redis。影响运行时的写入复用 Redis 发布锁，同时由 SQL 状态行串行化事务。阻断新准入、应用快照、解锁均校验租约；过期发布者不能覆盖新发布者。SQL 提交后不恢复旧快照，复用 SystemTask 每 30 秒检查并重试持久化的目标版本。运行时应用未确认期间拒绝新的运行时变更，资料编辑仍可继续。

Redis 账本丢失时，存在 pending/unknown 或最近 61 秒内可能消耗容量的调用会阻止自动恢复。恢复仅写配置键，不清空容量、配额、健康等运行账本。发布过程中配置键消失也会停止应用，交由下一轮重新核对。

## 表结构与渠道保护

`suppliers`、`supplier_pools` 增加版本、软删除、创建幂等信息；新增 `supplier_bindings`、`supplier_routing_rules`。模型规格、targets、health 使用 TEXT JSON，支持 SQLite、MySQL 5.7、PostgreSQL 9.6。

绑定以可空唯一 `active_key`（channel/model 的 SHA-256）约束有效记录。删除时清空该键，允许后续重新建立绑定，同时保留旧记录及创建幂等性。

Channel 的 `supplier_id` 和 `settings.supplier_pools` 仅为派生投影。统一数据库回调保护单条、批量、标签及上游模型更新：拒绝伪造投影、删除已绑定渠道、移除绑定模型及改变已验收协议/映射。绑定变更保留原有 pending/unknown 保守限制。读取需要 `channel:read`，写入需要 `supplier_routing:publish`；模型声明预览和成本核对沿用原权限。

## 升级与回退

先备份数据库和部署配置，暂停旧配置写入；多实例需确保所有旧写入进程退出，再启用新管理入口。

启动迁移从当前生效 JSON 建立记录，忽略未包含在当前快照中的历史投影行；不修改原运行时 JSON、Redis 容量账本。迁移事务完成才写事实源标记，重复启动不重新导入；PostgreSQL 同步旧手工 ID 对应的序列。

测试服务器可在停止旧应用后做最终备份，再启动新镜像。新记录写入开始后，不可直接换回旧 JSON 管理版本；应使用兼容记录表的修复版本。只有确认没有发生新资源写入时，才可回退旧二进制并保留新增表。

## 验证

- `go test ./model ./service ./controller ./router -count=1`
- 设置指向**独立临时实例**的 `SUPPLIER_TEST_REDIS`、`SUPPLIER_TEST_MYSQL_DSN`、`SUPPLIER_TEST_POSTGRES_DSN` 后运行供应商集成测试。测试会清理指定测试数据库，禁止指向共享或业务数据库。
- `bun test web/default/src/features/supplier-routing/lib/*.test.ts`
- `web/default`：`bun run typecheck`、定向 oxlint/oxfmt、`bun run build`。

已覆盖迁移与三库兼容、独立保存、0/false、创建幂等、资源版本冲突、渠道与绑定依赖、租约失效、提交后确认丢失恢复，以及仅恢复规则不改变当前容量。
