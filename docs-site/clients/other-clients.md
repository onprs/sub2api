---
title: 其他客户端
description: 将其他 SDK、IDE 和 GUI 客户端映射到 OnprsCodexApi 协议字段
---

# 其他客户端

只有同时满足以下条件的客户端才适合直接接入：

- 允许自定义 Base URL 或 provider endpoint。
- 允许填写自有 API Key，而不是强制厂商账号登录。
- 能选择 Chat Completions、Responses、Anthropic Messages 或 Google GenAI 中至少一种协议。
- 能输入服务端实际模型名，不强制固定官方模型清单。

## 通用字段映射

| 客户端字段 | 应填写 |
| --- | --- |
| Provider | OpenAI-compatible、Anthropic 或 Google，按实际协议选择 |
| Base URL | OpenAI/Anthropic 用 `https://api.onprs.top/v1`；Google 用 `https://api.onprs.top` |
| API Key | 为该客户端创建的独立 Key |
| Model | 渠道监控中的精确模型名 |
| Streaming | 初次排错先关闭，基础请求成功后再开启 |

## 验收方法

1. 先运行客户端的连接测试。
2. 再发送一条短文本生成请求。
3. 最后测试流式输出或工具调用。
4. 在控制台用量记录核对模型、端点、费用和请求 ID。

连接测试如果只请求 `/models`，不能证明生成端点可用。

## 未列出品牌的原因

GUI 和 IDE 插件更新频繁，同一品牌不同版本可能改变协议或 Base URL 拼接方式。本页不收录未经当前版本实际验证的品牌组合。可按上表接入，并把客户端名称、版本、脱敏配置和请求 ID 提交给支持确认。
