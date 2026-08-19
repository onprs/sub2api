package service

import "strings"

func canonicalBillingModelForPricing(model string) string {
	raw := strings.ToLower(strings.TrimSpace(model))
	if raw == "" {
		return ""
	}

	switch billingModelAliasLookupKey(raw) {
	case "kimi-k2.7-code":
		return "kimi-k2.7"
	case "gemini-pro-agent":
		return "gemini-3.1-pro-high"
	case "gemini-3-flash-agent":
		return "gemini-3.5-flash"
	case "gemini-3.7-flash-high", "gemini-3.7-flash-medium", "gemini-3.7-flash-low", "gemini-3.7-flash-tiered":
		return "gemini-3.7-flash"
	case "gemini-3.6-flash-high", "gemini-3.6-flash-medium", "gemini-3.6-flash-low", "gemini-3.6-flash-tiered":
		return "gemini-3.6-flash"
	case "gemini-3.5-flash-high", "gemini-3.5-flash-medium", "gemini-3.5-flash-low", "gemini-3.5-flash-extra-low":
		return "gemini-3.5-flash"
	case "gemini-2.5-flash-thinking":
		return "gemini-2.5-flash"
	default:
		return raw
	}
}

func billingModelPricingCandidates(model string) []string {
	raw := strings.ToLower(strings.TrimSpace(model))
	if raw == "" {
		return nil
	}

	seen := make(map[string]struct{}, 6)
	candidates := make([]string, 0, 6)
	add := func(candidate string) {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate == "" {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}

	add(raw)
	if strings.HasPrefix(raw, "cline-pass/") {
		suffix := strings.TrimPrefix(raw, "cline-pass/")
		add("clinepass/" + suffix)
		add(suffix)
	} else if strings.HasPrefix(raw, "clinepass/") {
		suffix := strings.TrimPrefix(raw, "clinepass/")
		add("cline-pass/" + suffix)
		add(suffix)
	} else if strings.HasPrefix(raw, "openrouter/") {
		suffix := strings.TrimPrefix(raw, "openrouter/")
		add(suffix)
	}
	lookupKey := billingModelAliasLookupKey(raw)
	add(lookupKey)
	canonical := canonicalBillingModelForPricing(raw)
	add(canonical)
	if lookupKey != "" {
		add(canonicalBillingModelForPricing(lookupKey))
	}
	return candidates
}

func billingModelAliasLookupKey(model string) string {
	key := strings.ToLower(strings.TrimSpace(model))
	key = strings.TrimLeft(key, "/")
	key = trimOpenCodeGoModelProviderPrefix(key)
	key = trimClinePassModelProviderPrefix(key)
	key = trimOpenRouterModelProviderPrefix(key)
	key = strings.TrimPrefix(key, "models/")
	key = trimOpenCodeGoModelProviderPrefix(key)
	key = strings.TrimPrefix(key, "publishers/google/models/")
	if idx := strings.LastIndex(key, "/publishers/google/models/"); idx != -1 {
		key = key[idx+len("/publishers/google/models/"):]
	}
	if idx := strings.LastIndex(key, "/models/"); idx != -1 {
		key = key[idx+len("/models/"):]
	}
	key = trimOpenCodeGoModelProviderPrefix(key)
	key = trimClinePassModelProviderPrefix(key)
	key = trimOpenRouterModelProviderPrefix(key)
	return strings.TrimSpace(strings.TrimLeft(key, "/"))
}

func trimClinePassModelProviderPrefix(key string) string {
	key = strings.TrimPrefix(key, "cline-pass/")
	key = strings.TrimPrefix(key, "clinepass/")
	return key
}

func trimOpenRouterModelProviderPrefix(key string) string {
	key = strings.TrimPrefix(key, "openrouter/")
	return key
}

func trimOpenCodeGoModelProviderPrefix(key string) string {
	key = strings.TrimPrefix(key, "opencode-go/")
	key = strings.TrimPrefix(key, "opencode_go/")
	return key
}
