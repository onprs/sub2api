---
title: HTTP 状态码排查
description: 排查 OnprsCodexApi 400、401、403、404、409、429 和 5xx
---

# HTTP 状态码排查

先读取错误正文中的 `code`、`type`、`reason` 和 `message`。相同状态码可能来自网关、上游或客户端前置检查。

## 状态码速查

| 状态 | 常见原因 | 第一动作 |
| --- | --- | --- |
| `400` | JSON、字段、协议或模型参数无效 | 使用纯文本非流式请求验证 |
| `401` | Key 缺失、无效、禁用或用户不可用 | 检查认证 Header 和 Key 状态 |
| `403` | Key 到期、IP/分组权限、余额或订阅无效 | 查看机器错误码和 Key 分组 |
| `404` | URL/端点错误、模型不存在、能力不支持 | 打印最终 URL 并核对协议矩阵 |
| `409` | 重复操作、状态冲突或资源正在处理 | 查询当前状态并按状态继续 |
| `429` | Key/订阅额度、RPM、并发或上游限流 | 读取错误码和重试时间，按间隔重试 |
| `5xx` | 上游、网关依赖、转换或临时服务故障 | 保留请求 ID，短暂退避后重试一次 |

## 400 Bad Request

1. 用 JSON 校验器检查请求体。
2. 删除工具、图片、schema、reasoning 和采样参数。
3. 确认 Chat 使用 `messages`，Responses 使用 `input`，Google 使用 `contents`。
4. 检查 `Content-Type: application/json`。
5. 若错误点名某字段，删除或调整该字段后再次验证。

## 401 Unauthorized

`API_KEY_REQUIRED` 表示服务未从 Bearer、`x-api-key` 或 `x-goog-api-key` 读到 Key。`INVALID_API_KEY` 表示 Key 不存在；`API_KEY_DISABLED` 表示已禁用。

确认客户端已读取配置文件、凭据存储或当前会话中的 Key。

## 403 Forbidden

常见错误包括 `API_KEY_EXPIRED`、`ACCESS_DENIED`、`INSUFFICIENT_BALANCE`、无有效订阅和分组不可用。`ACCESS_DENIED` 可能返回当前识别到的 IP，用于核对 Key 的 IP 规则。

## 404 Not Found

先区分：

- OpenResty/HTML 404：通常是域名或 URL 路径错误。
- JSON API 404：通常是端点、模型或平台能力不支持。
- OpenCode Go Responses WebSocket / 子路径 404：使用根级 HTTP Responses。

## 409 Conflict

订单正在处理、兑换码已用、订阅状态限制、重复创建或并发状态更新都可能返回 409。重新获取资源状态，并按当前状态继续。

## 429 Too Many Requests

读取机器错误码区分 Key 额度、订阅窗口、RPM、并发和上游容量。若响应提供 `Retry-After`，遵守该时间；否则使用指数退避并限制总重试次数。

## 5xx

使用相同 Key 和模型做一次纯文本非流式请求。若仍失败，换同分组另一个基础模型；两者都失败且渠道状态异常时联系支持。5xx 重试应设置间隔和最大次数。
