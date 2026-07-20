---
title: 流式、工具与结构化输出
description: OnprsCodexApi 流式响应、工具调用、结构化输出和媒体能力
---

# 流式、工具与结构化输出

## 流式响应

四类标准生成协议均支持普通 HTTP/SSE 流式转换：

| 入站协议 | 开启方式 | 正常终止信号 |
| --- | --- | --- |
| Chat Completions | `stream: true` | `[DONE]` |
| Responses | `stream: true` | `response.completed` 等终止事件 |
| Anthropic Messages | `stream: true` | `message_stop` |
| Google GenAI | `:streamGenerateContent` | 正常 SSE 结束 |

客户端按自身协议解析流事件。连接中断时，记录首个内容事件和最后一个完整事件。

## 工具调用

标准协议间会保留可表达的工具名、调用 ID、参数和工具结果关联。工具的实际执行仍由客户端负责；服务不会替客户端调用本地命令或第三方系统。

建议：

- 工具 schema 使用有效 JSON Schema，并为每个工具设置唯一名称。
- 工具结果回传时沿用原调用 ID。
- Google `functionResponse` 内容使用规范 JSON 对象；转换到 Anthropic 时服务会序列化为规范 JSON 文本。
- 两轮工具测试使用同一会话、Key 和 provider。

## 结构化输出

Responses 和 Chat 客户端可请求 JSON schema 等结构化形式，具体支持取决于目标模型和上游。排错时先验证普通文本，再检查模型能力和 schema。

业务端仍需验证返回数据的类型、必填字段和长度。

## Reasoning 与签名

Responses、Anthropic 和 Google 对 reasoning 或 thought signature 的表达方式不同。跨协议请求会在能力允许时保留相关内容；无法兼容时返回能力错误。

## 图片和批量图片

图片输入、图片生成、编辑和批量图片属于模型与平台专用能力。先确认目标模型在当前 Key 实际拉取的模型列表中，再查看对应模型的能力说明。

- OpenAI 风格生成：`/v1/images/generations`、`/v1/images/edits`。
- 批量图片：控制台[批量图片](https://cdn-api.onprs.online/batch-image)及 `/v1/images/batches` 系列接口。
- Spark 类 Codex 模型的图片能力以控制台标记为准。
