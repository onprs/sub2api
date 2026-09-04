# 文档站发布与回滚

生产站点：`https://doc-api.onprs.online/`

生产目录：`/opt/1panel/www/sites/doc.api.onprs.top`。1Panel 管理域名、TLS 和 OpenResty 配置；文档发布只更新静态文件，不运行 Node、Docker sidecar 或独立 systemd 服务。

站点入口 `index` 是指向 `releases/<release-id>` 的相对软链。内容切换不修改 `/opt/1panel/www/conf.d/doc.api.onprs.top.conf`，也不 reload OpenResty。

## 1. 本地 staging

只从干净且已提交的分支执行：

```bash
bash docs-site/deploy/stage-release.sh
```

脚本会执行冻结安装、类型检查、构建、外链检查和 Playwright smoke，生成带 SHA256 的归档，并通过一次校验后的上传包写入远端 release 目录。staging 不切换线上软链，只输出 cutover 和 rollback 的短命令。

## 2. 切换

在远端 Termius/tmux 会话中运行 staging 输出的单行命令：

```bash
bash /tmp/<release-id>-cutover.sh
```

切换脚本会自行将 stdout/stderr 追加到权限为 `0600` 的 `/tmp/<release-id>-cutover.log`，并在终端和日志中输出 `exit_code=<n> tee_exit_code=<n>`。不要在命令外层再添加 `tee` 或读取 `PIPESTATUS`。

切换脚本会：

- 校验 release 文件、归档标识和 1Panel 站点配置。
- 记录当前 release 和配置 SHA256。
- 原子切换 `index` 软链。
- 检查首页、文章、真实 404、缓存和安全响应头。
- 检查 Sub2API 健康状态及 Sub2API、OpenResty PID 未变化。
- 确认站点配置未变化且 OpenResty 未 reload。
- 成功时输出并记录 `cutover_done=true`。

校验失败时，脚本会自动恢复原软链。命令结束后必须通过独立 SSH 会话核对当前软链、`git-commit.txt`、站点 HTTP 状态、Sub2API 健康状态、进程 PID，以及日志中的终态标记和两个退出码。不能仅依据终端退出码决定是否回滚。

## 3. 回滚

只有独立核对确认切换失败或新站点异常时，才在远端持久会话中执行：

```bash
bash /tmp/<release-id>-rollback.sh
```

回滚脚本自行将输出追加到权限为 `0600` 的 `/tmp/<release-id>-rollback.log`，记录 `exit_code` 与 `tee_exit_code`，并根据发布时保存的状态切回上一 release。成功时输出 `rollback_done=true`，随后仍需独立检查文档站、Sub2API、配置 SHA256 和进程 PID。回滚不 reload OpenResty。

## 4. 观察期与缓存

发布流程不生成或上传独立 `cleanup.sh`。cutover/rollback 脚本、日志、当前 release 和上一 release 在完成独立核验前必须保留。观察期结束后的历史 release 维护应作为单独操作执行，逐项确认固定根目录、受保护 release 和允许删除的文件；迁移前的 1Panel 默认页面备份不在自动清理范围内。

## 配置边界

文档发布脚本不得修改或 reload OpenResty，不得修改 1Panel 数据库，不得触碰 Sub2API、PostgreSQL、Redis 或其他站点。需要变更域名、TLS、响应头或站点配置时，应作为单独的 1Panel 配置变更执行完整配置差异检查。
