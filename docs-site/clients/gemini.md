---
title: Gemini 与 Google GenAI
description: 使用 Google GenAI 协议连接 Gemini CLI 与 OnprsCodexApi
---

# Gemini 与 Google GenAI

适用版本：Gemini CLI `0.46.0`，最后核对日期 2026-07-16。配置规则参照 [Gemini CLI configuration](https://geminicli.com/docs/reference/configuration)。

## 配置方式

Gemini CLI 可从 `.gemini/.env` 文件加载配置，无需设置系统环境变量。用户级路径为 `~/.gemini/.env`，Windows 为 `%USERPROFILE%\.gemini\.env`。

```dotenv
GOOGLE_GEMINI_BASE_URL="https://cdn-api.onprs.online"
GEMINI_API_KEY="sk-your-api-key"
GEMINI_MODEL="replace-with-an-available-model"
```

`GOOGLE_GEMINI_BASE_URL` 填域名根，Gemini CLI 会自行组成 Google GenAI 路径。控制台“使用 Key”生成的终端命令可作为临时会话配置。

## 配置验证

```bash
gemini -p "Reply with exactly: connected" --output-format text
```

成功时命令输出 `connected`，控制台用量记录显示 Google GenAI 生成入口。

## SDK 路径

原生 REST 生成路径为：

```text
POST https://cdn-api.onprs.online/v1beta/models/{model}:generateContent
POST https://cdn-api.onprs.online/v1beta/models/{model}:streamGenerateContent
```

也支持稳定 `/v1/models/{model}:generateContent` 生成路径；模型发现以 `/v1beta/models` 的实际响应为准。

## 配置检查

- URL 出现重复 `/v1beta`：将 Base URL 设为域名根。
- `401`：确认 `GEMINI_API_KEY` 已传入，并由 SDK 使用 `x-goog-api-key`。
- `404`：核对 URL 中的模型名、动作名和版本路径。
- 工具结果格式错误：使用 Google `functionCall` / `functionResponse` 格式。

## 恢复原配置

从 `.gemini/.env` 或当前使用的凭据配置中删除上述三项，再重新打开终端。
