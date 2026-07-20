---
title: DNS、TLS 与代理排查
description: 排查域名解析、证书、系统时间、HTTP 代理和企业网络连接问题
---

# DNS、TLS 与代理排查

## 分层检查

```bash
curl -I --connect-timeout 10 https://cdn-api.onprs.online/
curl -i --connect-timeout 10 https://cdn-api.onprs.online/health
```

首页和 health 均不可达时，依次检查 DNS、TLS 和代理。health 可达时，继续检查 API 配置。

## DNS

```bash
nslookup cdn-api.onprs.online
```

核对本机 hosts、企业 DNS 和代理插件的解析结果。公共 DNS 可用于诊断，企业网络用户按组织策略操作。

## TLS 与系统时间

证书尚未生效、过期、域名不匹配或本机时间偏差都可能导致 TLS 错误。

- 自动同步系统日期、时间和时区。
- 保持 TLS 证书校验开启。
- 企业 HTTPS 检查环境应将组织 CA 安装到客户端信任库。
- API TLS 调用使用 `https://cdn-api.onprs.online` 域名。

## HTTP 代理

检查 `HTTP_PROXY`、`HTTPS_PROXY`、`ALL_PROXY` 和客户端内置代理。代理可能：

- 修改 SNI、证书或请求 Header。
- 缓冲 SSE，导致流式输出一次性出现。
- 在空闲时间关闭长连接。
- 去掉 Authorization 或限制请求体大小。

使用受控的直连网络做一次对照，并保持 TLS 校验开启。

## Cloudflare、CDN 和企业网关

收到 HTML challenge、WAF 页面或代理品牌错误页时，保存状态码、响应头、页面标题和发生时间，并检查浏览器验证、出口 IP、User-Agent 和企业策略。

## 服务域名

- API 与控制台使用 `https://cdn-api.onprs.online`。
- 文档使用 `https://doc-api.onprs.online`。

API Base URL 使用 `https://cdn-api.onprs.online`。
