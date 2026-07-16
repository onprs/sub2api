---
title: 快速开始
description: 从注册、创建 API Key 到发送第一条 OnprsCodexApi 请求
---

# 快速开始

准备控制台账号、API Key 和可用模型名后，按以下步骤完成接入。

## 1. 登录控制台

打开 [OnprsCodexApi 控制台](https://cdn.api.onprs.top/)，按页面提示完成注册和登录。

登录后进入[个人中心](https://cdn.api.onprs.top/profile)，确认邮箱和账号状态正常。不要与他人共用登录密码。

## 2. 创建 API Key

进入 [API Keys](https://cdn.api.onprs.top/keys)，为当前设备或应用创建独立 Key。名称建议包含用途和环境，例如 `codex-laptop` 或 `team-app-prod`。

创建时选择与你要调用的平台、模型和计费方式匹配的分组。完整步骤见[创建与管理 API Key](/getting-started/api-key)。

::: warning Key 只展示给需要使用它的程序
API Key 不是登录凭据。不要把完整 Key 发给客服、写入 Git、截图或前端代码。
:::

## 3. 确认模型和协议

进入[可用渠道](https://cdn.api.onprs.top/available-channels)确认分组当前可用模型，再根据客户端选择协议：

| 使用方式 | Base URL | 主要生成端点 |
| --- | --- | --- |
| OpenAI Chat Completions | `https://api.onprs.top/v1` | `/chat/completions` |
| OpenAI Responses / Codex | `https://api.onprs.top/v1` | `/responses` |
| Anthropic / Claude Code | `https://api.onprs.top/v1` | `/messages` |
| Google GenAI / Gemini | `https://api.onprs.top` | `/v1beta/models/{model}:generateContent` |

协议完整边界见[协议与端点](/api/protocols)。

## 4. 发送最小请求

先设置两个临时变量：

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
- 返回正文中有模型输出，而不是空字符串或错误对象。
- 响应头或正文中可找到请求 ID 时予以保留。
- [用量记录](https://cdn.api.onprs.top/usage)稍后能看到对应请求、模型和费用。

下一步：[配置常用客户端](/clients/)。
