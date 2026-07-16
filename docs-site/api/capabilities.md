---
title: 流式、工具与结构化输出
description: OnprsCodexApi 流式响应、工具调用、结构化输出和媒体能力边界
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

客户端必须按自身协议解析，不能把 Anthropic 事件当作 OpenAI `[DONE]` 流。连接中断时先记录是否已收到首个内容事件和最后一个事件。

## 工具调用

标准协议间会保留可表达的工具名、调用 ID、参数和工具结果关联。工具的实际执行仍由客户端负责；服务不会替客户端调用本地命令或第三方系统。

建议：

- 工具 schema 使用有效 JSON Schema，避免同名工具。
- 工具结果回传时沿用原调用 ID。
- Google `functionResponse` 内容使用规范 JSON 对象；转换到 Anthropic 时服务会序列化为规范 JSON 文本。
- 两轮工具测试应使用同一会话和同一 Key，避免中途切换 provider。

## 结构化输出

Responses 和 Chat 客户端可请求 JSON schema 等结构化形式，但最终支持取决于目标模型和上游。排错时先退回普通文本；普通文本成功、结构化输出失败，通常表示模型能力或 schema 不兼容。

不要把“返回了可解析 JSON”当成 schema 一定受到严格约束。业务端仍应验证类型、必填字段和长度。

## Reasoning 与签名

Responses、Anthropic 和 Google 对 reasoning 或 thought signature 的表达方式不同。跨协议请求会在能力允许时保留相关内容；无法兼容时可能返回能力错误。

## 图片和批量图片

图片输入、图片生成、编辑和批量图片属于模型与平台专用能力。基础生成路由支持不等于目标模型支持图片。

- OpenAI 风格生成：`/v1/images/generations`、`/v1/images/edits`。
- 批量图片：控制台[批量图片](https://cdn.api.onprs.top/batch-image)及 `/v1/images/batches` 系列接口。
- Spark 类 Codex 模型可能明确不支持图片输入或生成，应切换到控制台标记支持图片的模型。

视频、embedding、compact 和 Responses WebSocket 也属于专用路径，不在四类普通生成能力矩阵内。
