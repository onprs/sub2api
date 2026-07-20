---
title: Codex CLI
description: 使用 OpenAI Responses 协议连接 Codex CLI 与 OnprsCodexApi
---

# Codex CLI

适用版本：Codex CLI `0.144.1`，最后核对日期 2026-07-16。配置字段参照 [Codex configuration reference](https://developers.openai.com/codex/config-reference)。

## 前置条件

- Key 分组支持目标模型的 Responses 请求。
- `codex --version` 能正常运行。
- 已使用当前 Key 从 `/v1/models` 取得模型名。

## 设置 Key 环境变量

::: code-group

```bash [macOS / Linux]
export ONPRS_API_KEY="sk-your-api-key"
```

```powershell [Windows PowerShell]
$env:ONPRS_API_KEY="sk-your-api-key"
```

:::

通过 `ONPRS_API_KEY` 环境变量向 Codex CLI 提供 Key。

## 配置 provider

编辑 `~/.codex/config.toml`，Windows 路径为 `%USERPROFILE%\.codex\config.toml`。把模型占位符替换为 `/v1/models` 实际返回的模型名：

```toml
model_provider = "onprs"
model = "replace-with-an-available-model"

[model_providers.onprs]
name = "OnprsCodexApi"
base_url = "https://cdn-api.onprs.online/v1"
env_key = "ONPRS_API_KEY"
wire_api = "responses"
```

已有配置时，合并顶层默认值和 `[model_providers.onprs]` 表，并保留 MCP、sandbox 与其他 provider。

## 配置验证

先运行 `codex --strict-config --version` 检查配置字段，再进入一个测试目录启动 `codex`，发送 `Reply with exactly: connected`。成功后在用量记录确认入站端点为 Responses。

## WebSocket 模式

普通 HTTP Responses 是首选基线。只有控制台生成配置明确启用且当前分组支持时，才增加：

```toml
supports_websockets = true

[features]
responses_websockets_v2 = true
```

WebSocket 握手未成功时，先用普通 HTTP 验证基础生成链路。

## 配置检查与恢复

- `Missing environment variable`：新终端没有继承 `ONPRS_API_KEY`。
- `401`：变量值错误、Key 已禁用或旧官方登录覆盖了 provider。
- `404`：核对 `base_url` 的 `/v1` 地址和 Key 分组。
- 恢复时把 `model_provider` 改回原值，并删除 `[model_providers.onprs]`；环境变量一并移除。
