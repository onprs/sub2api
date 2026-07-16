---
title: DNS、TLS 与代理排查
description: 排查域名解析、证书、系统时间、HTTP 代理和企业网络连接问题
---

# DNS、TLS 与代理排查

## 分层检查

```bash
curl -I --connect-timeout 10 https://cdn.api.onprs.top/
curl -i --connect-timeout 10 https://api.onprs.top/health
```

首页和 health 均不可达时，再检查 DNS、TLS 和代理；health 可达但 API 失败通常不是基础网络问题。

## DNS

```bash
nslookup api.onprs.top
```

确认没有被本机 hosts、企业 DNS 或代理插件解析到错误地址。更换公共 DNS 只能用于诊断，企业网络用户应遵守组织策略。

## TLS 与系统时间

证书尚未生效、过期、域名不匹配或本机时间偏差都可能导致 TLS 错误。

- 自动同步系统日期、时间和时区。
- 不要使用 `curl -k` 作为长期修复。
- 企业 HTTPS 检查需要把组织 CA 正确安装到客户端信任库，而不是关闭证书校验。
- IP 地址不能替代 `https://api.onprs.top` 进行 API TLS 调用。

## HTTP 代理

检查 `HTTP_PROXY`、`HTTPS_PROXY`、`ALL_PROXY` 和客户端内置代理。代理可能：

- 修改 SNI、证书或请求 Header。
- 缓冲 SSE，导致流式输出一次性出现。
- 在空闲时间关闭长连接。
- 去掉 Authorization 或限制请求体大小。

用不经过代理的受控网络做一次对照。不要在公共网络中关闭 TLS 校验。

## Cloudflare、CDN 和企业网关

收到 HTML challenge、WAF 页面或代理品牌错误页时，它不是模型 API JSON。保存状态码、响应头、页面标题和发生时间，检查浏览器验证、出口 IP、User-Agent 和企业策略。

## 服务域名

- API 请求使用 `https://api.onprs.top`。
- 控制台使用 `https://cdn.api.onprs.top`。
- 文档使用 `https://doc.api.onprs.top`。

控制台和文档域名均不能作为 API Base URL。
