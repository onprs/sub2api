---
title: API 总览
description: OnprsCodexApi 认证、Base URL、协议、模型与响应基础
---

# API 总览

OnprsCodexApi 提供四类标准生成协议。路由决定客户端发送的协议，Key 分组和模型映射决定请求实际由哪个平台处理。

## 基础信息

```text
控制台：https://cdn-api.onprs.online/
OpenAI / Anthropic Base URL：https://cdn-api.onprs.online/v1
Google GenAI Base URL：https://cdn-api.onprs.online
```

推荐认证方式：

```http
Authorization: Bearer sk-your-api-key
```

Anthropic SDK 可使用 `x-api-key`，Google SDK 可使用 `x-goog-api-key`。Key 请通过 Header 发送，并在日志和截图中脱敏。

## 协议入口

| 协议 | 生成入口 | 典型客户端 |
| --- | --- | --- |
| OpenAI Chat Completions | `POST /v1/chat/completions` | 通用 OpenAI-compatible 客户端 |
| OpenAI Responses | `POST /v1/responses` | Codex CLI、新版 OpenAI SDK |
| Anthropic Messages | `POST /v1/messages` | Claude Code、Anthropic SDK |
| Google GenAI | `POST /v1beta/models/{model}:generateContent` | Gemini CLI、Google GenAI SDK |

完整路由见[协议与端点](/api/protocols)。

## 请求处理

服务会校验 API Key 和额度，根据模型配置路由请求，并按客户端使用的协议返回响应。用量页面可用于核对请求模型、实际模型和费用。

## 兼容边界

模型能力以渠道监控和目标平台说明为准，包括图片、工具调用、结构化输出、上下文长度和采样参数。跨协议无法完整表达某项能力时，服务会返回明确错误。

开始调用前先查看[模型与映射](/api/models)和[流式、工具与结构化输出](/api/capabilities)。
