---
title: IDE、GUI 与 OpenAI SDK
description: 使用 API Key、Base URL 和模型名配置 OpenAI 兼容客户端
---

# IDE、GUI 与 OpenAI SDK

适用于可以填写 OpenAI API Key、Base URL 和模型名的客户端。

## 1. 准备三个值

| 客户端字段 | 填写内容 |
| --- | --- |
| API Key | 控制台创建的独立 Key |
| Base URL | `https://cdn-api.onprs.online/v1` |
| Model | 当前 Key 请求 `GET /v1/models` 返回的模型名 |

客户端中的字段名称也会显示为 `Endpoint`、`Host` 或 `OpenAI API Base`。

## 2. 保存客户端配置

1. Provider 选择 `OpenAI` 或 `OpenAI-compatible`。
2. 填入上面的 API Key、Base URL 和模型名。
3. API mode 选择 `Chat Completions`。
4. 保存配置并重启客户端。

客户端明确标注会自动追加 `/v1` 时，Base URL 填写 `https://cdn-api.onprs.online`。

## 3. SDK 配置

SDK 使用相同的三个值：

::: code-group

```python [Python]
from openai import OpenAI

client = OpenAI(
    api_key="sk-your-api-key",
    base_url="https://cdn-api.onprs.online/v1",
)
result = client.chat.completions.create(
    model="replace-with-an-available-model",
    messages=[{"role": "user", "content": "Reply with exactly: connected"}],
)
print(result.choices[0].message.content)
```

```js [JavaScript]
import OpenAI from 'openai'

const client = new OpenAI({
  apiKey: 'sk-your-api-key',
  baseURL: 'https://cdn-api.onprs.online/v1'
})
const result = await client.chat.completions.create({
  model: 'replace-with-an-available-model',
  messages: [{ role: 'user', content: 'Reply with exactly: connected' }]
})
console.log(result.choices[0].message.content)
```

:::

## 4. 发送验证请求

发送 `Reply with exactly: connected`。客户端返回 `connected`，并且[用量记录](https://cdn-api.onprs.online/usage)出现本次请求，即表示配置完成。

curl 和其他协议示例见[发送第一条请求](/getting-started/first-request)。

## 配置未生效

- `401`：重新填写 Key，并确认 Key 状态为“有效”。
- `404`：核对 Base URL、模型名和客户端是否自动追加 `/v1`。
- 找不到自定义 Base URL：使用支持自定义 Endpoint 的客户端。
