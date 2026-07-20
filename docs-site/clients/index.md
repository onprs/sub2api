---
title: 客户端配置
description: 手动配置 Claude Code、Codex CLI、OpenCode、Gemini CLI、IDE、GUI 和 SDK
---

# 客户端配置

::: warning 自动配置脚本
控制台下载脚本仍在开发和验证中。当前请按对应教程手动配置。
:::

## 选择客户端

- [配置 Claude Code](/clients/claude-code)：编辑 `settings.json`。
- [配置 Codex CLI](/clients/codex-cli)：编辑 `config.toml` 和 `auth.json`。
- [配置 OpenCode](/clients/opencode)：编辑 `opencode.jsonc` 和独立 Key 文件。
- [配置 Gemini CLI](/clients/gemini)：编辑 `.gemini/.env`。
- [配置 IDE、GUI 与 SDK](/clients/openai-compatible)：填写客户端设置。

## 开始前准备

1. 安装并确认目标客户端可以启动。
2. 在控制台 [API Keys](https://cdn-api.onprs.online/keys) 创建一个状态为“有效”的独立 Key，并为它选择分组。
3. 按[模型列表](/api/models)使用当前 Key 请求 `GET /v1/models`；Gemini CLI 请求 `GET /v1beta/models`。从实际响应中复制模型名。

## 完成标准

配置完成后发送：

```text
Reply with exactly: connected
```

客户端返回 `connected`，并且[用量记录](https://cdn-api.onprs.online/usage)出现本次请求，即表示配置完成。
