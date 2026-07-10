# Repository Guidelines

## 当前文件与 worktree 状态

本仓库的代理与贡献者指南使用 `AGENTS.md`。旧 `AGENT.md` 不应恢复；如果某个 checkout 仍显示 `AGENT.md` 删除标记，不要为了清理工作区而还原它。

开始任何编辑前先确认真实位置：

```powershell
git status --short --branch
git worktree list
git rev-parse --short HEAD
```

当前主业务集成 worktree 是：

```text
C:\Users\liang\Desktop\Code\sub2api-main\.worktrees\subscription-rolling-quotas-upstream-20260605
```

该 worktree 分支为 `codex/subquota-upstream-20260605`，承载 OpenCode Go 平台级 provider、Channel Monitor、计费/配额、前端闭环、发布守卫、OpenCode Go console usage 和 staged onprs 发布准备等未合并业务改动。根目录 `C:\Users\liang\Desktop\Code\sub2api-main` 是 `master` 基线和指南上下文，不要把产品、支付、订阅、前端或部署相关改动默认落到根 `master`。

`.gitignore` 应保持不忽略 `AGENTS.md`，并忽略本地构建/发布产物，例如 `artifacts/`、`tools/__pycache__/` 和其他 `__pycache__/`。发布二进制、release guard 输出、临时脚本和缓存不要提交；业务源码、迁移、测试、前端组件与文档变更按任务范围提交。

## 项目结构

后端位于 `backend/`：入口在 `backend/cmd/`，业务逻辑在 `backend/internal/`，Ent 代码在 `backend/ent/`，迁移在 `backend/migrations/`。前端位于 `frontend/`，使用 Vue 3、Vite、TypeScript、Pinia、Tailwind、ESLint 与 Vitest，源码在 `frontend/src/`。发布、部署和辅助脚本位于 `deploy/`、`tools/`、`release/` 与根 `Makefile`；文档位于 `docs/` 和 `README*.md`。

## 构建与测试命令

- `make build`：构建后端和前端。
- `make build-backend`：调用 `make -C backend build` 生成 Go 服务二进制。
- `make build-frontend`：运行 `pnpm --dir frontend run build`，用于前端构建和 Go embed 前置验证。
- `make test`：运行后端测试和前端检查。
- `make test-backend`：运行 `go test ./...` 与 `golangci-lint run ./...`。
- `make test-frontend`：运行 ESLint、Vue 类型检查和关键 Vitest 用例。
- `pnpm --dir frontend run dev`：本地前端开发服务。
- `corepack pnpm --dir frontend install --frozen-lockfile`：新环境安装前端依赖的首选命令。
- `make secret-scan`：分享或提交前检查敏感信息。

窄改动先跑定向测试，再跑相关扩大测试。涉及计费、鉴权、网关、迁移或发布路径时扩大后端测试；涉及 UI 时补充 Vitest 或截图验证。Go embed 或发布前必须先完成前端 build，再构建后端 Linux amd64 二进制。

## 编码风格与提交

Go 代码遵循 `gofmt`、短小小写包名和 `*_test.go` 命名。前端组件使用 `PascalCase.vue`，函数和变量使用 `camelCase`，测试优先放在就近 `__tests__/`，文件名使用 `.spec.ts`。不要提交构建产物、release artifacts、缓存目录或格式化噪音，除非改动本身需要重新生成。

提交标题沿用简短 conventional 风格，例如 `docs: ...`、`chore: ...`、`fix: ...`、`feat: ...`。保持提交范围清晰；大分支提交时可以提交完整业务集，但仍要排除 `artifacts/`、`__pycache__/`、本地 release 二进制和临时日志。

## OpenCode Go 账号限流语义

OpenCode Go 在本分支中是独立平台 `opencode_go`，不是 OpenAI 或 Anthropic 的别名。默认官方 API root 是 `https://opencode.ai/zen/go/v1`，对外首版能力围绕 `/v1/chat/completions`、`/v1/messages`、`/v1/models`；Channel Monitor provider 也使用 `opencode_go`，通过 `api_mode=chat_completions|messages` 区分探活协议。

OpenCode Go 模型协议不要猜。`chat_completions` 模型走 `/chat/completions`，`messages` 模型走 `/messages`；后台账号 credentials 的 `model_protocols` 优先，其次才是内置官方模型协议表和模型 family fallback。常见内置分组：`deepseek`、`kimi`、`glm`、`mimo` 多为 chat completions；`qwen`、`minimax` 多为 messages。若某模型 401/403、空响应或长时间无响应，先查账号协议映射、渠道模型映射和监控 api_mode，而不是把它折成 OpenAI provider。

OpenCode Go official console usage snapshot 要按 sub2api 原有账号级限流/调度模型处理：仅当 `opencode_go_usage_source=official_console` 且 console auth 状态为 `ready` 时，任一 5h、7d、30d 窗口 `used_percent >= 100` 且 reset 时间在未来，就视为账号级限流。该状态应让 `IsQuotaExceeded()` 返回 true、让账号不可调度，并同步 `rate_limit_reset_at`；多个窗口同时满额时使用最晚 reset 时间。estimated snapshot 只用于展示/估算，不触发账号限流；console auth expired 也不能误判为可调度限流。

相关回归优先跑：

```powershell
Push-Location backend
go test ./internal/service -run 'TestOpenCodeGoOfficialUsageExhaustion|TestAccountUsageServicePersistOpenCodeGoConsoleSummaryMarksExhaustedAccountRateLimited' -count=1
go test ./internal/service -run 'OpenCodeGo|AccountUsageServicePersistOpenCodeGoConsoleSummary|QuotaExceeded|AccountIsSchedulable|ChannelMonitor' -count=1
go test -tags unit ./internal/service -run 'OpenCodeGo|QuotaExceeded|AccountIsSchedulable|Sticky|SchedulerSnapshot' -count=1
Pop-Location
corepack pnpm --dir frontend exec vitest run src/components/account/__tests__/AccountStatusIndicator.spec.ts
```

## OpenCode Go 与监控排查

OpenCode Go 监控中，`upstream returned 2xx but response text was empty` 表示 HTTP 路径和鉴权通常已经通过，失败点更可能是模型返回结构、输出预算或响应文本抽取。当前监控实现为 OpenCode Go 使用 `monitorOpenCodeGoChallengeMaxTokens = 512` 与 `temperature = 0`，并用专用抽取器聚合 `choices[].message.content`、content blocks、`output_text` 等可见文本，同时跳过 reasoning/thinking 块。

Antigravity Gemini 监控也有 thinking 模型边角：Gemini 3 类模型可能把小输出预算消耗在 thinking 上，导致 challenge 只返回首位数字或取到 thought 文本。当前实现使用 `monitorAntigravityGeminiChallengeMaxTokens = 1024`，对 `gemini-3*` 注入 `thinkingConfig.includeThoughts=false` 与 `thinkingLevel=low`，并跳过 `thought: true` parts。遇到 `challenge mismatch (expected ..., got "9")` 这类问题时，先查输出预算、thinking 配置和文本抽取，不要直接判定上游不可用。

模型列表只能作为提示，不能单独证明模型可调用。Cherry Studio 或 OpenCode 客户端自动拉到的 `/models` 结果可能来自静态列表、前门默认分组或账号 `model_mapping` 聚合；排查“为什么模型列表不对”时，要区分 public model、upstream model、账号 credentials.model_mapping、分组路由和实际短调用结果。

## 线上与部署安全

线上主机别名为 `onprs`，服务为 `sub2api.service`，`WorkingDirectory=/opt/sub2api`，`ExecStart=/opt/sub2api/sub2api`。除非用户明确要求，不要从 Codex 重启、停止或替换线上服务。只读检查可以运行：

```bash
systemctl is-active sub2api
sha256sum /opt/sub2api/sub2api
/opt/sub2api/sub2api --version 2>&1 | head -1
curl -sS -o /dev/null -w "health_http=%{http_code}\n" http://127.0.0.1:8080/health
```

风险变更流程：先本地调查并验证，运行聚焦测试；后端行为变更时扩大测试；Go embed 发布前先构建前端；从正确 worktree 构建 Linux amd64 release；上传到 `/opt/sub2api/releases/`；未获授权不要切换线上二进制；给出 Termius/tmux 执行与回滚命令。

`onprs` 当前 PostgreSQL 由 1Panel Docker 容器提供，不要假设宿主机存在 `postgres` 系统用户。备份数据库优先用容器内工具：

```bash
docker ps --format '{{.Names}} {{.Image}} {{.Status}}' | grep -Ei 'postgres|postgis'
docker exec 1Panel-postgresql-G4sf psql -U sub2api -d sub2api -Atc 'select 1'
docker exec 1Panel-postgresql-G4sf pg_dump -U sub2api -d sub2api -Fc > /tmp/sub2api-db-before.dump
test -s /tmp/sub2api-db-before.dump
```

不要打印 `/opt/sub2api/config.yaml` 中的数据库密码、token 或私钥。给用户的 Termius/tmux 发布命令不要是一大段交互粘贴；应先写入 `/tmp/<task>.sh`，再执行并保存日志：

```bash
bash /tmp/sub2api-cutover.sh 2>&1 | tee /tmp/sub2api-cutover.log
echo "exit_code=${PIPESTATUS[0]}"
```

`systemctl restart sub2api` 返回后，服务可能已 `active` 但 8080 尚未监听；健康检查必须使用等待循环。若即时 health 失败但 systemd 仍 active，先看 `ss -ltnp | grep ':8080'`、`systemctl status sub2api` 和 `journalctl -u sub2api`，不要立刻假设需要回滚。

## 定制发布守卫

在 `codex/subquota-upstream-20260605` worktree 中，`tools/verify_release_binary.py` 的 `onprs-subquota` profile 当前要求包含：

- `five_hour_limit_usd`
- `seven_day_limit_usd`
- `thirty_day_limit_usd`
- `subscription_quota_snapshot_version`
- `user_group_plan_unique_active`
- `renewal_discount_percent`
- `subscription_renewal_discount_percent`
- `lmspeed.net/provider/api-onprs-top`
- `api/provider/claim-badge/1420`
- `opencode_go`
- `https://opencode.ai/zen/go/v1`
- `https://opencode.ai/docs/go/`
- `channel_monitor_provider_opencode_go`

部署到 `onprs` 前必须在构建该二进制的 worktree 中运行：

```powershell
python tools/verify_release_binary.py "<path-to-sub2api-binary>" --profile onprs-subquota --expected-sha256 "<sha256-if-known>"
```

必须看到 `release_binary_ok=true`；缺任何标记都不要部署。

最近一次 OpenCode Go official-usage 账号级限流修复的 staged release 信息：

- 本地 artifact：`artifacts/sub2api-opencode-go-limit-status-e7e2ac0e-20260624113043/sub2api`
- SHA256：`ca4295ddd637f4d120cf0057ca108c59ce165244d53dbb927886aaa5c20d95ee`
- 远端 staged path：`/opt/sub2api/releases/sub2api-opencode-go-limit-status-e7e2ac0e-20260624113043/sub2api`
- 远端切换脚本：`/tmp/sub2api-opencode-limit-cutover.sh`
- 远端回滚脚本：`/tmp/sub2api-opencode-limit-rollback.sh`

上传到 `/opt/sub2api/releases/` 只表示 staged，不代表已经切换；切换前后必须分别核对 staged SHA、live SHA、`--version`、`systemctl is-active sub2api` 和本机 `/health`。

## 安全与本地脏文件

不要打印或提交 token、OAuth 凭据、数据库密码、支付密钥、私钥。不要回滚用户或其他流程留下的无关改动；如果根 `master` 或旧 worktree 有无关脏文件，只要任务没有明确指向它们，就不要清理、stage、commit、删除或还原。

历史线上检查点只能当参考；当前线上状态必须重新检查后才能引用。
