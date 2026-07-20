---
title: OpenCode
description: 使用控制台导入脚本配置 OpenCode
---

# OpenCode

## 1. 下载配置脚本

1. 打开控制台 [API Keys](https://cdn-api.onprs.online/keys)。
2. 在目标 Key 所在行点击“使用密钥”。
3. 确认弹窗中的分组和默认模型。
4. 点击“下载 Windows 脚本”或“下载 macOS/Linux 脚本”。

脚本会备份现有配置，添加 OnprsCodexApi provider，并导入当前 Key 的模型列表。

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

脚本询问导入目标时输入 `2`，询问是否设为默认配置时输入 `y`。OpenCode Desktop 已运行时，按脚本提示输入 `y` 完成重启。

## 3. 选择模型并验证

1. 启动 OpenCode，并新建会话。
2. 运行 `/models`，选择 `OnprsCodexApi` provider 下的模型。
3. 发送 `Reply with exactly: connected`。

返回 `connected` 后，在[用量记录](https://cdn-api.onprs.online/usage)确认本次请求。

## 配置未生效

- 看不到 `OnprsCodexApi`：完全退出 OpenCode Desktop 和 sidecar，再重新打开。
- 模型列表需要更新：重新下载并运行脚本。
- 返回 `401` 或 `404`：按 [Key、权限与模型](/troubleshooting/auth-model)检查 Key 状态和分组。
