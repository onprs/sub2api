---
title: API 总览
description: OnprsCodexApi 认证、Base URL、协议、模型与响应基础
---

# API 总览

OnprsCodexApi 提供四类标准生成协议。路由决定客户端发送的协议，Key 分组和模型映射决定请求实际由哪个平台处理。

## 基础信息

```text
控制台：https://api.onprs.top/
OpenAI / Anthropic Base URL：https://api.onprs.top/v1
Google GenAI Base URL：https://api.onprs.top
```

推荐认证方式：

```http
Authorization: Bearer sk-your-api-key
```

Anthropic SDK 可使用 `x-api-key`，Google SDK 可使用 `x-goog-api-key`。不要在 URL query、日志或错误截图中暴露 Key。

## 协议入口

| 协议 | 生成入口 | 典型客户端 |
| --- | --- | --- |
| OpenAI Chat Completions | `POST /v1/chat/completions` | 通用 OpenAI-compatible 客户端 |
| OpenAI Responses | `POST /v1/responses` | Codex CLI、新版 OpenAI SDK |
| Anthropic Messages | `POST /v1/messages` | Claude Code、Anthropic SDK |
| Google GenAI | `POST /v1beta/models/{model}:generateContent` | Gemini CLI、Google GenAI SDK |

完整路由见[协议与端点](/api/protocols)。

## 一个请求的处理过程

1. 从路由确定入站协议。
2. 根据 API Key 读取用户、分组、平台和计费方式。
3. 校验 Key、余额或订阅额度。
4. 根据模型映射和可用账号选择实际平台与上游模型。
5. 必要时在标准协议间转换请求和响应。
6. 流式返回或缓冲返回，并写入用量和计费记录。

因此，客户端看到的模型名和响应格式属于入站协议；用量页面还可能显示实际的上游模型和端点。

## 兼容边界

协议可转换不代表所有模型都支持图片、工具调用、结构化输出、超长上下文或同样的采样参数。跨协议遇到不可无损表达的能力时，服务可能返回明确错误，而不是静默丢弃内容。

开始调用前先查看[模型与映射](/api/models)和[流式、工具与结构化输出](/api/capabilities)。
