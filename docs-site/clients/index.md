---
title: 客户端配置
description: 按步骤配置 Claude Code、Codex CLI、OpenCode、Gemini CLI、IDE、GUI 和 SDK
---

# 客户端配置

找到你正在使用的客户端，打开对应教程即可。

| 你使用的客户端 | 配置方式 | 教程 |
| --- | --- | --- |
| Claude Code | 控制台导入脚本 | [配置 Claude Code](/clients/claude-code) |
| Codex CLI | 控制台导入脚本 | [配置 Codex CLI](/clients/codex-cli) |
| OpenCode | 控制台导入脚本 | [配置 OpenCode](/clients/opencode) |
| Gemini CLI | `.gemini/.env` 文件 | [配置 Gemini CLI](/clients/gemini) |
| IDE、GUI 或 OpenAI SDK | API Key、Base URL、模型名 | [配置 OpenAI 兼容客户端](/clients/openai-compatible) |

## 开始前准备

1. 安装并确认目标客户端可以启动。
2. 在控制台 [API Keys](https://cdn-api.onprs.online/keys) 创建一个状态为“有效”的 Key，并为它选择分组。
3. 在 Key 所在行点击“使用密钥”，确认弹窗显示的分组和默认模型。

一个客户端使用一个独立 Key，后续停用、轮换和查询用量时更容易定位。控制台导入脚本会使用当前 Key 的实际模型列表生成配置；脚本包含当前 Key，执行完成后删除下载文件。

## 完成标准

配置完成后发送：

```text
Reply with exactly: connected
```

客户端返回 `connected`，并且[用量记录](https://cdn-api.onprs.online/usage)出现本次请求，即表示配置完成。
