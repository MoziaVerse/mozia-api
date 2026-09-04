# 代理商挂牌：文档入口策略

## 配置入口

Mega → 代理商管理 → 配置 → **挂牌配置**。

品牌名、Logo、Favicon、文档入口、备案和版权在同一标签页保存。域名绑定仍在「基本信息」中独立修改，避免保存展示信息时意外切换域名。

文档策略：

- `default`：沿用官方文档，保留模型等具体文档路径。现有代理商默认不变。
- `hidden`：隐藏门户中的文档入口；历史 `/docs/*`、`/blog/*` 入口返回 404。
- `custom`：所有文档入口跳到 `documentation_url` 指定的完整 HTTPS 地址，不拼接模型路径。

覆盖 Matrix 导航、页脚、首页、控制台使用指南、模型文档、公告文档外链、历史跳转和 `llms.txt`。公告正文摘要与紧急通知保留；第三方活动与应用入口不受影响。用户协议、支付协议和项目自带 README 不在本次范围。

隐藏入口不限制用户直接访问官方文档站；替换链接不修改目标文档内容，也不提供文档域名托管。

## 数据契约

- `mozia-api` 的 `resellers.documentation_mode` / `documentation_url` 是事实源。
- Mega 通过现有 `PUT /api/internal/v1/platform/resellers/:id/presentation` 保存，沿用平台权限与审计。
- 两个字段必须一起提交；旧客户端同时省略时保留已保存策略。非 custom 模式清空 URL。
- custom 地址必须是完整 HTTPS URL，禁止凭证、空白控制字符和反斜杠。不从后端抓取自定义 URL。
- 列表与 presentation 返回策略。Matrix backend → `/api/brand` → SSR BrandProvider 按请求 Host 消费；不得写入前端模块级租户状态。
- 客户端刷新页面后生效，无需逐域名修改 Matrix `.env` 或 SSO 配置。
- 品牌解析失败时，非官方域名隐藏文档入口，不回退到官方文档。

## 发布顺序

1. 部署 `mozia-api`，由现有 GORM AutoMigrate 添加两列。旧调用方兼容。
2. 部署 Matrix backend 与 frontend，使其识别新字段。
3. 部署 Mega 后端与前端，再通过「挂牌配置」修改策略。
4. 在目标域名检查导航、首页、模型详情、公告、`/docs/*`、`/blog/*`、`llms.txt`；同时检查官方域名未受影响。

新历史跳转使用 302 + `Cache-Control: no-store`。用户浏览器若已缓存旧版 308，需要清除旧重定向缓存再验证。

## 回归检查

```sh
# mozia-api
go test ./router -run 'TestReseller(Admin|Documentation)' -count=1

# Matrix
bun test src/lib/model-docs.test.ts src/lib/brand.server.test.ts src/routes/documentation-policy.test.ts src/components/layout/documentation-policy.test.tsx backend/src/router/brand.test.ts backend/src/service/reseller-registration.test.ts
bun run typecheck
bun run build

# mozia-mega
bun test apps/server/src/routes/mozia-batch-assign.test.ts
bun run typecheck
bun run build
```
