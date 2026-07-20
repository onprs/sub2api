---
title: Gemini CLI
description: 使用配置文件连接 Gemini CLI 与 OnprsCodexApi
---

# Gemini CLI

## 1. 取得 Key 和模型名

1. 打开控制台 [API Keys](https://cdn-api.onprs.online/keys)。
2. 在目标 Key 所在行点击“使用密钥”，选择“Gemini CLI”。
3. 复制弹窗中的 Key。
4. 按[模型列表](/api/models)使用当前 Key 请求 `GET /v1beta/models`，复制返回的模型名并去掉 `models/` 前缀。

## 2. 创建配置文件

配置文件路径：

- Windows：`%USERPROFILE%\.gemini\.env`
- macOS / Linux：`~/.gemini/.env`

先创建目录和文件：

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

将下面三行写入 `.env`，替换 Key 和模型名：

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
- 返回 `404`：核对模型名已经去掉 `models/` 前缀。
- URL 中出现两次 `/v1beta`：保持 `GOOGLE_GEMINI_BASE_URL` 为上面的域名根地址。
