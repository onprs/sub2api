---
title: Gemini 与 Google GenAI
description: 使用 Google GenAI 协议连接 Gemini CLI 与 OnprsCodexApi
---

# Gemini 与 Google GenAI

适用版本：Gemini CLI `0.46.0`，最后核对日期 2026-07-16。环境变量规则参照 [Gemini CLI configuration](https://geminicli.com/docs/reference/configuration)。

## 配置环境变量

`GOOGLE_GEMINI_BASE_URL` 填域名根，不追加 `/v1beta`。Gemini CLI 会自行组成 Google GenAI 路径。

::: code-group

```bash [macOS / Linux]
export GOOGLE_GEMINI_BASE_URL="https://api.onprs.top"
export GEMINI_API_KEY="sk-your-api-key"
export GEMINI_MODEL="replace-with-an-available-model"
```

```powershell [Windows PowerShell]
$env:GOOGLE_GEMINI_BASE_URL="https://api.onprs.top"
$env:GEMINI_API_KEY="sk-your-api-key"
$env:GEMINI_MODEL="replace-with-an-available-model"
```

:::

## 最小测试

```bash
gemini -p "Reply with exactly: connected" --output-format text
```

成功时命令输出 `connected`，控制台用量记录显示 Google GenAI 生成入口。

## SDK 路径

原生 REST 生成路径为：

```text
POST https://api.onprs.top/v1beta/models/{model}:generateContent
POST https://api.onprs.top/v1beta/models/{model}:streamGenerateContent
```

也支持稳定 `/v1/models/{model}:generateContent` 生成路径；模型发现仍应优先使用 `/v1beta/models` 或控制台。

## 常见失败

- `/v1beta/v1beta/...`：Base URL 被错误写成带 `/v1beta` 的地址。
- `401`：`GEMINI_API_KEY` 未传入，或 SDK 没有使用 `x-goog-api-key`。
- `404`：URL 中模型名、动作名或版本路径不匹配。
- 工具结果格式错误：确认客户端发送规范 Google `functionCall` / `functionResponse`，不要自行混入 Anthropic 内容块。

## 恢复原配置

删除三个环境变量并重新打开终端。若变量写入 shell profile 或系统用户环境，也需从对应持久配置中移除。
