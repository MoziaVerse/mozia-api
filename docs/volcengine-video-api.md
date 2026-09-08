# 火山方舟原生视频 API

Mozia API 支持 ArtsAPI 文档中的四个原生视频任务端点。鉴权使用 Mozia API 令牌：

`Authorization: Bearer <MOZIA_API_KEY>`

| 操作 | 方法 | 路径 |
| --- | --- | --- |
| 创建视频任务 | POST | /api/v3/contents/generations/tasks |
| 查询任务 | GET | /api/v3/contents/generations/tasks/{task_id} |
| 查询任务列表 | GET | /api/v3/contents/generations/tasks |
| 取消排队任务或删除记录 | DELETE | /api/v3/contents/generations/tasks/{task_id} |

## 渠道与 endpoint 配置

- 使用现有 Artsapi 渠道（类型 206），配置上游密钥、可用模型和价格。也支持 DoubaoVideo、VolcEngine 渠道。
- ArtsAPI Base URL 可填写 `https://ai.artsapi.com`、带 `/v1` 或带 `/api/v3` 的地址；网关会统一拼接原生路径。
- 模型 endpoint 标识为 `volcengine-video`，默认信息为 `{"path":"/api/v3/contents/generations/tasks","method":"POST"}`。模型编辑模板、价格页筛选和调用示例已登记该类型。
- 如模型已有自定义 `supported_endpoint_types`，需要在该配置中加入 `volcengine-video`；自定义配置优先于渠道默认值。
- 客户端提交对外模型名；渠道模型映射填写 ArtsAPI 实际支持的模型名。原生查询响应的 `model` 保留上游值。
- 原有 `/v1/video/generations` 和 `/v1/videos` 调用继续使用原适配器。

## 调用示例

`BASE_URL` 为 Mozia API 的服务地址，`MODEL` 为已配置的对外模型名称。

```bash
curl "$BASE_URL/api/v3/contents/generations/tasks" \
  -H "Authorization: Bearer $MOZIA_API_KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"model\": \"$MODEL\",
    \"content\": [{\"type\":\"text\",\"text\":\"一只猫在阳光下的花园中奔跑\"}],
    \"duration\": 5,
    \"generate_audio\": false
  }"
```

创建响应保留原生形状，例如 `{"id":"cgt-..."}`，不增加 OpenAI Video 或 Mozia 的 data 包装。将返回的 id 设置为 `TASK_ID`：

```bash
curl "$BASE_URL/api/v3/contents/generations/tasks/$TASK_ID" \
  -H "Authorization: Bearer $MOZIA_API_KEY"

curl -G "$BASE_URL/api/v3/contents/generations/tasks" \
  -H "Authorization: Bearer $MOZIA_API_KEY" \
  --data-urlencode "page_num=1" \
  --data-urlencode "page_size=20" \
  --data-urlencode "filter.status=succeeded"

curl -X DELETE "$BASE_URL/api/v3/contents/generations/tasks/$TASK_ID" \
  -H "Authorization: Bearer $MOZIA_API_KEY"
```

## 协议与数据归属

除已配置的渠道模型映射、参数覆盖外，请求 JSON 字段保留，包括原生 `content` 的图片、视频、音频角色及后续扩展字段。显式的 `false`、`0` 不丢失，省略 duration 不会自动补值，`duration=-1` 交由上游按模型能力处理。

查询保留原生状态、`content.video_url`、`last_frame_url`、`usage` 和扩展字段；上游标准 error 对象及 HTTP 错误状态保留。本地鉴权、路由、网络和数据库错误也使用 `{"error":{...}}`，错误码属于 Mozia 网关。

查询、删除和列表只允许访问当前用户通过本原生接口创建的任务，并遵守令牌的模型限制。调用使用提交时保存的上游密钥。样片引用 `content[].draft_task.id` 也校验用户归属，并固定原提交渠道和密钥。

列表响应为 `{"items":[...],"total":N}`，汇总当前用户最近七天的原生任务，按创建时间倒序分页。支持：

- `page_num`、`page_size`：1–500，默认 1、20。
- `filter.model`、`filter.status`、`filter.service_tier`：转发给上游，模型过滤使用上游模型名称。
- `filter.task_ids`：多个 ID 使用重复查询参数。

列表按提交渠道及密钥分组，每批最多 50 个自有任务 ID，核验上游返回记录的归属后再合并分页。无法完整读取某个分组时返回错误，避免把部分结果作为完整列表。上游已删除或过期的任务不出现在列表中；上游直接创建、旧 v1 路径创建的任务不自动导入。

## 计费与取消

复用现有任务预扣、钱包/订阅、参数计费和异步结算。按次定价保持按次结算；token 或 token_parametric 模式可依据上游返回的 usage 调整费用。自动时长 `-1` 应搭配按次或 token 定价；按秒配置仍必须能解析有效时长。

HTTP 查询、列表和后台轮询共享终态 CAS，仅成功抢到状态转换的一方结算或退款。DELETE 前先读取并处理已完成任务的 usage；DELETE 成功本身不触发退款，后续查询确认 `failed/cancelled/expired` 后才退款。

原生任务不套用本地统一超时退款阈值，上游的 expired 状态才是退款依据。如果上游在终态/usage 被读取前直接删除任务，404 不视为失败，也不自动退款；保留已有扣费和本地记录，需人工对账。删除操作不删除本地财务记录。

## 验证与资料

本次用本地模拟上游验证协议、鉴权、模型映射、字段保留、归属隔离、列表分页、取消/删除与结算幂等；未使用真实 ArtsAPI 密钥发起计费请求。真实模型可用性和参数限制仍以该上游账号为准。

新增测试（包括 race 检查）、相关后端包回归、默认前端 typecheck/lint/构建已通过。仓库全量检查仍有既存问题：

- `TestMoziaH3ChannelRegistration` 的预期模型列表少了现有 t2va 模型；relay 包其余测试通过。
- Classic 前端的 `date-fns-tz` 引用了当前 `date-fns` 未导出的子路径，导致 Classic 构建失败，无法提供主程序所需的完整嵌入资源。
- `go build ./...` 还会扫描本地未跟踪的 `output/newapi-upstream-review-20260908/extra-probe.go`，其中引用了当前仓库没有的 relaykit 包。这些原有文件和依赖配置未改动。

- [ArtsAPI 原生协议](https://ai.artsapi.com/docs/volcengine-ark/doc0024)
- [火山创建任务](https://docs.volcengine.com/docs/82379/1520757?lang=zh)
- [火山查询任务](https://docs.volcengine.com/docs/82379/1521309?lang=zh)
- [火山任务列表](https://docs.volcengine.com/docs/82379/1521675?lang=zh)
- [火山取消/删除任务](https://docs.volcengine.com/docs/82379/1521720?lang=zh)
