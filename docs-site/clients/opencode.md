---
title: OpenCode
description: 使用自定义 OpenAI-compatible provider 连接 OpenCode 与 OnprsCodexApi
---

# OpenCode

适用版本：OpenCode `1.18.2`，最后核对日期 2026-07-16。自定义 provider 和 `baseURL` 规则见 [OpenCode Providers](https://opencode.ai/docs/providers)。

## 配置文件

OpenCode 通常读取 `~/.config/opencode/opencode.jsonc` 或 `opencode.json`。建议让 Key 单独保存在权限受限文件中：

```bash
mkdir -p ~/.config/opencode
printf '%s' 'sk-your-api-key' > ~/.config/opencode/onprs.key
chmod 600 ~/.config/opencode/onprs.key
```

Windows 可使用控制台 [API Keys](https://cdn.api.onprs.top/keys) 的“使用 Key”导入器，以避免手工处理权限和路径。

## 添加 provider

把 `replace-with-an-available-model` 在 provider 模型表和顶层 `model` 中同时替换：

```json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "onprs": {
      "name": "OnprsCodexApi",
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "baseURL": "https://api.onprs.top/v1",
        "apiKey": "{file:~/.config/opencode/onprs.key}"
      },
      "models": {
        "replace-with-an-available-model": {
          "name": "Onprs model"
        }
      }
    }
  },
  "model": "onprs/replace-with-an-available-model"
}
```

控制台导入器会按 Key 分组生成实际模型表，优先使用该结果，避免长期维护静态模型清单。

## OpenCode Go 说明

OpenCode Go 是独立平台，不是 OpenAI 或 Anthropic 的别名。Sub2API 根据账号的模型协议映射，把请求发送到实际 Chat Completions 或 Messages 上游；普通用户仍应使用控制台为该 Key 生成的 provider 和模型列表。

OpenCode Go 支持根级 HTTP Responses 生成，但不支持 Responses WebSocket 和专用 `/responses/*` 子路径。客户端遇到这类调用时应改用普通 HTTP 生成入口。

## 最小测试

完全退出 OpenCode Desktop 的后台进程后重启，运行 `/models` 选择 `onprs/...`，再发送 `Reply with exactly: connected`。仅看到模型不代表生成链路已成功。

## 恢复原配置

删除 `provider.onprs` 和指向它的顶层 `model`，再删除 `onprs.key`。重启 OpenCode Desktop 和 sidecar；旧会话仍显示缓存错误时，新建会话。
