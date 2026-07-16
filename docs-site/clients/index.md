---
title: 客户端配置
description: 为 OnprsCodexApi 选择并配置 Claude Code、Codex CLI、OpenCode、Gemini 或 OpenAI 兼容客户端
---

# 客户端配置

先按客户端原生协议选择教程。不要仅凭模型名称判断协议；同一模型别名可能由不同平台和协议提供。

| 客户端 | 推荐协议 | Base URL | 教程验证版本 |
| --- | --- | --- | --- |
| Claude Code | Anthropic Messages | `https://api.onprs.top/v1` | `2.1.175` |
| Codex CLI | OpenAI Responses | `https://api.onprs.top/v1` | `0.144.1` |
| OpenCode | OpenAI-compatible provider | `https://api.onprs.top/v1` | `1.18.2` |
| Gemini CLI | Google GenAI | `https://api.onprs.top` | `0.46.0` |
| 通用 OpenAI SDK / GUI | Chat Completions 或 Responses | `https://api.onprs.top/v1` | 按客户端而定 |

版本最后核对日期：2026-07-16。服务端协议支持以[协议矩阵](/api/protocols)为准。

## 配置前检查

1. 为这个客户端创建独立 API Key。
2. 在[可用渠道](https://cdn.api.onprs.top/available-channels)复制该 Key 分组可用的模型名。
3. 关闭客户端中已有的官方账号登录或其他 provider，避免凭据优先级冲突。
4. 配置后完全退出并重启桌面客户端或终端。
5. 用只要求返回短文本的提示完成最小测试。

## 自动导入与手工配置

控制台 [API Keys](https://cdn.api.onprs.top/keys) 的“使用 Key”入口可生成适配当前 Key、分组和模型的配置。自动导入前应查看脚本内容，并先备份已有配置。

需要与现有多 provider 配置合并时，使用本章的手工方式，不要覆盖整个配置文件。

## 选择错误时的表现

- `401`：Key 未传入、被其他登录状态覆盖或已失效。
- `404`：Base URL 多写或少写了 `/v1`，或者协议端点不匹配。
- `400`：客户端发送了目标模型不支持的字段。
- 模型列表为空：Key 分组、模型映射或客户端 provider 配置不匹配。
- 配置修改无效：旧进程仍在运行，或客户端读取了另一个配置目录。
