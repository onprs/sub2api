---
title: Gemini CLI
description: 使用 .gemini/.env 手动配置 Gemini CLI
---

# Gemini CLI

::: warning 自动配置功能
自动配置功能仍在开发和验证中。当前请按以下步骤手动配置。
:::

## 1. 准备 Key 和模型名

1. 在控制台 [API Keys](https://cdn-api.onprs.online/keys) 创建状态为“有效”的独立 Key，并为它选择分组。
2. 按[模型列表](/api/models)使用当前 Key 请求 `GET /v1beta/models`，复制返回的模型名并去掉 `models/` 前缀。

## 2. 创建配置文件

配置文件路径：

- Windows：`%USERPROFILE%\.gemini\.env`
- macOS / Linux：`~/.gemini/.env`

创建目录和文件：

::: code-group

```powershell [Windows PowerShell]
New-Item -ItemType Directory -Force "$HOME\.gemini" | Out-Null
notepad "$HOME\.gemini\.env"
```

```bash [macOS / Linux]
mkdir -p ~/.gemini
touch ~/.gemini/.env
```

:::

使用文本编辑器打开 `.env`，写入以下内容并替换 Key 和模型名：

```dotenv
GOOGLE_GEMINI_BASE_URL="https://cdn-api.onprs.online"
GEMINI_API_KEY="sk-your-api-key"
GEMINI_MODEL="replace-with-an-available-model"
```

## 3. 启动并验证

打开新终端并运行：

```bash
gemini -p "Reply with exactly: connected" --output-format text
```

命令返回 `connected` 后，在[用量记录](https://cdn-api.onprs.online/usage)确认本次 Google GenAI 请求。

## 配置未生效

- 返回 `401`：核对 `.env` 中的 `GEMINI_API_KEY`。
- 返回 `404`：确认模型名来自当前 Key 的 `/v1beta/models` 响应，并且已经去掉 `models/` 前缀。
- URL 中出现两次 `/v1beta`：保持 `GOOGLE_GEMINI_BASE_URL` 为上面的域名根地址。
