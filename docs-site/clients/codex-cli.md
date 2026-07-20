---
title: Codex CLI
description: 使用 config.toml 和 auth.json 手动配置 Codex CLI
---

# Codex CLI

::: warning 自动配置脚本
控制台下载脚本仍在开发和验证中。当前请按以下步骤手动配置。
:::

## 1. 准备 Key 和模型名

1. 在控制台 [API Keys](https://cdn-api.onprs.online/keys) 创建状态为“有效”的独立 Key，并为它选择分组。
2. 按[模型列表](/api/models)使用当前 Key 请求 `GET /v1/models`，复制实际返回的模型名。

## 2. 创建配置目录

配置目录：

- Windows：`%USERPROFILE%\.codex`
- macOS / Linux：`~/.codex`

创建目录和文件：

::: code-group

```powershell [Windows PowerShell]
New-Item -ItemType Directory -Force "$HOME\.codex" | Out-Null
notepad "$HOME\.codex\config.toml"
notepad "$HOME\.codex\auth.json"
```

```bash [macOS / Linux]
mkdir -p ~/.codex
touch ~/.codex/config.toml ~/.codex/auth.json
```

:::

目录中已有配置时，先备份 `config.toml` 和 `auth.json`。

## 3. 写入配置

创建 `config.toml`，将模型占位符替换为上一步复制的模型名：

```toml
model_provider = "OpenAI"
model = "replace-with-an-available-model"

[model_providers.OpenAI]
name = "OpenAI"
base_url = "https://cdn-api.onprs.online/v1"
wire_api = "responses"
requires_openai_auth = true
```

在同一目录创建 `auth.json`，将 Key 占位符替换为当前 Key：

```json
{
  "OPENAI_API_KEY": "sk-your-api-key"
}
```

## 4. 启动并验证

1. 完全退出原有 Codex CLI 会话。
2. 打开新终端并运行 `codex`。
3. 发送 `Reply with exactly: connected`。

返回 `connected` 后，在[用量记录](https://cdn-api.onprs.online/usage)确认本次 `/v1/responses` 请求。

## 配置未生效

- 仍使用原 provider：检查 `model_provider = "OpenAI"` 是否位于 `config.toml` 顶层。
- 返回 `401`：检查 `auth.json` 的文件名、JSON 格式和 Key。
- 返回 `404`：确认模型名来自当前 Key 的 `/v1/models` 响应。
