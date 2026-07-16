---
title: 发送第一条请求
description: 使用 curl 测试 Chat Completions、Responses、Anthropic Messages 和 Google GenAI
---

# 发送第一条请求

准备一个 API Key，并从[渠道监控](https://cdn.api.onprs.top/monitor)复制模型名。下面的 `replace-with-an-available-model` 必须替换。

## Chat Completions

```bash
curl --fail-with-body https://api.onprs.top/v1/chat/completions \
  -H "Authorization: Bearer $ONPRS_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "replace-with-an-available-model",
    "messages": [{"role": "user", "content": "Reply with: connected"}],
    "stream": false
  }'
```

成功时查看 `choices[0].message.content`。

## Responses

```bash
curl --fail-with-body https://api.onprs.top/v1/responses \
  -H "Authorization: Bearer $ONPRS_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "replace-with-an-available-model",
    "input": "Reply with: connected",
    "stream": false
  }'
```

成功时在 `output` 数组的消息内容中读取文本。不同 SDK 可能提供 `output_text` 便捷字段。

## Anthropic Messages

```bash
curl --fail-with-body https://api.onprs.top/v1/messages \
  -H "x-api-key: $ONPRS_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "replace-with-an-available-model",
    "max_tokens": 128,
    "messages": [{"role": "user", "content": "Reply with: connected"}],
    "stream": false
  }'
```

成功时查看 `content` 数组中的 `text` 内容块。

## Google GenAI

Google 原生路径中的模型名属于 URL，先使用只含字母、数字、点、下划线和连字符的控制台模型 ID。

```bash
curl --fail-with-body \
  "https://api.onprs.top/v1beta/models/replace-with-an-available-model:generateContent" \
  -H "x-goog-api-key: $ONPRS_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [{"role": "user", "parts": [{"text": "Reply with: connected"}]}]
  }'
```

成功时查看 `candidates[0].content.parts`。

## 失败时保留什么

增加 `-i` 可同时显示响应头。记录状态码、错误正文、`x-request-id`、请求时间、协议路径和模型名；删除 Authorization、Cookie 和完整 Key 后，再按[统一排错流程](/troubleshooting/)检查。
