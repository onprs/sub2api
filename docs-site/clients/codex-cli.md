---
title: Codex CLI
description: 使用控制台导入脚本配置 Codex CLI
---

# Codex CLI

## 1. 下载配置脚本

1. 打开控制台 [API Keys](https://cdn-api.onprs.online/keys)。
2. 在目标 Key 所在行点击“使用密钥”。
3. 确认弹窗中的分组和默认模型。
4. 点击“下载 Windows 脚本”或“下载 macOS/Linux 脚本”。

脚本会备份现有配置，再写入 `~/.codex/config.toml` 和对应凭据。

## 2. 运行脚本

::: code-group

```powershell [Windows PowerShell]
cd "$HOME\Downloads"
.\sub2api-cli-import.bat
```

```bash [macOS / Linux]
cd ~/Downloads
chmod 700 sub2api-cli-import.sh
./sub2api-cli-import.sh
```

:::

脚本询问导入目标时输入 `1`，询问是否设为默认配置时输入 `y`。看到 `Sub2API CLI import finished` 后关闭原有 Codex CLI 会话。

## 3. 启动并验证

打开新终端并运行：

```bash
codex
```

发送 `Reply with exactly: connected`。返回 `connected` 后，在[用量记录](https://cdn-api.onprs.online/usage)确认本次 `/v1/responses` 请求。

## 配置未生效

- 仍使用原 provider：确认运行脚本时第二个选项输入了 `y`，再启动新终端。
- 没有目标模型：在“使用密钥”弹窗确认默认模型，重新下载并运行脚本。
- 返回 `401` 或 `404`：按 [Key、权限与模型](/troubleshooting/auth-model)检查 Key 状态和分组。
