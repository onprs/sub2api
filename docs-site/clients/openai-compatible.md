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
| Base URL | `https://api.onprs.top/v1` |
| Model | 从[渠道监控](https://cdn.api.onprs.top/monitor)复制 |
| API mode | 优先选 Chat Completions；明确支持 Responses 时也可选 Responses |

部分客户端把字段称为 `Endpoint`、`Host` 或 `OpenAI API Base`。如果它会自行追加 `/v1`，则填写 `https://api.onprs.top`；出现 `/v1/v1/...` 的 404 就表示重复追加。

## Python 最小示例

```python
import os
from openai import OpenAI

client = OpenAI(
    api_key=os.environ["ONPRS_API_KEY"],
    base_url="https://api.onprs.top/v1",
)

response = client.chat.completions.create(
    model="replace-with-an-available-model",
    messages=[{"role": "user", "content": "Reply with: connected"}],
)
print(response.choices[0].message.content)
```

## JavaScript 最小示例

```js
import OpenAI from 'openai'

const client = new OpenAI({
  apiKey: process.env.ONPRS_API_KEY,
  baseURL: 'https://api.onprs.top/v1'
})

const response = await client.responses.create({
  model: 'replace-with-an-available-model',
  input: 'Reply with: connected'
})
console.log(response.output_text)
```

## 成功判据

客户端能返回 `connected`，控制台[用量记录](https://cdn.api.onprs.top/usage)出现对应模型和请求。客户端若只测试 `/models` 成功，仍需再做一次生成请求。

## 恢复原配置

切回原 provider 或官方 Base URL，移除 OnprsCodexApi Key，并完全重启客户端。不要删除其他 provider 的配置。
