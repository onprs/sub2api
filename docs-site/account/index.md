---
title: API Key 安全
description: OnprsCodexApi API Key 最小权限、存储、轮换、审计和泄露响应
---

# API Key 安全

API Key 可以代表账号发起付费请求，请将其作为生产凭据管理。

## 最小权限

- 每个客户端、设备、环境和自动化任务使用独立 Key。
- 只绑定所需分组，不为测试任务开放全部模型。
- 有到期、额度和 IP 限制选项时，根据用途收紧。
- 每位团队成员使用独立 Key。

## 安全存储

推荐使用系统凭据库、CI secret、受限环境变量或权限为 `0600` 的独立文件。以下位置仅保留脱敏占位符：

- Git 仓库和提交历史。
- 浏览器前端包、移动应用静态资源。
- Dockerfile、镜像层或公开 Compose 文件。
- Issue、聊天记录、截图和录屏。
- URL query、分析埋点和普通日志。

示例中的 `sk-your-api-key` 只是占位符。

## 日志脱敏

日志中仅保留 Key 名称、控制台 Key ID 或少量前后缀。Authorization、Cookie、登录 Token 和完整请求正文应默认过滤。

向支持提交 curl 时改成环境变量：

```bash
-H "Authorization: Bearer $ONPRS_API_KEY"
```

## 定期轮换

新建 Key、逐个切换客户端、观察用量、禁用旧 Key，最后撤销。

## 泄露响应

1. 立即在控制台禁用或撤销 Key。
2. 创建新 Key，并只更新受影响的客户端。
3. 检查异常时间范围内的用量、IP、模型和请求 ID。
4. 搜索 Git 历史、CI 日志和聊天附件中的泄露副本。
5. 通过[联系支持](/account/support)报告异常扣费或滥用。

发现 Git 或日志泄露后，请轮换 Key，并清理历史记录和缓存副本。
