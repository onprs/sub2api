---
title: 创建与管理 API Key
description: 创建、命名、轮换、禁用和撤销 OnprsCodexApi API Key
---

# 创建与管理 API Key

## 创建 Key

1. 登录后打开 [API Keys](https://cdn-api.onprs.online/keys)。
2. 选择新建 Key。
3. 使用能识别用途的名称，例如 `claude-workstation`、`ci-staging`。
4. 选择与目标模型和计费方式匹配的分组。
5. 如页面提供到期时间、额度或 IP 访问限制，按最小权限原则设置。
6. 创建后直接配置到目标客户端，并通过安全方式传递。

API Key 通常通过以下三种方式之一发送：

```http
Authorization: Bearer sk-your-api-key
```

```http
x-api-key: sk-your-api-key
```

```http
x-goog-api-key: sk-your-api-key
```

OpenAI 兼容客户端通常使用 Bearer；Anthropic 和 Google SDK 可能使用各自的 Key Header。网关均可识别，优先沿用客户端原生方式。

## 选择分组

创建 Key 时以页面可选分组为准。分组决定平台、模型范围和计费方式。创建后使用当前 Key 拉取模型列表，实际返回结果就是该 Key 的可用模型范围。

出现 `403`、模型不可用或订阅不存在时，先检查 Key 分组。

## 轮换 Key

1. 新建一个用途相同的新 Key。
2. 在一个客户端或实例中替换并完成最小请求。
3. 逐个替换其他实例，观察用量和错误。
4. 确认旧 Key 不再使用后，将其禁用。
5. 保留短暂观察期，再永久删除或撤销旧 Key。

完成切换后再撤销旧 Key，可保持客户端连续可用。

## 泄露处理

一旦怀疑泄露，立即禁用或撤销 Key，再创建新 Key。随后检查[用量记录](https://cdn-api.onprs.online/usage)中的时间、模型、IP 和请求 ID，并联系支持说明异常范围。

::: danger 提交脱敏信息
支持人员定位问题通常只需要 Key 名称、页面中的 Key ID、脱敏前后缀、请求 ID 和发生时间。完整 Key 请保留在本地。
:::
