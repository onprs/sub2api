package service

import (
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

func validateAPIKeyCustomHostsBaseURL(raw string, cfg *config.Config, allowCustomHosts bool) (string, error) {
	if cfg == nil {
		return "", errors.New("config is not available")
	}
	policy := cfg.Security.URLAllowlist
	if !policy.Enabled {
		return urlvalidator.ValidateURLFormat(raw, policy.AllowInsecureHTTP)
	}

	// 自定义主机只放宽主机白名单；URL 校验和拨号阶段的 SSRF 策略保持生效。
	options := urlvalidator.ValidationOptions{AllowPrivate: policy.AllowPrivateHosts}
	if !allowCustomHosts {
		options.AllowedHosts = policy.UpstreamHosts
		options.RequireAllowlist = true
	}
	return urlvalidator.ValidateHTTPSURL(raw, options)
}

func validateAnthropicAPIKeyBaseURL(raw string, cfg *config.Config) (string, error) {
	if cfg == nil {
		return "", errors.New("config is not available")
	}
	return validateAPIKeyCustomHostsBaseURL(raw, cfg, cfg.Security.URLAllowlist.AllowAnthropicAPIKeyCustomHosts)
}

func validateOpenAIAPIKeyBaseURLWithPolicy(raw string, cfg *config.Config) (string, error) {
	if cfg == nil {
		return "", errors.New("config is not available")
	}
	return validateAPIKeyCustomHostsBaseURL(raw, cfg, cfg.Security.URLAllowlist.AllowOpenAIAPIKeyCustomHosts)
}
