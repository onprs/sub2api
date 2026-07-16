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

## 临时配置

::: code-group

```bash [macOS / Linux]
export ANTHROPIC_BASE_URL="https://api.onprs.top/v1"
export ANTHROPIC_AUTH_TOKEN="sk-your-api-key"
export CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY="1"
claude
```

```powershell [Windows PowerShell]
$env:ANTHROPIC_BASE_URL="https://api.onprs.top/v1"
$env:ANTHROPIC_AUTH_TOKEN="sk-your-api-key"
$env:CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY="1"
claude
```

:::

临时变量只对当前终端有效，适合先验证。

## 持久配置

编辑 `~/.claude/settings.json`；Windows 同样位于用户目录的 `.claude/settings.json`。与现有 JSON 合并：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "https://api.onprs.top/v1",
    "ANTHROPIC_AUTH_TOKEN": "sk-your-api-key",
    "CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY": "1"
  }
}
```

如需固定模型，可在确认控制台模型名后增加 `ANTHROPIC_MODEL`。非 `claude-` 前缀模型还可能需要 `ANTHROPIC_CUSTOM_MODEL_OPTION`；优先使用控制台“使用 Key”生成的配置。

## 最小测试

启动新会话，选择控制台可用模型并发送：`Reply with exactly: connected`。成功后在[用量记录](https://cdn.api.onprs.top/usage)确认 `/v1/messages` 请求。

## 常见失败

- 仍出现官方订阅登录：当前进程没有读取 `ANTHROPIC_AUTH_TOKEN`，关闭所有 Claude Code 进程后重开。
- `404`：Base URL 缺少 `/v1` 或被重复追加。
- 模型不可选：启用 gateway model discovery，或显式设置控制台模型名。
- 工具调用中断：记录 Claude Code 版本、请求 ID 和错误正文，查看[流与工具调用排错](/troubleshooting/streaming)。

## 恢复原配置

从 `settings.json` 的 `env` 中删除上述 OnprsCodexApi 变量，关闭终端和 IDE 后重启。临时配置可直接关闭终端，或使用 `unset` / `Remove-Item Env:` 清除。
