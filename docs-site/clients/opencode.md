---
title: OpenCode
description: 使用 opencode.jsonc 和独立 Key 文件手动配置 OpenCode
---

# OpenCode

::: warning 自动配置脚本
控制台下载脚本仍在开发和验证中。当前请按以下步骤手动配置。
:::

## 1. 准备 Key 和模型名

1. 在控制台 [API Keys](https://cdn-api.onprs.online/keys) 创建状态为“有效”的独立 Key，并为它选择分组。
2. 按[模型列表](/api/models)使用当前 Key 请求 `GET /v1/models`，复制实际返回的模型名。

## 2. 创建配置文件

配置目录：

- Windows：`%USERPROFILE%\.config\opencode`
- macOS / Linux：`~/.config/opencode`

创建目录和文件：

::: code-group

```powershell [Windows PowerShell]
New-Item -ItemType Directory -Force "$HOME\.config\opencode" | Out-Null
notepad "$HOME\.config\opencode\onprs.key"
notepad "$HOME\.config\opencode\opencode.jsonc"
```

```bash [macOS / Linux]
mkdir -p ~/.config/opencode
touch ~/.config/opencode/onprs.key ~/.config/opencode/opencode.jsonc
chmod 600 ~/.config/opencode/onprs.key
```

:::

`onprs.key` 的文件内容只写当前 Key：

```text
sk-your-api-key
```

再创建或编辑 `opencode.jsonc`。将两处模型占位符替换为同一个实际模型名：

```json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "onprs": {
      "name": "OnprsCodexApi",
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "baseURL": "https://cdn-api.onprs.online/v1",
        "apiKey": "{file:~/.config/opencode/onprs.key}"
      },
      "models": {
        "replace-with-an-available-model": {
          "name": "replace-with-an-available-model"
        }
      }
    }
  },
  "model": "onprs/replace-with-an-available-model"
}
```

文件已有 provider 时，保留原配置并添加 `provider.onprs` 和顶层 `model`。

## 3. 启动并验证

1. 完全退出 OpenCode Desktop 和 sidecar，再重新打开。
2. 新建会话并运行 `/models`，选择 `OnprsCodexApi` 下的模型。
3. 发送 `Reply with exactly: connected`。

返回 `connected` 后，在[用量记录](https://cdn-api.onprs.online/usage)确认本次请求。

## 配置未生效

- 看不到 `OnprsCodexApi`：检查 `opencode.jsonc` 路径和 JSON 格式。
- 返回 `401`：检查 `onprs.key` 的路径和内容。
- 返回 `404`：确认两处模型名均来自当前 Key 的 `/v1/models` 响应。
