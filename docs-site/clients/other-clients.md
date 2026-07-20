---
title: 其他客户端
description: 配置其他 SDK、IDE 和 GUI 客户端
---

# 其他客户端

客户端可以填写 API Key、Base URL 和模型名时，按 [IDE、GUI 与 OpenAI SDK](/clients/openai-compatible) 配置。

客户端要求选择协议时，使用下表：

| 客户端协议 | Base URL | 模型列表 |
| --- | --- | --- |
| OpenAI | `https://cdn-api.onprs.online/v1` | `GET /v1/models` |
| Anthropic | `https://cdn-api.onprs.online/v1` | `GET /v1/models` |
| Google GenAI | `https://cdn-api.onprs.online` | `GET /v1beta/models` |

模型名以当前 Key 实际返回的列表为准。配置完成后发送 `Reply with exactly: connected`，再到[用量记录](https://cdn-api.onprs.online/usage)确认请求。

需要排查时，提供客户端名称、版本、脱敏配置和请求 ID。
