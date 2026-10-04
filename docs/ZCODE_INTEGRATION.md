# ZCode 原生接入说明

本文档描述 Sub2API 对 ZCode（Z.AI / BigModel）OAuth 授权、Start Plan 免费额度与 Coding Plan 的原生接入实现。面向维护者；不包含任何真实凭据。

## 设计边界

- 不新增平台：ZCode 复用既有 `platform=zhipu` 账号，通过 `type=oauth` + `credentials.auth_mode=zcode_oauth` 识别。
- 不新增数据库迁移：敏感凭据存入既有 `accounts.credentials`（加密），状态存入 `accounts.extra`。
- 不引入 Bun/Node/sidecar 等额外运行时；协议逻辑集中在 Go 模块 `backend/internal/pkg/zcode/`。
- 既有智谱 API Key 账号（payg / coding）行为完全不变；ZCode 只在 `auth_mode=zcode_oauth` 时进入专用路径。
- 请求协议转换继续由既有协议 Pipeline 负责；ZCode 模块只处理认证、身份头、端点路由、CAPTCHA 与出站封装。

## 账号数据约定

`credentials` 由服务端在授权会话完成后写入：

```json
{
  "auth_mode": "zcode_oauth",
  "account_mode": "start",
  "zcode_provider": "zai",
  "zcode_auto_claim": false,
  "zcode_device_id": "内部设备标识",
  "zcode_tokens_encrypted": "服务端加密后的 token JSON",
  "api_protocol": "anthropic"
}
```

- `account_mode`：`start`（Start Plan）或 `coding`（Coding Plan）。
- `zcode_provider`：`zai` 或 `bigmodel`。
- `zcode_tokens_encrypted` 使用与 TOTP 相同的 AES-GCM 加密器；前端永远不接触该字段，编辑账号时由 `MergePreservingSensitiveCreds` 保留。
- 前端创建/编辑只提交 `zcode_oauth_session_id`（一次性授权会话标识），服务端消费会话后写入密文；会话绑定发起管理员与目标账号，消费后失效。

`extra` 使用的键：

- `zcode_state`：授权状态（如 `login_required`）与最近检查时间。
- `zcode_quota`：额度快照（余额、窗口、到期/重置时间）。
- `zcode_claim`：领取状态（结果、活动、下次尝试时间）。

## 模块划分

### 协议层 `backend/internal/pkg/zcode/`

| 文件 | 职责 |
| --- | --- |
| `types.go` | Provider、套餐、token、JWT 解析、错误分类、Store 接口 |
| `client.go` | 固定超时、JSON envelope、Retry-After 解析、错误不携带上游原文 |
| `oauth.go` | 服务端 init/poll、授权 URL 官方域名校验、Z.AI / BigModel 分支 |
| `identity.go` | App Version、User-Agent、ZCode 头与平台/OS 头 |
| `routing.go` | 端点映射缓存、singleflight、失败保留旧映射；目标必须 https 且在域名白名单内 |
| `quota.go` | Start Plan 余额、Coding Plan 多窗口、remaining/total/percent、套餐状态 |
| `claim.go` | 活动 preview、claim、starts_at、最低版本与固定结果分类 |
| `captcha.go` / `captcha_browser.go` | CAPTCHA 配置缓存与并发上限；受控 Chromium 执行官方动态 SDK（可取消、匿名、拒绝带用户名密码的代理） |
| `signing.go` | Coding Plan 的 HKDF、AES-GCM、Ed25519、PoW 与签名缓存 |
| `body.go` / `zcode_system.json` | Start Plan 请求体/system 兼容处理 |
| `forward.go` | 只处理 ZCode 认证、headers、端点、CAPTCHA 与出站 envelope |
| `store_redis.go` | OAuth 会话、分布式锁、原子 Take、所有者校验释放 |
| `process_windows.go` / `process_unix.go` | 无窗口 Chromium 子进程 |

Chromium 的用户目录、配置目录、缓存和 crashpad 均使用一次性匿名临时目录，兼容服务 HOME 不可写的部署；现有 `no_sandbox` 默认保持 `false`。

上游来源与同步：

- 上游仓库 `TriDefender/zcode-api`，pin 记录在 `upstream.json`（含 commit 与文件映射）。
- `tools/check_zcode_upstream.py` 对 pinned ref 做分类比对并输出审查清单；只有显式 `--update-pin --reviewed` 才更新 pin，不自动修改本地实现。
- `NOTICE.md` 记录上游许可声明核查结果。
- `tools/zcode_parity_reference.mjs` 生成 Node/Go 签名对照向量，测试数据在 `testdata/`。

### 业务接入 `backend/internal/service/zcode_*.go`

- `zcode_account.go`：`IsZCodeOAuth()` 判定、创建/编辑校验、调度资格（`login_required` 与额度耗尽快照）。
- `zcode_oauth.go`：管理员绑定授权会话（Redis 加密存储）、poll、一次性保存消费（`PrepareCredentials` + 幂等 `finish`）。
- `zcode_service.go`：解密 token、原生 `RoundTrip`、错误映射为 `zcode_<kind>`、账号冷却与状态持久化。
- `zcode_tasks.go`：额度查询缓存、后台轮转 worker（单循环 + 固定 worker 数，不为每个账号起 goroutine）、自动/手动领取（Redis 周期锁 + 持久化 `next_attempt`）。
- `zcode_quota_adapter.go`：接入既有 CN provider 额度查询入口。
- `zcode_state.go`：以「密文 + 供应商 + 套餐 + 代理」为身份版本的条件更新入口。

### 数据库条件更新

`backend/internal/repository/account_zcode_state.go` 的 `UpdateZCodeStateIfIdentityUnchanged` 在单条 CTE 语句中原子完成：

- 仅当账号的平台、类型、`zcode_tokens_encrypted`、`zcode_provider`、`account_mode`、`proxy_id` 全部与更新发起时一致才写入（`proxy_id` 用 `IS NOT DISTINCT FROM` 处理 NULL）。
- 同事务向 `scheduler_outbox` 写入一条 `account_changed` 事件。
- `CooldownUntil` 非空时写入冷却；`ClearOwnCooldown` 只清除 `zcode_%` 前缀的冷却原因。
- `extra` 按 JSONB 合并，值为 `nil` 时写 JSON null（读取端按无状态处理）。
- 身份已变化时返回 `(false, nil)`：后台任务只记录，不用旧凭据重试。

### 管理端与前端

管理端接口（继承管理员鉴权）：

- `POST /admin/zhipu/oauth/start`、`POST /admin/zhipu/oauth/poll`
- `GET /admin/accounts/:id/zcode/status`、`GET /admin/accounts/:id/zcode/quota`
- `GET /admin/accounts/:id/zcode/claim/preview`、`POST /admin/accounts/:id/zcode/claim`

前端：

- 智谱账号创建/编辑可选择 API Key 或 ZCode OAuth，选择授权来源与套餐。
- OAuth 流程只接触授权 URL、会话 ID 与状态；不接触 JWT、access token 或 CAPTCHA token。
- 额度卡片优先展示后端快照，显式刷新走后端缓存接口，挂载时不直连上游。

## 配置

Start Plan 的请求验证与套餐领取需要 Chromium 或 Google Chrome。浏览器应安装在后端所在的运行环境，并允许服务用户执行；容器部署需要在容器内提供浏览器。自动探测未找到可执行文件时，使用 `chromium_path` 配置实际路径。

`gateway.zcode.*`（均有默认值，见 `internal/config/config.go`）：

| 键 | 默认 | 说明 |
| --- | --- | --- |
| `app_version` | 上游当前版本 | ZCode 客户端版本标识 |
| `origin` | 上游默认源 | API 源站 |
| `chromium_path` | 空（自动探测） | CAPTCHA solver 使用的浏览器路径 |
| `no_sandbox` | `false` | Chromium 沙箱开关 |
| `identity_platform` / `identity_os_version` | 上游默认 | 身份头 |
| `quota_cache_seconds` | `60` | 额度查询缓存 |
| `claim_interval_seconds` | `300` | 自动领取检查间隔 |

## 错误与调度语义

- ZCode 原生 transport 的失败响应统一为 `zcode_<kind>` 错误类型且不携带上游原文；`RateLimitService` 检测到该前缀后跳过普通错误策略，避免覆盖已分类状态。
- 401 → `login_required`（账号置为不可调度，等待重新授权）；429/额度耗尽/验证失败/网络超时分别进入限流、额度冷却、验证冷却与临时不可用。
- 重新授权成功后清除 `zcode_` 前缀冷却并重置额度/领取快照。
- 更换授权来源或套餐但凭据未变化时拒绝保存（`ZCODE_RELOGIN_REQUIRED`）。

## 测试

- `internal/pkg/zcode`：协议、OAuth、JWT、路由、额度、claim、CAPTCHA、Redis 原子锁、签名向量（默认门禁）。
- `internal/service`：`TestZCodeValidationAndLegacyRegression` 在默认门禁；依赖 miniredis 的会话/多实例/额度/失败脱敏测试位于 `zcode_service_redis_test.go`（`//go:build unit`，随 unit 门禁执行，与既有 redis 测试同约定）。
- 前端：`src/components/account/__tests__/ZCodeOAuth.spec.ts` 及创建/编辑弹窗回归。
