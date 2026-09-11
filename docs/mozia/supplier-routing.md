# 供应商资源池与动态分流：第一期

实现分支：`codex/supplier-routing`。复用 new-api 的渠道、鉴权、计费、Redis、模型更新检测和后台任务，没有新增运行依赖。

## 已实现

- 供应商档案、共享资源池、模型验收规格、渠道绑定。
- 容量均衡、供应商目标份额、按渠道 Priority 主备切换；客户规则优先于分组和模型规则。
- 共享并发、RPM、TPM 原子预占；多渠道、多 Key、多网关共用资源池额度。
- 健康降载、暂停、冷却试运行；重试排除失败资源池，执行异常排除相同故障域。
- 逐次调用记录、上游原始 usage、采购价快照、待核对及人工采购核对。
- 不可变版本、校验、发布、客户灰度、观察模式、策略回滚。

配置入口：**系统设置 → 模型与路由 → 供应商调度**，路径 `/system-settings/models/supplier-routing`。默认使用可视化表单编辑供应商、资源池、渠道与模型绑定、分流规则；高级 JSON 与表单共用一份草稿，校验后发布生效。页面保留模型声明预览和版本回滚。普通渠道编辑不能直接修改供应商归属、资源池映射或已验收的模型映射。

监控入口：控制台侧栏 **供应商监控**，路径 `/supplier-monitor`。独立查看本小时流量、调度建议和最近调用，每 30 秒刷新；支持按供应商和模型筛选。设置页不再轮询监控数据。

### 三个控制项

- **启用供应商动态调度**：开启后，匹配供应商规则的请求可以采用新策略。关闭时使用原渠道选路，但已绑定资源池的容量限制仍然生效。
- **仅观察调度建议**：记录新策略建议，实际请求仍按原路由发送；建议本身不调用上游、不占用容量。此开关优先于灰度比例。
- **客户灰度比例**：关闭观察模式后，决定多少客户实际使用新策略。例如 10% 是按用户 ID 稳定划分约 10% 的匹配客户，其余仍观察；不是每次请求随机取 10%。0% 全部观察，100% 全部应用于匹配请求。

监控汇总按客户请求去重，排除探测与调度建议；表格筛选不改变汇总卡片口径。首次份额按同模型、同分组的供应商计算，不同明细行上的份额不能相加。调度建议单列展示；最近调用从最近 100 条记录中排除建议，因此可能不足 100 条。采购记录及核对仅向具有对应权限的账号展示。

## 接入和发布

1. 所有访问同一试点资源池的网关先部署本版本，并使用同一个 Redis 主库。试点池不能同时供未接入准入的旧程序使用。
2. 在原有渠道页面建立 OpenAI 兼容渠道，配置地址、Key、模型、分组、映射、Priority 和采购价格。
3. 在供应商路由页面填写档案和模型规格。`acceptance` 填实际压测/验收报告标识；地区与数据政策先通过商务验收，再以规则的供应商白名单限制派发。
4. 首先发布 `enabled=false`：保留原选路，但已绑定渠道必须通过容量准入。
5. 改为 `enabled=true, shadow=true`，记录新策略建议，实际仍按原路由派发。
6. 关闭 `shadow`，逐步提高 `canary_percent`。按服务端用户 ID 稳定分桶，同一用户不会逐请求随机切换模式。
7. 切回原路由用 `enabled=false`；回滚按钮只恢复策略、灰度和开关，保留当前资源池容量、绑定与在途预占。

发布权限为 `supplier_routing:publish`；查看配置和指标沿用 `channel:read`，拉取模型声明沿用 `channel:operate`，采购记录及核对沿用 `model_pricing:read/write`。发布和人工核对写入现有管理审计。通用 Option 接口不能修改路由发布记录。

## 配置示例

下面是单池结构示例；ID、模型和验收报告需要替换成实际值。增加供应商时复用相同结构，并在规则 `targets` 中填入例如 50、30、20 的权重。

```json
{
  "revision": 0,
  "enabled": false,
  "shadow": true,
  "canary_percent": 0,
  "suppliers": [{"id": 1, "name": "机房 A", "region": "cn-east", "enabled": true, "contact": "商务联系人", "data_policy": "已确认的数据处理政策", "terms": "合同编号"}],
  "pools": [{
    "id": 1, "supplier_id": 1, "name": "A 共享集群", "failure_domain": "dc-a",
    "enabled": false,
    "limits": {"concurrency": 10, "rpm": 100, "tpm": 100000},
    "max_execution_seconds": 60, "input_safety_percent": 110,
    "acceptance": "填写实际验收报告后启用",
    "models": [{"name": "your-model", "version": "verified-version", "context_tokens": 8192, "max_output_tokens": 2048, "tools": true, "json": true, "limits": {"concurrency": 0, "rpm": 0, "tpm": 0}}]
  }],
  "bindings": [{"channel_id": 123, "model": "your-model", "pool_id": 1}],
  "rules": [{
    "id": "model-default", "model": "your-model", "group": "", "user_id": 0,
    "mode": "capacity", "targets": [{"supplier_id": 1, "weight": 100}],
    "max_attempts": 3, "timeout_seconds": 120, "max_supplier_percent": 100,
    "health": {"window_seconds": 60, "min_samples": 10, "failure_percent": 20, "max_ttft_ms": 5000, "cooldown_seconds": 30, "trial_percent": 10}
  }]
}
```

分模型额度 `0` 表示继承池上限，不能超过池额度。新池/久无样本的池从试运行开始，试运行比例限制模型并发及调度权重，RPM/TPM 仍按完整硬额度检查，避免单个合法请求无法进入试运行。

`max_supplier_percent=0` 表示不加集中度限制；其他值约束本规则本小时首次派发，向上取整允许一个请求的离散误差。该计数不因发布新版本清空。严格集中度可能在只有一家可用时拒绝请求，请按实际容灾约定配置。

## 容量与费用口径

- 第一阶段仅纳管 **OpenAI 文本 Chat Completions**，包括流式和普通工具调用、JSON 输出；拒绝媒体、托管搜索、其他协议和 `n≠1`。指定渠道、亲和及探测同样经过出站准入。
- RPM 是最近 60 秒实际尝试的请求开始次数。TPM 是同一窗口的“输入保守估算＋输出上界”预占，完成后由原始 usage 校正。**这是按请求开始时间计入的滑动窗口**，接入前必须确认上游采用兼容口径；按生成时刻计 token 的供应商不能直接套用此配置。
- 客户端未传输出上限时，发送已验收的 `max_tokens` 上限；显式上限必须为正且不超规格。输入估算安全余量需按供应商 tokenizer 压测校准。
- 成功和明确结束的失败释放并发；真实请求不退 RPM。未发送的预占可撤销。取消、传输异常、流中断和不完整响应保留并发至保守期限。
- `max_execution_seconds` 必须是上游实际执行/排队超时保证。占位期限为“本请求总截止时间＋上游执行期限”，覆盖发送延迟和本地断开后上游继续运行；不依赖网关正常退出释放。
- 初次派发份额与重试/探测分开统计；增加渠道或 Key 不会增加供应商目标份额。主备切换另有标记。SQL 调用记录是对账依据，Redis 调度计数包含短暂预占，未发送完成撤销后回正。
- 客户在首次预扣前确定价格，动态路由重试保留其倍率及阶梯快照；采购费用按每次调用独立记录，使用已有渠道成本价和 8 位小数，不跨币种合并。
- 自动采购核算支持按次及文本输入/输出/缓存读取单价。复杂计费模式、未知 usage 或执行状态不明均保留待核对，不能当作零费用。小时任务把超过执行期限的遗留 `pending` 转为 `unknown`，再由运营凭上游账单核对；当前没有通用供应商账单查询接口。

调用记录使用现有 `X-Oneapi-Request-Id`，可与客户请求日志关联，不新增正文或密钥日志。统计显示首次/最终成功数、未决请求、首次份额、健康状态和 TTFT；短窗口指标按资源池、模型和分组隔离。

## 故障与恢复

- Redis 不可达、配置丢失或发布状态不确定时采用保守拒绝。当前发布屏障作用于网关选路，可能短暂影响其他渠道；不在状态未知时自动重建空容量账本。
- Redis 丢失后先确认旧执行已结束，等待各记录的 `capacity_until`，核对所有 `pending/unknown` 调用，再保证至少 61 秒没有供应商调用，重新发布。不要直接删除 Redis 键来“解除限流”。
- Redis 使用持久化、足够内存及不淘汰键的部署方式；动态准入脚本使用单个共享主库，当前不支持 Redis Cluster 跨槽执行。
- 发布使用 Redis 租约＋SQL 版本比较更新。保存 SQL 后 Redis 发布失败时仍暂停派发；重新读取最新版本再发布，禁止用旧版本覆盖新状态。
- 在途或待核对调用存在时不能改资源绑定；已有池不能转归其他供应商。模型声明变更只进入预览/待确认，纳管渠道不自动加入新模型，声明版本或上下文/输出上限缩水时拒绝派发。

## 验证和交付边界

已覆盖 SQLite、MySQL 5.7、PostgreSQL 9.6 的迁移与发布；共享 Redis 双客户端并发、幂等、容量下调、份额、健康恢复、未知执行占位；真实 SSE 处理、采购 usage 与客户价格冻结；发布权限边界和最终成功统计。测试同时修复了现有流式结束信号及日志计数的竞态。

```sh
# 指向一次性测试实例：测试会清空 Redis DB 15，数据库兼容测试会重建相关表。
SUPPLIER_TEST_REDIS=127.0.0.1:32768 go test -race ./service ./controller ./relay/channel/openai ./relay/helper -run 'Supplier|StreamScannerHandler'
SUPPLIER_TEST_MYSQL_DSN='<一次性测试库 DSN>' SUPPLIER_TEST_POSTGRES_DSN='<一次性测试库 DSN>' go test ./model -run '^TestSupplierDatabaseCompatibility$'
go test ./controller ./middleware ./router ./relay/... ./model ./service/...
# web/default 内执行
bun run typecheck
bun run build
```

基线 `49d301493` 的 `TestMoziaH3ChannelRegistration` 已存在模型清单差异（额外的 `minimax/minimax-h3-t2va`），与本次变更无关，未改动该测试。

本分支交付第一期 A+B；第二期成本/低延迟/包量策略及合同核销按原计划在试点数据稳定后实施。真实机房压测、供应商账单比对和线上灰度尚未执行，试点前需确定吞吐、路由时延和 SQL/Redis 开销验收阈值。
