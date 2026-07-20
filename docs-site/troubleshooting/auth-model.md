---
title: Key、权限与模型排查
description: 排查无效 Key、禁用、到期、IP 限制、分组权限和模型不可用
---

# Key、权限与模型排查

## Key 错误对照

| 错误码 | 含义 | 修复 |
| --- | --- | --- |
| `API_KEY_REQUIRED` | 请求没有可识别的 Key | 添加 Bearer、`x-api-key` 或 `x-goog-api-key` |
| `INVALID_API_KEY` | Key 不存在或复制错误 | 从控制台复制完整值并重新配置 |
| `API_KEY_DISABLED` / `API_KEY_INACTIVE` | Key 已禁用 | 启用或新建 Key |
| `API_KEY_EXPIRED` | Key 到期 | 创建新 Key 或调整允许的到期设置 |
| `API_KEY_QUOTA_EXHAUSTED` | Key 自身额度耗尽 | 查看独立的 Key 额度设置 |
| `ACCESS_DENIED` | IP 白/黑名单拒绝 | 核对服务识别到的出口 IP |
| `USER_INACTIVE` | 账号状态不可用 | 登录控制台确认或联系支持 |

## Header 选择

先用 Bearer 做通用测试：

```bash
curl -i https://cdn-api.onprs.online/v1/models \
  -H "Authorization: Bearer $ONPRS_API_KEY"
```

若 SDK 自动使用 `x-api-key` 或 `x-goog-api-key`，每次请求保持一个一致的 Key 值。

## 分组和平台

Key 在创建时绑定分组，分组决定平台、模型范围和计费类型。切换分组时创建对应的新 Key。

1. 打开 API Keys 查看 Key 分组。
2. 使用当前 Key 拉取 `/v1/models` 或 `/v1beta/models`。
3. 在渠道监控确认提供商和渠道状态。
4. 若计费方式不匹配，创建绑定正确分组的新 Key。

## 模型调用检查

错误正文可能是 `model not found`、`unsupported model`、`no available accounts supporting model` 或客户端自己的模型校验。

- 从当前 Key 实际返回的模型列表中复制模型名。
- 调用 `/v1/models` 验证模型发现，再做最小生成。
- 先使用纯文本基础参数，再逐项加入图片、工具或采样参数。
- 确认客户端发送的最终模型名。
- 只有一个模型受影响时，查看该模型和账号池状态。

## 客户端缓存

Claude Code、Codex CLI 和 OpenCode 可能缓存模型或 provider。配置更新后完全退出进程，OpenCode Desktop 还需停止 sidecar 并新建会话。旧会话持续报相同错误时，用新会话验证。
