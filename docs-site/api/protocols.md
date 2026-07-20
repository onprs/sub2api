---
title: 协议与端点
description: Chat Completions、Responses、Anthropic Messages 与 Google GenAI 路径矩阵
---

# 协议与端点

新集成使用以下公开入口。兼容别名供特定客户端使用。

## OpenAI Chat Completions

```text
POST /v1/chat/completions
GET  /v1/models
```

请求使用 `messages` 数组；流式请求设置 `stream: true`，返回 SSE，并以 `[DONE]` 结束。

兼容别名：`POST /chat/completions`。

## OpenAI Responses

```text
POST /v1/responses
GET  /v1/models
```

请求使用 `input`；流式请求设置 `stream: true`，返回 Responses 事件。普通 SDK 使用 `/v1/responses`；根级 `/responses` 是兼容别名，`/backend-api/codex` 由 Codex 专用客户端使用。

`/v1/responses/compact`、图片、视频和 WebSocket 属于专用能力，具体支持以平台说明为准。OpenCode Go 的 Responses 入口为根级 HTTP 生成，Responses WebSocket 和 `/responses/*` 专用子路径不在支持范围。

## Anthropic Messages

```text
POST /v1/messages
GET  /v1/models
POST /v1/messages/count_tokens
```

请求使用 `messages` 和 `max_tokens`；流式返回 Anthropic SSE 事件，以 `message_stop` 完成。`count_tokens` 在所选平台提供对应能力时可用。

## Google GenAI

```text
GET  /v1beta/models
GET  /v1beta/models/{model}
POST /v1beta/models/{model}:generateContent
POST /v1beta/models/{model}:streamGenerateContent
POST /v1/models/{model}:generateContent
POST /v1/models/{model}:streamGenerateContent
```

`/v1beta` 用于标准模型发现和生成。稳定 `/v1/models/{model}:...` 生成路径也受支持；Google 模型列表使用 `GET /v1beta/models`，`GET /v1/models` 返回 OpenAI 风格列表。

Google API Key 通过 `x-goog-api-key` Header 传入。

## 专用 Antigravity 路径

`/antigravity/v1/*` 和 `/antigravity/v1beta/*` 会强制使用 Antigravity 平台。只有控制台配置或支持人员明确要求时使用；普通接入优先选择上面的统一入口。

## 认证错误快速判断

- `API_KEY_REQUIRED`：三个支持的 Header 都未读取到 Key。
- `INVALID_API_KEY`：Key 不存在。
- `API_KEY_DISABLED`：Key 已禁用。
- `ACCESS_DENIED`：IP 访问策略不允许当前来源。

更多状态见[HTTP 状态码](/troubleshooting/status-codes)。
