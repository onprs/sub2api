---
title: 客户端配置
description: 为 OnprsCodexApi 选择并配置 Claude Code、Codex CLI、OpenCode、Gemini 或 OpenAI 兼容客户端
---

# 客户端配置

按客户端原生协议选择教程；同一模型别名可能由不同平台和协议提供。

| 客户端 | 推荐协议 | Base URL | 教程验证版本 |
| --- | --- | --- | --- |
| Claude Code | Anthropic Messages | `https://cdn-api.onprs.online/v1` | `2.1.175` |
| Codex CLI | OpenAI Responses | `https://cdn-api.onprs.online/v1` | `0.144.1` |
| OpenCode | OpenAI-compatible provider | `https://cdn-api.onprs.online/v1` | `1.18.2` |
| Gemini CLI | Google GenAI | `https://cdn-api.onprs.online` | `0.46.0` |
| 通用 OpenAI SDK / GUI | Chat Completions 或 Responses | `https://cdn-api.onprs.online/v1` | 按客户端而定 |

版本最后核对日期：2026-07-16。服务端协议支持以[协议矩阵](/api/protocols)为准。

## 配置前检查

1. 为这个客户端创建独立 API Key。
2. 使用当前 Key 拉取模型列表，并复制实际返回的模型名。
3. 确保客户端启用 OnprsCodexApi provider，并读取对应 Key。
4. 配置后完全退出并重启桌面客户端或终端。
5. 用只要求返回短文本的提示完成最小测试。

## 自动导入与手工配置

控制台 [API Keys](https://cdn-api.onprs.online/keys) 的“使用 Key”入口可生成适配当前 Key、分组和模型的配置。导入前查看脚本内容并备份已有配置。

需要保留多个 provider 时，按本章手工合并对应字段，同时保留现有配置。

## 配置检查

- `401`：确认客户端读取了当前 Key，并启用了 OnprsCodexApi provider。
- `404`：核对 Base URL 中的 `/v1` 和协议端点。
- `400`：根据错误正文调整模型参数或能力字段。
- 模型列表为空：核对 Key 分组、模型映射和 provider 配置。
- 配置未生效：完全退出客户端，并确认实际读取的配置目录。
