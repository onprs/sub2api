---
title: Claude Code
description: 使用 settings.json 手动配置 Claude Code
---

# Claude Code

::: warning 自动配置脚本
控制台下载脚本仍在开发和验证中。当前请按以下步骤手动配置。
:::

## 1. 准备 API Key

在控制台 [API Keys](https://cdn-api.onprs.online/keys) 创建状态为“有效”的独立 Key，并为它选择分组。

## 2. 创建配置文件

配置文件路径：

- Windows：`%USERPROFILE%\.claude\settings.json`
- macOS / Linux：`~/.claude/settings.json`

创建目录和文件：

::: code-group

```powershell [Windows PowerShell]
New-Item -ItemType Directory -Force "$HOME\.claude" | Out-Null
notepad "$HOME\.claude\settings.json"
```

```bash [macOS / Linux]
mkdir -p ~/.claude
touch ~/.claude/settings.json
```

:::

使用文本编辑器打开该文件。文件已有内容时，保留原字段并合并下面的 `env` 字段。

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "https://cdn-api.onprs.online/v1",
    "ANTHROPIC_AUTH_TOKEN": "sk-your-api-key",
    "CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY": "1"
  }
}
```

将 `sk-your-api-key` 替换为当前 Key，并保存文件。

## 3. 启动并验证

1. 完全退出原有 Claude Code 和 IDE 会话。
2. 打开新终端并运行 `claude`。
3. 运行 `/model`，选择当前 Key 模型列表中的模型。
4. 发送 `Reply with exactly: connected`。

返回 `connected` 后，在[用量记录](https://cdn-api.onprs.online/usage)确认本次 `/v1/messages` 请求。

## 配置未生效

- 仍显示官方登录：确认 `settings.json` 路径和 `ANTHROPIC_AUTH_TOKEN`，再完全重启客户端。
- 没有目标模型：确认 `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY` 的值为字符串 `"1"`。
- 返回 `401` 或 `404`：按 [Key、权限与模型](/troubleshooting/auth-model)检查 Key 状态、分组和模型名。
