# 文档站发布与回滚

生产站点：`https://doc-api.onprs.online/`

生产目录：`/opt/1panel/www/sites/doc.api.onprs.top`。1Panel 管理域名、TLS 和 OpenResty 配置；文档发布只更新静态文件，不运行 Node、Docker sidecar 或独立 systemd 服务。

站点入口 `index` 是指向 `releases/<release-id>` 的相对软链。内容切换不修改 `/opt/1panel/www/conf.d/doc.api.onprs.top.conf`，也不 reload OpenResty。

## 1. 本地 staging

只从干净且已提交的分支执行：

```bash
bash docs-site/deploy/stage-release.sh
```

脚本会执行冻结安装、类型检查、构建、外链检查和 Playwright smoke，生成带 SHA256 的归档，并通过一次校验后的上传包写入远端 release 目录。staging 不切换线上软链。

## 2. 切换

在远端 Termius/tmux 会话中运行 staging 输出的命令：

```bash
bash /tmp/<release-id>-cutover.sh 2>&1 | tee /tmp/<release-id>-cutover.log
echo "exit_code=${PIPESTATUS[0]}"
```

切换脚本会：

- 校验 release 文件、归档标识和 1Panel 站点配置。
- 记录当前 release 和配置 SHA256。
- 原子切换 `index` 软链。
- 检查首页、文章、真实 404、缓存和安全响应头。
- 检查 Sub2API 健康状态及 Sub2API、OpenResty PID 未变化。
- 确认站点配置未变化且 OpenResty 未 reload。

校验失败时，脚本会自动恢复原软链。

## 3. 回滚

```bash
bash /tmp/<release-id>-rollback.sh 2>&1 | tee /tmp/<release-id>-rollback.log
echo "exit_code=${PIPESTATUS[0]}"
```

回滚脚本根据发布时保存的状态切回上一 release，并再次检查文档站、Sub2API、配置 SHA256 和进程 PID。回滚同样不 reload OpenResty。

## 4. 清理

确认线上稳定后：

```bash
bash /tmp/<release-id>-cleanup.sh 2>&1 | tee /tmp/<release-id>-cleanup.log
echo "exit_code=${PIPESTATUS[0]}"
```

清理会删除 `/tmp` 中的本次脚本和日志，并默认保留最新 3 个文档 release。可通过 `DOCS_RELEASE_RETENTION` 调整保留数，但不能低于 2；当前 release 和记录的上一 release 始终受保护。迁移前的 1Panel 默认页面备份不在自动清理范围内。

## 配置边界

文档发布脚本不得修改或 reload OpenResty，不得修改 1Panel 数据库，不得触碰 Sub2API、PostgreSQL、Redis 或其他站点。需要变更域名、TLS、响应头或站点配置时，应作为单独的 1Panel 配置变更执行完整配置差异检查。
