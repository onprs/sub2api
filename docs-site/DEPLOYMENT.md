# 文档站发布与回滚

生产目录：`/opt/1panel/www/sites/sub2api-docs`。静态站由现有 1Panel OpenResty 在 `0.0.0.0:4173` 提供，不运行 Node、Docker sidecar 或 systemd 文档服务。

## 1. 本地 staging

只从干净且已提交的分支执行：

```bash
bash docs-site/deploy/stage-release.sh
```

脚本执行冻结安装、类型检查、构建、外链检查和 Playwright smoke，生成带 SHA256 的归档，并上传 release 与三个远端脚本。它不会切换 `current` 或 reload OpenResty。

## 2. 切换

在远端 Termius/tmux 会话中运行 staging 输出的命令：

```bash
bash /tmp/<release-id>-cutover.sh 2>&1 | tee /tmp/<release-id>-cutover.log
echo "exit_code=${PIPESTATUS[0]}"
```

首次切换会：

- 保存限制权限的 `nginx -T` 和 conf.d SHA256 基线。
- 原子切换 `current` 软链。
- 只安装 `sub2api-docs-port.conf`。
- 验证完整配置差异只来自该文件。
- graceful reload，检查主站与文档站。
- 移除配置并 reload 演练一次配置回滚，再恢复并完成最终 reload。

后续内容发布在配置完全一致时只切换软链，不 reload OpenResty。

## 3. 回滚

```bash
bash /tmp/<release-id>-rollback.sh 2>&1 | tee /tmp/<release-id>-rollback.log
echo "exit_code=${PIPESTATUS[0]}"
```

有上一 release 时切回上一软链。首次发布没有上一 release 时，回滚会删除新增端口配置并 graceful reload，使 `4173` 停止服务。两种情况都复查 Sub2API 内部和公网 health。

## 4. 清理

确认线上稳定后：

```bash
bash /tmp/<release-id>-cleanup.sh 2>&1 | tee /tmp/<release-id>-cleanup.log
echo "exit_code=${PIPESTATUS[0]}"
```

清理会删除 `/tmp` 中本次上传物、快照、脚本和日志，并默认保留最新 3 个 release（可用 `DOCS_RELEASE_RETENTION` 调整，但不能低于 2）。当前 release 和它记录的上一 release 始终受保护。

## 配置边界

`deploy/sub2api-docs-port.conf` 禁止加入 `80/443`、`default_server`、现有域名、`proxy_pass`、upstream 或共享 include。域名、TLS、CDN、反向代理和主站 `doc_url` 不属于本文档站发布流程。
