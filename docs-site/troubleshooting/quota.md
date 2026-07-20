---
title: 余额、套餐与限流排查
description: 排查余额不足、订阅到期、滚动窗口、Key 额度、并发、RPM 和上游 429
---

# 余额、套餐与限流排查

## 先区分限制来源

| 错误 | 来源 | 处理 |
| --- | --- | --- |
| `INSUFFICIENT_BALANCE` | 标准计费分组的账号余额不足 | 充值或改用有订阅权益的正确分组 Key |
| `SUBSCRIPTION_NOT_FOUND` / 无有效订阅 | 订阅分组无可用权益 | 检查套餐分组、状态和到期时间 |
| `USAGE_LIMIT_EXCEEDED` | 订阅窗口耗尽 | 查看具体窗口与重置时间 |
| `API_KEY_QUOTA_EXHAUSTED` | Key 自身额度耗尽 | 检查 Key 额度设置 |
| RPM / concurrency / rate limit | 请求频率或同时请求过多 | 降低并发，按重试时间退避 |
| provider `RESOURCE_EXHAUSTED` | 上游模型容量或账号限流 | 等待或切换当前可用模型 |

余额、订阅和 Key 额度分别计算。具体计费来源由 Key 分组决定。

## 滚动窗口耗尽

错误正文可能包含：

- `FIVE_HOUR_LIMIT_EXCEEDED`
- `SEVEN_DAY_LIMIT_EXCEEDED`
- `THIRTY_DAY_LIMIT_EXCEEDED`

打开[我的订阅](https://cdn-api.onprs.online/subscriptions)，查看哪个窗口为零、何时重置。多个窗口同时生效，必须全部有剩余。

## 并发限制

并发表示同时占用的请求数量，不等同于每分钟请求数。流式请求会在连接持续期间占用并发。

- 取消客户端自动开启的多个 agent 或并行任务。
- 检查异常长时间未结束的流。
- 为重试设置间隔和最大次数。
- 客户端断开后，等待当前请求完成计费再发起替代请求。

## RPM 和上游 429

服务端 RPM、Key 级限速和上游模型容量都可能返回 429。遵守 `Retry-After` 或错误中的 retry delay。无明确时间时可使用 1、2、4、8 秒退避，并在少量尝试后停止。

切换模型时逐个验证，并保持受控并发。

## 页面用量与请求仍不一致

1. 刷新订阅和用量页面，排除旧缓存。
2. 确认正在查看正确的 Key、分组和套餐。
3. 保留窗口开始、重置时间、已用和剩余的截图。
4. 提供一个失败请求 ID 和紧邻的成功请求 ID 给支持。
