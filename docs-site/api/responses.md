---
title: 响应、用量与请求 ID
description: 读取四类协议响应、Token 用量、费用和请求 ID
---

# 响应、用量与请求 ID

## 读取文本

| 协议 | 文本位置 |
| --- | --- |
| Chat Completions | `choices[].message.content` |
| Responses | `output[].content[]`，部分 SDK 提供 `output_text` |
| Anthropic Messages | `content[]` 中的 `text` 块 |
| Google GenAI | `candidates[].content.parts[]` 中的 `text` |

响应可同时包含文本、工具调用、图片和 reasoning。生产代码应按协议遍历内容数组和内容块。

## 用量字段

不同协议使用 `usage`、`usageMetadata` 或流式增量事件表示输入、输出和缓存 Token。服务端以实际执行协议提取用量，再写入统一用量记录。

控制台费用按请求包含以下适用项目：

- 输入和输出 Token 费用。
- 缓存写入、缓存读取费用。
- 图片输出或按请求费用。
- 分组倍率、账号倍率或时段倍率。
- 余额计费或订阅权益归属。

客户端原始 `usage` 可用于核对单次请求，最终扣费以[用量记录](https://cdn-api.onprs.online/usage)为准。

## 请求 ID

优先保留响应头中的：

```text
x-request-id
```

部分客户端或网关链路还会产生 `x-client-request-id`。提交支持请求时可同时提供两者；每个请求保留其原始 ID。

使用 curl 可这样显示响应头：

```bash
curl -i --fail-with-body https://cdn-api.onprs.online/v1/models \
  -H "Authorization: Bearer $ONPRS_API_KEY"
```

## 错误响应

错误外层结构按入站协议呈现。通用定位顺序是：

1. HTTP 状态码。
2. 机器可读错误码、`type` 或 `reason`。
3. 用户可读 `message`。
4. `x-request-id`。
5. 发生时间、路径、模型和是否流式。

先按[HTTP 状态码](/troubleshooting/status-codes)处理，再搜索具体错误码。
