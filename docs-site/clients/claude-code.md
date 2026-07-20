---
title: Claude Code
description: 使用 Anthropic Messages 协议连接 Claude Code 与 OnprsCodexApi
---

# Claude Code

适用版本：Claude Code `2.1.175`，最后核对日期 2026-07-16。配置方式遵循 [Anthropic LLM gateway 文档](https://docs.anthropic.com/en/docs/claude-code/llm-gateway)。

## 前置条件

- API Key 所属分组可通过 Anthropic Messages 协议调用目标模型。
- 已安装 Claude Code，并可运行 `claude --version`。
- 已完全退出仍在运行的 Claude Code 或 IDE 扩展会话。

## 配置方式

优先使用控制台“使用 Key”生成的配置，也可手工编辑 `~/.claude/settings.json`；Windows 路径同样位于用户目录的 `.claude/settings.json`。Claude Code 可直接从该文件读取配置，无需设置系统环境变量。

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "https://cdn-api.onprs.online/v1",
    "ANTHROPIC_AUTH_TOKEN": "sk-your-api-key",
    "CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY": "1"
  }
}
```

如需固定模型，可从当前 Key 的 `/v1/models` 返回结果中选择，并设置 `ANTHROPIC_MODEL`。非 `claude-` 前缀模型还可能需要 `ANTHROPIC_CUSTOM_MODEL_OPTION`；优先使用控制台生成的配置。

### 终端会话配置（可选）

::: code-group

```bash [macOS / Linux]
export ANTHROPIC_BASE_URL="https://cdn-api.onprs.online/v1"
export ANTHROPIC_AUTH_TOKEN="sk-your-api-key"
export CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY="1"
claude
```

```powershell [Windows PowerShell]
$env:ANTHROPIC_BASE_URL="https://cdn-api.onprs.online/v1"
$env:ANTHROPIC_AUTH_TOKEN="sk-your-api-key"
$env:CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY="1"
claude
```

:::

终端配置只对当前会话有效，适合临时验证。

## 配置验证

启动新会话，选择当前 Key 模型列表中的模型并发送：`Reply with exactly: connected`。成功后在[用量记录](https://cdn-api.onprs.online/usage)确认 `/v1/messages` 请求。

## 配置检查

- 出现官方订阅登录：关闭所有 Claude Code 进程，并确认新进程读取了 `settings.json` 或当前终端配置。
- `404`：核对 Base URL 中的 `/v1`。
- 模型不可选：启用 gateway model discovery，或显式设置 `/v1/models` 返回的模型名。
- 工具调用中断：记录 Claude Code 版本、请求 ID 和错误正文，查看[流与工具调用排错](/troubleshooting/streaming)。

## 恢复原配置

从 `settings.json` 的 `env` 中删除上述 OnprsCodexApi 配置，关闭终端和 IDE 后重启。使用终端会话配置时，关闭终端即可清除。
