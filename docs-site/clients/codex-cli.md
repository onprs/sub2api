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

## 配置文件

控制台“使用 Key”可生成 `config.toml` 和 `auth.json`。Codex CLI 会从 `auth.json` 读取 Key，无需设置系统环境变量。

`config.toml` 位于 `~/.codex/config.toml`，Windows 路径为 `%USERPROFILE%\.codex\config.toml`。把模型占位符替换为 `/v1/models` 实际返回的模型名：

```toml
model_provider = "OpenAI"
model = "replace-with-an-available-model"

[model_providers.OpenAI]
name = "OpenAI"
base_url = "https://cdn-api.onprs.online/v1"
wire_api = "responses"
requires_openai_auth = true
```

在同目录的 `auth.json` 中保存 Key：

```json
{
  "OPENAI_API_KEY": "sk-your-api-key"
}
```

已有配置时，按控制台生成结果合并对应字段，并保留 MCP、sandbox 与其他 provider。

## 配置验证

先运行 `codex --strict-config --version` 检查配置字段，再进入一个测试目录启动 `codex`，发送 `Reply with exactly: connected`。成功后在用量记录确认入站端点为 Responses。

## 配置检查与恢复

- `401`：确认 `auth.json` 中的 Key、Key 状态和当前 provider。
- `404`：核对 `base_url` 的 `/v1` 地址和 Key 分组。
- 恢复时还原原有 `config.toml` 和 `auth.json`，再重启 Codex CLI。
