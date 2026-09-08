# Claude 协议转换：上游移植取舍（2026-09-08）

基于本地 `439545377`，对照 new-api 上游 `bee45b58a3c0b77e8dc81e6b5aeb4474aa9058d1`，选择性移植协议修复。保留本仓库 DTO、渠道配置、计费和路由架构。

## 移植方式与取舍

| 问题 | 最终处理 | 原因 |
| --- | --- | --- |
| Kimi K3 动态工具消息 | 移植 [`6e10f9bc9`](https://github.com/QuantumNous/new-api/commit/6e10f9bc927a4eae889864a6ef601359d53526b9)；系统提示词两处修改使用 `git apply --3way`，DTO 改动适配回本地 | 保留 `messages[].tools`，工具消息不额外输出 `content:null`，不被系统提示词覆盖，纳入 token 估算 |
| developer 角色、工具 strict、无参数工具 | 选择性移植 [`0ed497f06`](https://github.com/QuantumNous/new-api/commit/0ed497f066a68613375124303ef54f220267b334) 中的行为 | developer 转入 Anthropic system；strict 显式 false 不丢失；无参数工具不再被删除 |
| adaptive thinking 被 reasoning_effort 覆盖 | 采用上游按模型选择 thinking 类型的思路，保留本地 Opus 4.7/4.8 可见摘要配置 | 避免 adaptive 被覆盖为不兼容的 enabled；清除不兼容采样参数 |
| 手动思考预算越过输出上限 | 适配上游预算边界逻辑 | 自动预算限制为 `1024 <= budget_tokens < max_tokens`；显式非法预算或过小上限返回 400，不擅自增加调用方输出额度 |
| DeepSeek V4 / Kimi K3 的 thinking 和 effort | 保留本地实现 | DeepSeek 保留 enabled/disabled；Kimi K3 不发送其不接受的 thinking 开关；保留各模型 effort 映射 |
| Claude → OpenAI 的思考历史及混合工具上下文 | 保留本地修复 | 工具轮次需要完整 reasoning_content、文本、tool_calls 与 tool_result；上游通用转换未完整覆盖 |
| OpenAI → Claude 的连续 assistant 工具消息 | 删除会丢工具调用的旧文本合并逻辑 | 保留每条消息及工具调用关联，避免生成孤立 tool_result |
| 多段普通响应、缓存标记、结构化输出 | 采用上游文本拼接并补足本地公共转换器 | 拼接全部 text/thinking；DTO 解析保留 cache_control；OpenAI json_schema 转为 Claude output_config.format 并与 effort 共存 |
| Claude → OpenAI 流式工具索引 | 移植上游按 content block 映射独立工具索引的状态逻辑 | 前置 thinking/text 不再让第一个 tool_calls.index 从 2 开始；每次响应创建独立状态，重试不继承旧索引 |
| Claude 通道处理 Responses 请求 | 复用本地 Responses ↔ Chat 转换，再接 Claude 转换器 | 接通请求、普通响应、流式响应；不引入上游独立 RelayKit 模块和整套注册框架 |
| Responses 摘要与流式事件 | 适配上游 summary、sequence_number、done 文本/参数修复，并补齐 content/summary part 生命周期 | 官方 SDK 需要先建立内容块才能消费文本 delta；在共用转换器修复，避免其他转换路径继续输出旧字段 |
| 签名被误当作思考换行 | 转 Chat/Responses 时不把 signature_delta 写入 reasoning_content；原生 Messages 保留签名事件 | 签名是不可解释元数据，不是模型生成的文本 |

这些取舍依据 [Claude Code 网关协议](https://code.claude.com/docs/en/llm-gateway-protocol)、[Claude thinking/effort](https://platform.claude.com/docs/en/build-with-claude/effort)、[结构化输出](https://platform.claude.com/docs/en/build-with-claude/structured-outputs)、[DeepSeek 思考模式](https://api-docs.deepseek.com/guides/thinking_mode/) 和 [Kimi K3 官方说明](https://github.com/MoonshotAI/Kimi-K3)。Responses 的内容块顺序同时对照 [OpenAI Python SDK 实现](https://github.com/openai/openai-python/blob/main/src/openai/lib/streaming/responses/_responses.py)。

## 验证

- `go test ./relay/channel/claude ./relay/channel/openai ./relay/channel/aws ./relay/helper ./service ./service/relayconvert ./dto -count=1` 通过。
- `go test ./relay/... ./service/... ./dto/... -count=1` 仅有已有的 `TestMoziaH3ChannelRegistration` 失败：测试期待两个模型，当前注册三个。已在未修改的 `439545377` 工作树单独复现。
- `go test ./... -run '^$'` 中业务包编译通过；根 main 包因工作树没有生成的 `web/classic/dist` 无法编译。本次没有构建前端产物。
- 四个目标模型通过真实 OpenAI adaptor 的离线请求转换验证：`deepseek/deepseek-v4-flash`、`deepseek/deepseek-v4-pro`、`moonshotai/kimi-k3`、`moonshotai/kimi-k3-new`。工具关联、并行控制、思考历史、文本与 effort 均保留；Kimi 有意不发送 thinking 开关。
- SSE 回归通过：前置思考/文本后的两个工具、参数分片、重复使用 RelayInfo 时状态重置、缓存用量转换、原生签名透传，以及 Responses 缺失 message_stop 时不发送 response.completed。对外 OpenAI 用量包含缓存输入，内部计费仍保留 Anthropic 未缓存输入语义。
- 官方 OpenAI Python SDK 3.8.0 连续消费 23 个转换后的 Responses 事件，最终文本、摘要、两个工具参数和用量断言通过；官方 Anthropic Python SDK 1.4.0 消费 16 个转换后的 Messages 事件，思考、文本、并行工具参数和用量断言通过。所有验证均未调用收费模型或生产服务。

## 明确边界

- 本次不是完整上游版本升级；未移植与协议修复无关的渠道、UI、数据库或计费重构。
- 跨 OpenAI Chat 格式的 Anthropic 带签名 thinking 完整回放、文档和多模态 tool_result 转换仍有缺口；本次没有宣称全格式无损互转。Kimi 原生 `messages[].tools` 支持也不等于已实现 Anthropic 服务端 tool_search 的跨厂商模拟。
- Responses 的 `previous_response_id`、`conversation` 等服务端状态不能在无状态 Chat/Claude 桥接中重现，继续明确返回 400。供应商专有工具不自动降级为普通函数。
- `/v1/messages/count_tokens` 按 Claude Code 官方协议是可选接口。本次不加入本地近似计数冒充供应商精确计数；Claude Code 可走自身回退逻辑。
- 尚需部署后用目标模型进行真实 Claude Code 多轮工具调用验收，离线转换通过不代表所有上游模型能力均支持。
