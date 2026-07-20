---
title: 快速开始
description: 从注册、创建 API Key 到发送第一条 OnprsCodexApi 请求
---

# 快速开始

准备控制台账号后，按以下步骤创建 API Key、拉取模型并完成接入。

## 1. 登录控制台

打开 [OnprsCodexApi 控制台](https://cdn-api.onprs.online/)，按页面提示完成注册和登录。

登录后进入[个人中心](https://cdn-api.onprs.online/profile)，确认邮箱和账号状态，并为账号使用独立密码。

## 2. 创建 API Key

进入 [API Keys](https://cdn-api.onprs.online/keys)，为当前设备或应用创建独立 Key。名称建议包含用途和环境，例如 `codex-laptop` 或 `team-app-prod`。

创建时选择与你要调用的平台、模型和计费方式匹配的分组。完整步骤见[创建与管理 API Key](/getting-started/api-key)。

::: warning 妥善保管 API Key
API Key 仅配置给需要调用接口的程序，并保存在安全存储中。支持排查使用 Key 名称、ID 和脱敏前后缀即可。
:::

## 3. 拉取模型并确认协议

使用当前 Key 请求 `GET /v1/models`；Google GenAI 客户端请求 `GET /v1beta/models`。从实际返回的模型列表中选择模型，再根据客户端选择协议：

| 使用方式 | Base URL | 主要生成端点 |
| --- | --- | --- |
| OpenAI Chat Completions | `https://cdn-api.onprs.online/v1` | `/chat/completions` |
| OpenAI Responses / Codex | `https://cdn-api.onprs.online/v1` | `/responses` |
| Anthropic / Claude Code | `https://cdn-api.onprs.online/v1` | `/messages` |
| Google GenAI / Gemini | `https://cdn-api.onprs.online` | `/v1beta/models/{model}:generateContent` |

协议完整边界见[协议与端点](/api/protocols)。

## 4. 发送第一条请求

以下示例使用 shell 变量复用 Key 和模型名。它们只服务于示例命令；实际客户端可使用配置文件、凭据存储或客户端自带的 Key 设置。

::: code-group

```bash [macOS / Linux]
export ONPRS_API_KEY="sk-your-api-key"
export ONPRS_MODEL="replace-with-an-available-model"
```

```powershell [Windows PowerShell]
$env:ONPRS_API_KEY="sk-your-api-key"
$env:ONPRS_MODEL="replace-with-an-available-model"
```

:::

然后按[发送第一条请求](/getting-started/first-request)中的任一协议示例测试。

## 成功判据

- HTTP 状态为 `200`。
- 返回正文包含模型输出。
- 响应头或正文中可找到请求 ID 时予以保留。
- [用量记录](https://cdn-api.onprs.online/usage)稍后能看到对应请求、模型和费用。

下一步：[配置常用客户端](/clients/)。
