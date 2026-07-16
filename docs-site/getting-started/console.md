---
title: 查看模型与用量
description: 在 OnprsCodexApi 控制台查看渠道、模型、用量、订阅和订单
---

# 查看模型与用量

模型、价格、库存和账号状态等动态信息以控制台显示为准。

## 可用渠道

打开[可用渠道](https://cdn.api.onprs.top/available-channels)，按 API Key 所属分组确认平台和模型。模型列表会随渠道状态和分组配置更新。

找不到模型时继续检查[模型与映射](/api/models)。

## 模型价格

打开[模型价格](https://cdn.api.onprs.top/model-pricing)，查看输入、输出、缓存、图片或按次计费信息。实际费用以用量记录为准。

## 用量记录

打开[用量记录](https://cdn.api.onprs.top/usage)，可按时间和模型检查请求。定位异常时重点保留：

- 请求时间和请求 ID。
- 请求模型与实际上游模型。
- 协议或请求类型。
- 输入、输出及缓存 Token。
- 计费类型、倍率和费用。
- 首字时间与总耗时。

记录写入可能略晚于 API 响应，不要在请求刚结束时反复提交相同调用。

## 订阅与订单

- [我的订阅](https://cdn.api.onprs.top/subscriptions)：状态、到期时间和滚动额度进度。
- [购买套餐](https://cdn.api.onprs.top/purchase)：当前可售价格、库存、有效期和权益。
- [我的订单](https://cdn.api.onprs.top/orders)：支付和权益发放状态。
- [兑换码](https://cdn.api.onprs.top/redeem)：兑换并查看最近记录。

相关规则见[套餐、计费与额度](/plans/)。
