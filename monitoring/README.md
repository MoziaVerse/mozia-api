# Mozia-API 生产级监控与告警部署手册（方案 A）

本目录实现了完整的两维监控架构（**方案 A：业务机极轻量采集 + 独立监控机集中存储/展示/外网拨测**），开箱即用，包含两张自动加载的 Grafana 大盘、分级飞书签名告警与零停机滚动重启能力。

---

## 1. 整体架构（方案 A）

```text
┌───────────────────────────── 业务机 (mozia-api Host) ─────────────────────────────┐
│  Caddy (TLS + /readyz 主动探活 + 屏蔽 /metrics)                                    │
│   ├──▶ mozia-api (primary :3369, 内网 metrics :9091)                              │
│   ├──▶ mozia-api-replica (slave :3371, 内网 metrics :9092)                        │
│   ├──▶ postgres-dev (只读监控账号 mozia_monitor) ◀── postgres_exporter (:9187)    │
│   ├──▶ redis-newapi                             ◀── redis_exporter (:9121)        │
│   └──▶ 宿主机 & Docker                          ◀── node_exporter (:9100)         │
│                                                 ◀── cAdvisor (:8088)              │
└──────────────────────── 仅绑定内网 IP，禁止暴露公网 ──────────────────────────────┘
                                       ▲
                              内网定时抓取 (15s)
                                       │
┌──────────────────── 独立监控机 (推荐 2C4G, 40GB+ 磁盘) ───────────────────────────┐
│  Prometheus (留存 30 天) ──▶ Alertmanager ──▶ feishu-webhook (HMAC-SHA256 签名)   │
│         ▲                                                   │                     │
│  Grafana (:3030, 预置大盘一 & 大盘二)                       ▼                     │
│  Uptime Kuma (:3001, 公网黑盒拨测 + 证书到期 + 状态页)  飞书告警群                 │
└───────────────────────────────────────────────────────────────────────────────────┘
```

---

## 2. 业务机部署步骤

### 2.1 在 PostgreSQL 中创建只读监控账号（幂等）

使用 [init-monitor-user.sql](postgres/init-monitor-user.sql)，在业务机上运行（替换 `<STRONG_PASSWORD>` 为随机高强度密码，**不要提交到 Git**）：

```bash
PG_ADMIN_USER=$(grep -E '^POSTGRES_USER=' /home/server/app/dbInstance/postgres/.env | cut -d= -f2-)
docker exec -i postgres-dev psql -U "$PG_ADMIN_USER" -d mozia_api_db \
  -v monitor_password="<STRONG_PASSWORD>" \
  < monitoring/postgres/init-monitor-user.sql
```

### 2.2 更新 `mozia-api` 环境变量（`.env`）

参照 [.env.mozia.example](../.env.mozia.example)，在业务机 `.env` 中补充：

```dotenv
# 绑定到业务机内网 IP（绝对不要用 0.0.0.0）
MOZIA_API_METRICS_BIND=172.16.x.x:9091
MOZIA_API_REPLICA_METRICS_BIND=172.16.x.x:9092
# 跨机内网抓取建议配置 Bearer Token
METRICS_TOKEN=<随机高强度 Token>
# 重点大客户监控名单（支持 user_id 或 user_id:别名）
MONITOR_VIP_USERS=6218:VIP-6218
# 后续如果把大客户归入专门分组，也可以补充：
# MONITOR_VIP_GROUPS=vip,enterprise
SHUTDOWN_DRAIN_SECONDS=5
SHUTDOWN_TIMEOUT_SECONDS=60
```

> **免重启热更新大客户名单**：除了环境变量 `MONITOR_VIP_USERS`，后续随时可以在数据库 `options` 表更新 `MonitorVIPUsers`（例如 `6218:VIP-6218,7001:客户B`）或 `MonitorVIPGroups`，主从双实例会在 `SYNC_FREQUENCY`（默认 60 秒）内自动热生效，无需重启服务。

### 2.3 启动业务机轻量采集端（Exporters）

```bash
cd monitoring
cp .env.monitoring.example .env.monitoring
# 编辑 .env.monitoring：填入 AGENT_BIND_IP、PG_EXPORTER_DSN 等

# 如果宿主机尚未安装 systemd node_exporter，请加上 --profile node-exporter：
docker compose --env-file .env.monitoring -f docker-compose.agent.yml --profile node-exporter up -d

# 如果宿主机已自带 systemd node_exporter（监听 :9100），直接运行：
docker compose --env-file .env.monitoring -f docker-compose.agent.yml up -d
```

### 2.4 配置 Caddy 反向代理（安全摘流 + 屏蔽内部路径）

参考 [Caddyfile.mozia-api.example](caddy/Caddyfile.mozia-api.example)：
- 使用 `health_uri /readyz`、`health_interval 3s`、`unhealthy_status 503`；
- 必须配置 `flush_interval -1` 保障 SSE 流式输出不被缓冲；
- 在公网域名拦截 `/metrics*` 与 `/debug/pprof*` 返回 `404`。

### 2.5 零停机滚动重启 / 发版

```bash
./scripts/rolling-restart-mozia.sh --build
```

脚本按 `检查 primary 就绪 -> 重启 replica 并等 /readyz 200 -> 等待 Caddy 重新纳入 -> 重启 primary 并等 /readyz 200` 顺序执行；容器停机时先切 `/readyz` 为 503 摘流 5 秒，再宽限 60 秒等待在途 SSE 吐字完成，并自动把 `batchUpdate` 计费缓存与 `perfmetrics` 刷盘后退出。

---

## 3. 独立监控机部署步骤（推荐 2C4G，40G+ 磁盘）

将 `monitoring/` 目录同步到独立监控机后执行：

```bash
cd monitoring
cp .env.monitoring.example .env.monitoring
# 编辑 .env.monitoring：
#   - BUSINESS_HOST_IP=<业务机内网 IP>
#   - METRICS_TOKEN=<与业务机一致>
#   - GRAFANA_ADMIN_PASSWORD=<强密码>
#   - FEISHU_WEBHOOK_URL & FEISHU_SECRET=<飞书机器人 Webhook 与签名密钥>
#   - ALERT_ENV=prod

# 生成 Prometheus 抓取目标文件（不入 Git）
./scripts/render-targets.sh .env.monitoring

# 启动全套监控栈（Prometheus + Alertmanager + Feishu Webhook + Grafana + Uptime Kuma）
docker compose --env-file .env.monitoring -f docker-compose.monitoring.yml up -d --build
```

测试飞书告警通道是否畅通：

```bash
docker exec mozia-feishu-webhook /usr/local/bin/feishu-webhook --send-test
```

访问 Grafana（推荐通过 SSH 隧道或内网 VPN，避免直接裸露公网）：

```bash
ssh -L 3030:127.0.0.1:3030 <监控机>
# 浏览器打开 http://127.0.0.1:3030，进入「Mozia Monitoring」文件夹查看：
#   - 大盘一：服务器硬件 & 基础底座监控
#   - 大盘二：mozia-api 多实例健康 & 大客户 Token SLA
```

---

## 4. Uptime Kuma 外网拨测与状态页建议

通过 SSH 隧道 `ssh -L 3001:127.0.0.1:3001 <监控机>` 打开 `http://127.0.0.1:3001`，建议新建 3 个监控项并关联飞书通知：

1. **HTTPS 入口 & 证书到期监控**：类型 `HTTP(s)`，URL `https://<你的域名>/readyz`，开启「证书到期通知（提前 14 天）」，周期 60 秒。
2. **模型端到端真实对话拨测**：类型 `HTTP(s) - Keyword`，Method `POST`，URL `https://<你的域名>/v1/chat/completions`，Header 带专用低额拨测 Key，Body `{"model":"<便宜主力模型>","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`，关键字校验 `"choices"`，周期 180 秒。
3. **客户状态页（Status Page）**：在 Uptime Kuma 创建 Status Page，并在 `mozia-api` 后台开启 Uptime Kuma 状态页集成，即可直接展示给企业大客户。
