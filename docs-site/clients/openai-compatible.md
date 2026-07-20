---
title: OpenAI 兼容客户端
description: 配置支持自定义 OpenAI Base URL 的 SDK、IDE 和 GUI 客户端
---

# OpenAI 兼容客户端

适用于允许填写 OpenAI API Key、Base URL 和模型名的 SDK、IDE 插件或 GUI 客户端。

## 字段填写

| 字段 | 值 |
| --- | --- |
| API Key | 你在控制台创建的独立 Key |
| Base URL | `https://cdn-api.onprs.online/v1` |
| Model | 从当前 Key 的 `/v1/models` 返回结果中复制 |
| API mode | 优先选 Chat Completions；明确支持 Responses 时也可选 Responses |

部分客户端把字段称为 `Endpoint`、`Host` 或 `OpenAI API Base`。客户端会自行追加 `/v1` 时，填写 `https://cdn-api.onprs.online`；其他情况填写完整 Base URL。

以下示例从进程环境读取 Key，实际项目可替换为现有的安全凭据来源。

## Python 示例

```python
import os
from openai import OpenAI

client = OpenAI(
    api_key=os.environ["ONPRS_API_KEY"],
    base_url="https://cdn-api.onprs.online/v1",
)

response = client.chat.completions.create(
    model="replace-with-an-available-model",
    messages=[{"role": "user", "content": "Reply with: connected"}],
)
print(response.choices[0].message.content)
```

## JavaScript 示例

```js
import OpenAI from 'openai'

const client = new OpenAI({
  apiKey: process.env.ONPRS_API_KEY,
  baseURL: 'https://cdn-api.onprs.online/v1'
})

const response = await client.responses.create({
  model: 'replace-with-an-available-model',
  input: 'Reply with: connected'
})
console.log(response.output_text)
```

## 成功判据

客户端返回 `connected`，且控制台[用量记录](https://cdn-api.onprs.online/usage)出现对应模型和请求，即表示接入完成。连接测试后请再发送一次生成请求。

## 恢复原配置

切回原 provider 或官方 Base URL，移除 OnprsCodexApi Key，并保留其他 provider 配置。完全重启客户端后生效。
