---
title: 发送第一条请求
description: 使用 curl 测试 Chat Completions、Responses、Anthropic Messages 和 Google GenAI
---

# 发送第一条请求

准备一个 API Key，使用当前 Key 拉取模型列表，并用实际返回的模型名替换下方的 `replace-with-an-available-model`。

## Chat Completions

```bash
curl --fail-with-body https://cdn-api.onprs.online/v1/chat/completions \
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
curl --fail-with-body https://cdn-api.onprs.online/v1/responses \
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
curl --fail-with-body https://cdn-api.onprs.online/v1/messages \
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

Google 原生路径中的模型名属于 URL，请使用 `/v1beta/models` 实际返回的模型 ID。

```bash
curl --fail-with-body \
  "https://cdn-api.onprs.online/v1beta/models/replace-with-an-available-model:generateContent" \
  -H "x-goog-api-key: $ONPRS_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [{"role": "user", "parts": [{"text": "Reply with: connected"}]}]
  }'
```

成功时查看 `candidates[0].content.parts`。

## 排错信息

增加 `-i` 可同时显示响应头。记录状态码、错误正文、`x-request-id`、请求时间、协议路径和模型名。提交材料前移除 Authorization、Cookie 和完整 Key，再按[统一排错流程](/troubleshooting/)检查。
