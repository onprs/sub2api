---
title: 统一错误排查流程
description: 按服务状态、Base URL、API Key、协议、模型、额度和网络依次定位问题
---

# 统一错误排查流程

不要先反复重试。按下面顺序缩小范围，可以避免把 Key、路径和额度问题误判为服务故障。

## 1. 确认服务状态

打开[渠道状态](https://cdn.api.onprs.top/monitor)和控制台首页，确认是否有维护或渠道异常。若所有 Key、模型和协议都失败，再检查 `https://api.onprs.top/health` 是否可达。

## 2. 检查 Base URL

- OpenAI / Anthropic：`https://api.onprs.top/v1`
- Gemini CLI：`https://api.onprs.top`
- curl 完整端点：例如 `https://api.onprs.top/v1/messages`

打印客户端最终请求 URL，排除 `/v1/v1`、缺少 `/v1`、拼错域名或把文档站端口当成 API 端口。

## 3. 检查 Key

使用控制台 [API Keys](https://cdn.api.onprs.top/keys) 确认 Key 状态、分组、到期时间、额度和 IP 限制。新建临时终端变量做最小 curl，不要在日志中打印完整 Key。

## 4. 检查协议、路径和模型

先发送纯文本、非流式、无工具的最小请求。模型名从[渠道监控](https://cdn.api.onprs.top/monitor)复制。

## 5. 检查余额、套餐和限制

- 标准分组看余额。
- 订阅分组看有效期与 `5h / 7d / 30d` 窗口。
- 同时检查 Key 自身额度、用户并发、RPM 和上游 `429`。

## 6. 检查网络和客户端

使用 curl 与客户端对比。curl 成功而客户端失败，通常是代理、证书、环境变量优先级、旧进程或响应解析问题。

## 快速定位

| 现象或关键字 | 进入 |
| --- | --- |
| `400 / 401 / 403 / 404 / 409 / 429 / 5xx` | [HTTP 状态码](/troubleshooting/status-codes) |
| `INVALID_API_KEY`、`API_KEY_DISABLED`、模型不存在 | [Key、权限与模型](/troubleshooting/auth-model) |
| `INSUFFICIENT_BALANCE`、`USAGE_LIMIT_EXCEEDED`、并发 | [余额、套餐与限流](/troubleshooting/quota) |
| 首字超时、流中断、空响应、工具格式 | [超时、流与工具调用](/troubleshooting/streaming) |
| DNS、TLS、代理、企业网络 | [DNS、TLS 与代理](/troubleshooting/network) |
| 支付未回调、重复支付、退款等待 | [支付与订单](/troubleshooting/payment) |

## 联系支持前

准备发生时间和时区、客户端及版本、请求路径、模型、是否流式、HTTP 状态、完整错误正文、`x-request-id` 和脱敏请求。不要提供完整 Key、密码、Cookie 或登录 Token。
