//go:build unit

package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// capabilityRemote 是可编排的远程目录桩：按 URL 返回 provider 级 / 模型级目录，
// 并可通过 failNext 模拟临时失败。
type capabilityRemote struct {
	mu             sync.Mutex
	body           []byte // provider 级目录（api.json）
	canonical      []byte // 模型级目录（models.json）；为空时返回 ErrNotExist
	failNext       error
	fetchCall      int // provider 级目录下载次数
	canonicalCalls int // 模型级目录下载次数
}

func (r *capabilityRemote) FetchPricingJSON(_ context.Context, url string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// 计数按“尝试次数”计，失败的重试也要算进去。
	if strings.Contains(url, "models.json") {
		r.canonicalCalls++
	} else {
		r.fetchCall++
	}
	if r.failNext != nil {
		err := r.failNext
		r.failNext = nil
		return nil, err
	}
	if strings.Contains(url, "models.json") {
		if len(r.canonical) == 0 {
			return nil, os.ErrNotExist
		}
		return r.canonical, nil
	}
	return r.body, nil
}

func (r *capabilityRemote) FetchHashText(context.Context, string) (string, error) {
	return "", nil
}

func (r *capabilityRemote) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fetchCall
}

const capabilityCatalogJSON = `{
  "openai": {
    "models": {
      "gpt-5.6-sol": {
        "reasoning": true,
        "tool_call": true,
        "modalities": {"input": ["text", "image", "pdf"], "output": ["text"]},
        "limit": {"context": 1050000, "output": 128000}
      },
      "gpt-image-2": {
        "reasoning": false,
        "tool_call": false,
        "modalities": {"input": ["text"], "output": ["image"]},
        "limit": {"context": 32000, "output": 8000}
      },
      "no-capability-fields": {}
    }
  },
  "meta": {
    "models": {
      "muse-spark-1.2-contributor": {
        "reasoning": true,
        "tool_call": true,
        "modalities": {"input": ["text", "image"], "output": ["text"]},
        "limit": {"context": 1048576, "output": 131072}
      }
    }
  },
  "google": {
    "models": {
      "gemini-3.7-flash": {
        "reasoning": true,
        "tool_call": true,
        "modalities": {"input": ["text", "image", "pdf"], "output": ["text"]},
        "limit": {"context": 1048576, "output": 65536}
      }
    }
  },
  "unmapped-provider": {
    "models": {"x": {"reasoning": true}}
  },
  "deepseek": {
    "models": {
      "deepseek-v4-pro": {
        "reasoning": true,
        "tool_call": true,
        "modalities": {"input": ["text"], "output": ["text"]},
        "limit": {"context": 1000000, "output": 384000}
      },
      "deepseek-v4-flash": {
        "reasoning": true,
        "tool_call": true,
        "modalities": {"input": ["text"], "output": ["text"]},
        "limit": {"context": 1000000, "output": 384000}
      },
      "deepseek-v4-unknown-limit": {
        "reasoning": true,
        "tool_call": false,
        "modalities": {"input": ["text"], "output": ["text"]},
        "limit": {"context": 8000, "output": 1000}
      }
    }
  },
  "thinkingmachines": {
    "models": {
      "thinkingmachines/Inkling": {
        "reasoning": true,
        "tool_call": true,
        "modalities": {"input": ["text", "image"], "output": ["text"]},
        "limit": {"context": 65536, "output": 65536}
      },
      "thinkingmachines/Inkling:peft:262144": {
        "reasoning": true,
        "tool_call": true,
        "modalities": {"input": ["text", "image"], "output": ["text"]},
        "limit": {"context": 262144, "output": 65536}
      }
    }
  },
  "nvidia": {
    "models": {
      "nvidia/nemotron-3-ultra-550b-a55b": {
        "reasoning": true,
        "tool_call": true,
        "modalities": {"input": ["text"], "output": ["text"]},
        "limit": {"context": 1000000, "output": 65536}
      }
    }
  }
}`

func TestModelCapabilityCatalog_LookupMapsPlatformToProvider(t *testing.T) {
	catalog := newModelCapabilityCatalog(&capabilityRemote{body: []byte(capabilityCatalogJSON)}, nil)
	ctx := context.Background()

	got, ok := catalog.lookup(ctx, PlatformOpenAI, "GPT-5.6-SOL")
	require.True(t, ok)
	require.Equal(t, 1_050_000, got.ContextTokens)
	require.Equal(t, 128_000, got.MaxOutputTokens)
	require.True(t, got.Reasoning)
	require.True(t, got.ToolCall)
	require.True(t, got.Vision)
	require.True(t, got.PDFInput)
	require.False(t, got.ImageOutput)

	// Gemini 与 Antigravity 共用 google provider。
	got, ok = catalog.lookup(ctx, PlatformAntigravity, "gemini-3.7-flash")
	require.True(t, ok)
	require.Equal(t, 1_048_576, got.ContextTokens)
	require.True(t, got.Reasoning)

	// 图片输出能力单独表达。
	got, ok = catalog.lookup(ctx, PlatformOpenAI, "gpt-image-2")
	require.True(t, ok)
	require.True(t, got.ImageOutput)
	require.False(t, got.Vision)

	// 目录收录但字段全缺：返回零值能力，由调用方按缺失处理。
	got, ok = catalog.lookup(ctx, PlatformOpenAI, "no-capability-fields")
	require.True(t, ok)
	require.Equal(t, ModelCapability{}, got)

	// 平台未映射到任何 provider。
	_, ok = catalog.lookup(ctx, PlatformCommandCode, "gpt-5.6-sol")
	require.False(t, ok)

	// provider 已映射但模型未收录。
	_, ok = catalog.lookup(ctx, PlatformOpenAI, "unknown-model")
	require.False(t, ok)
}

func TestModelCapabilityCatalog_LoadsOnceWithinTTL(t *testing.T) {
	remote := &capabilityRemote{body: []byte(capabilityCatalogJSON)}
	catalog := newModelCapabilityCatalog(remote, nil)
	ctx := context.Background()

	// 并发首次查询只能触发一次远程加载，且每个查询都能拿到结果。
	var wg sync.WaitGroup
	results := make([]bool, 8)
	for i := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, ok := catalog.lookup(ctx, PlatformOpenAI, "gpt-5.6-sol")
			results[index] = ok
		}(i)
	}
	wg.Wait()
	for _, ok := range results {
		require.True(t, ok)
	}
	require.Equal(t, 1, remote.calls())

	_, ok := catalog.lookup(ctx, PlatformOpenAI, "gpt-5.6-sol")
	require.True(t, ok)
	require.Equal(t, 1, remote.calls(), "TTL 内不得重复下载目录")
}

func TestModelCapabilityCatalog_FailureReturnsCachedSnapshotAndThrottlesRetry(t *testing.T) {
	remote := &capabilityRemote{body: []byte(capabilityCatalogJSON)}
	catalog := newModelCapabilityCatalog(remote, nil)
	ctx := context.Background()

	_, ok := catalog.lookup(ctx, PlatformOpenAI, "gpt-5.6-sol")
	require.True(t, ok)
	require.Equal(t, 1, remote.calls())

	// 让快照过期并让下一次加载失败：旧快照必须继续可用。
	catalog.mu.Lock()
	catalog.loadedAt = time.Now().Add(-2 * modelCapabilityCatalogTTL)
	catalog.mu.Unlock()
	remote.mu.Lock()
	remote.failNext = errors.New("temporary upstream failure")
	remote.mu.Unlock()

	got, ok := catalog.lookup(ctx, PlatformOpenAI, "gpt-5.6-sol")
	require.True(t, ok, "加载失败时必须回退到上一份快照")
	require.Equal(t, 1_050_000, got.ContextTokens)
	require.Equal(t, 2, remote.calls())

	// 失败冷却期内不得反复重试。
	_, ok = catalog.lookup(ctx, PlatformOpenAI, "gpt-5.6-sol")
	require.True(t, ok)
	require.Equal(t, 2, remote.calls())

	// 冷却结束后重试成功。
	catalog.mu.Lock()
	catalog.lastFailure = time.Now().Add(-2 * modelCapabilityCatalogRetryDelay)
	catalog.mu.Unlock()
	_, ok = catalog.lookup(ctx, PlatformOpenAI, "gpt-5.6-sol")
	require.True(t, ok)
	require.Equal(t, 3, remote.calls())
}

func TestModelCapabilityProviderForPlatform(t *testing.T) {
	cases := map[string]string{
		PlatformOpenAI:      "openai",
		PlatformAnthropic:   "anthropic",
		PlatformGemini:      "google",
		PlatformAntigravity: "google",
		PlatformGrok:        "xai",
		PlatformOpenCodeGo:  "opencode-go",
		PlatformClinePass:   "cline-pass",
		PlatformOpenRouter:  "openrouter",
		PlatformCommandCode: "",
		"COMPOSITE":         "",
		"":                  "",
	}
	for platform, expected := range cases {
		require.Equal(t, expected, modelCapabilityProviderForPlatform(platform), platform)
	}
}

// commandCodeCatalogForTest 构造只含上下文窗口的 Command Code 官方目录。
func commandCodeCatalogForTest(models map[string]int) *CommandCodeCatalog {
	entries := make(map[string]commandCodeCatalogEntry, len(models))
	for name, contextWindow := range models {
		entries[commandCodeCanonicalModelID(name)] = commandCodeCatalogEntry{
			ID:            name,
			ContextWindow: contextWindow,
		}
	}
	return &CommandCodeCatalog{entries: entries}
}

func TestSplitCommandCodeModelID(t *testing.T) {
	cases := []struct {
		model     string
		namespace string
		base      string
	}{
		{"deepseek/deepseek-v4-pro", "deepseek", "deepseek-v4-pro"},
		{"Qwen/Qwen3.7-Max", "qwen", "qwen3.7-max"},
		{"gpt-5.6-luna", "", "gpt-5.6-luna"},
		{"meituan/LongCat-2.0:free", "meituan", "longcat-2.0"},
		{"thinkingmachines/Inkling", "thinkingmachines", "inkling"},
		{"  z-ai/glm-5.3-flash  ", "z-ai", "glm-5.3-flash"},
		{"", "", ""},
	}
	for _, tc := range cases {
		namespace, base := splitCommandCodeModelID(tc.model)
		require.Equal(t, tc.namespace, namespace, tc.model)
		require.Equal(t, tc.base, base, tc.model)
	}
}

func TestIsOpenAICompatibleBareModelID(t *testing.T) {
	for _, model := range []string{"gpt-5.6-sol", "o3", "o4-mini"} {
		require.True(t, isOpenAICompatibleBareModelID(model), model)
	}
	for _, model := range []string{"kimi-k3", "glm-5.2", "grok-4.6", "opus"} {
		require.False(t, isOpenAICompatibleBareModelID(model), model)
	}
}

func TestContextsConsistent(t *testing.T) {
	require.True(t, contextsConsistent(1000000, 1048576), "取整差异应视为同源")
	require.True(t, contextsConsistent(256000, 262144))
	require.True(t, contextsConsistent(200000, 200000))
	require.False(t, contextsConsistent(200000, 1000000), "5 倍差异不得视为同源")
	require.False(t, contextsConsistent(256000, 65536))
	require.False(t, contextsConsistent(0, 1000000), "缺失值不能证明同源")
	require.False(t, contextsConsistent(1000000, 0))
}

func TestCommandCodeModelCapability_BorrowsVendorCapabilityWhenContextMatches(t *testing.T) {
	svc := &PricingService{
		modelCapabilities: newModelCapabilityCatalog(&capabilityRemote{body: []byte(capabilityCatalogJSON)}, nil),
		commandCodeCatalog: commandCodeCatalogForTest(map[string]int{
			"deepseek/deepseek-v4-pro": 1000000,
		}),
	}
	ctx := context.Background()

	capability, ok := svc.GetModelCapability(ctx, PlatformCommandCode, "deepseek/deepseek-v4-pro")
	require.True(t, ok)
	require.True(t, capability.Reasoning)
	require.True(t, capability.ToolCall)
	require.Equal(t, 1000000, capability.ContextTokens)
	require.Equal(t, 384000, capability.MaxOutputTokens)
}

func TestCommandCodeModelCapability_DropsLimitsWhenContextDiffers(t *testing.T) {
	svc := &PricingService{
		modelCapabilities: newModelCapabilityCatalog(&capabilityRemote{body: []byte(capabilityCatalogJSON)}, nil),
		commandCodeCatalog: commandCodeCatalogForTest(map[string]int{
			// Command Code 官方窗口远小于厂商目录：说明是另一种部署或限额口径。
			"deepseek/deepseek-v4-pro": 200000,
		}),
	}
	ctx := context.Background()

	capability, ok := svc.GetModelCapability(ctx, PlatformCommandCode, "deepseek/deepseek-v4-pro")
	require.True(t, ok, "同名模型仍应保留固有能力")
	require.True(t, capability.Reasoning)
	require.True(t, capability.ToolCall)
	require.Zero(t, capability.ContextTokens, "不同源时不得借用上下文窗口")
	require.Zero(t, capability.MaxOutputTokens, "不同源时不得借用输出上限")
}

func TestCommandCodeModelCapability_UsesVariantMatchingOfficialContext(t *testing.T) {
	svc := &PricingService{
		modelCapabilities: newModelCapabilityCatalog(&capabilityRemote{body: []byte(capabilityCatalogJSON)}, nil),
		commandCodeCatalog: commandCodeCatalogForTest(map[string]int{
			// 厂商目录里 262144 的 peft 变体才是实际部署，应与它对齐而不是基座的 65536。
			"thinkingmachines/inkling": 256000,
		}),
	}
	ctx := context.Background()

	capability, ok := svc.GetModelCapability(ctx, PlatformCommandCode, "thinkingmachines/inkling")
	require.True(t, ok)
	require.Equal(t, 262144, capability.ContextTokens)
	require.Equal(t, 65536, capability.MaxOutputTokens)
}

func TestCommandCodeModelCapability_MatchesNestedVendorKey(t *testing.T) {
	svc := &PricingService{
		modelCapabilities: newModelCapabilityCatalog(&capabilityRemote{body: []byte(capabilityCatalogJSON)}, nil),
		commandCodeCatalog: commandCodeCatalogForTest(map[string]int{
			"nvidia/nemotron-3-ultra-550b-a55b": 1000000,
		}),
	}
	capability, ok := svc.GetModelCapability(context.Background(), PlatformCommandCode, "nvidia/nemotron-3-ultra-550b-a55b")
	require.True(t, ok, "厂商目录把命名空间写进 key 时也要能命中")
	require.True(t, capability.Reasoning)
	require.Equal(t, 1000000, capability.ContextTokens)
}

func TestCommandCodeModelCapability_ModelAliasIsNormalized(t *testing.T) {
	svc := &PricingService{
		modelCapabilities: newModelCapabilityCatalog(&capabilityRemote{body: []byte(capabilityCatalogJSON)}, nil),
		commandCodeCatalog: commandCodeCatalogForTest(map[string]int{
			"deepseek/deepseek-v4-flash-fast": 1000000,
		}),
	}
	capability, ok := svc.GetModelCapability(context.Background(), PlatformCommandCode, "deepseek/deepseek-v4-flash-fast")
	require.True(t, ok, "已核实的变体名应归一到厂商模型")
	require.True(t, capability.Reasoning)
	require.Equal(t, 384000, capability.MaxOutputTokens)
}

func TestCommandCodeModelCapability_UnknownVendorModelIsNotGuessed(t *testing.T) {
	svc := &PricingService{
		modelCapabilities: newModelCapabilityCatalog(&capabilityRemote{body: []byte(capabilityCatalogJSON)}, nil),
		commandCodeCatalog: commandCodeCatalogForTest(map[string]int{
			"Qwen/Qwen3.8-27B":               1000000,
			"inclusionai/ling-3.0-flash":     1000000,
			"thinkingmachines/inkling-small": 1000000,
		}),
	}
	ctx := context.Background()

	for _, model := range []string{
		"Qwen/Qwen3.8-27B",                // 厂商目录未收录
		"inclusionai/ling-3.0-flash:free", // 无厂商命名空间映射
		"thinkingmachines/inkling-small",  // 厂商只有基座，没有 small 变体
		"some-random-model",               // 裸名且非 OpenAI 命名习惯
	} {
		_, ok := svc.GetModelCapability(ctx, PlatformCommandCode, model)
		require.False(t, ok, model)
	}
}

func TestCommandCodeCapabilityProviderIDs_StableAndUnique(t *testing.T) {
	ids := commandCodeCapabilityProviderIDs()
	require.NotEmpty(t, ids)

	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		require.NotEmpty(t, id)
		_, duplicated := seen[id]
		require.False(t, duplicated, "provider 列表不得重复: %s", id)
		seen[id] = struct{}{}
	}
	require.Equal(t, ids, commandCodeCapabilityProviderIDs(), "provider 列表顺序必须稳定")

	// 第一方 provider 必须在列表内，否则永远借不到能力。
	for _, required := range []string{"deepseek", "xai", "meta", "alibaba", "moonshotai", "zai", "minimax", "google", "stepfun", "openai"} {
		require.Contains(t, seen, required)
	}
}

// canonicalCatalogJSON 模拟 models.dev 的模型级目录（models.json）。
const canonicalCatalogJSON = `{
  "alibaba/qwen3.8-27b": {
    "reasoning": true,
    "tool_call": true,
    "modalities": {"input": ["text", "image", "video"], "output": ["text"]},
    "limit": {"context": 262144, "output": 32768}
  },
  "alibaba/qwen3.8-max-0902": {
    "reasoning": true,
    "tool_call": true,
    "modalities": {"input": ["text", "image", "video", "pdf"], "output": ["text"]},
    "limit": {"context": 1000000, "output": 131072}
  },
  "deepseek/deepseek-v4.1-flash": {
    "reasoning": true,
    "tool_call": true,
    "modalities": {"input": ["text", "image"], "output": ["text"]},
    "limit": {"context": 1000000, "output": 384000}
  },
  "poolside/laguna-s-2.1": {
    "reasoning": true,
    "tool_call": true,
    "modalities": {"input": ["text"], "output": ["text"]},
    "limit": {"context": 1048576, "output": 131072}
  }
}`

func TestParseModelCapabilityCanonicalCatalog(t *testing.T) {
	parsed := parseModelCapabilityCanonicalCatalog([]byte(canonicalCatalogJSON))
	require.Len(t, parsed, 3)

	alibaba := parsed["alibaba"]
	require.NotNil(t, alibaba)
	capability := alibaba["qwen3.8-27b"]
	require.Equal(t, 262_144, capability.ContextTokens)
	require.Equal(t, 32_768, capability.MaxOutputTokens)
	require.True(t, capability.Reasoning)
	require.True(t, capability.Vision)

	// 非法 JSON 与空目录都返回 nil，由调用方降级到 provider 级目录。
	require.Nil(t, parseModelCapabilityCanonicalCatalog([]byte("not json")))
	require.Nil(t, parseModelCapabilityCanonicalCatalog([]byte(`{}`)))
}

func TestNormalizeModelCapabilityKey(t *testing.T) {
	cases := map[string]string{
		"Qwen3.8-27B":      "qwen3.8-27b",
		"Qwen/Qwen3.8 27B": "qwen/qwen3.8-27b",
		"  GLM_5  ":        "glm-5",
		"MiniMax-M2.5":     "minimax-m2.5",
	}
	for input, expected := range cases {
		require.Equal(t, expected, normalizeModelCapabilityKey(input), input)
	}
}

func TestCommandCodeModelCapability_PrefersCanonicalCatalog(t *testing.T) {
	remote := &capabilityRemote{
		body:      []byte(capabilityCatalogJSON),
		canonical: []byte(canonicalCatalogJSON),
	}
	svc := &PricingService{
		modelCapabilities: newModelCapabilityCatalog(remote, nil),
		commandCodeCatalog: commandCodeCatalogForTest(map[string]int{
			// 模型级与 provider 级上下文都同源，模型级优先。
			"Qwen/Qwen3.8-27B":             262144,
			"Qwen/Qwen3.8-Max-0902":        1000000,
			"deepseek/deepseek-v4.1-flash": 1000000,
		}),
	}
	ctx := context.Background()

	// 这三个模型只在模型级目录里，provider 级目录没有。
	capability, ok := svc.GetModelCapability(ctx, PlatformCommandCode, "Qwen/Qwen3.8-27B")
	require.True(t, ok)
	require.Equal(t, 262_144, capability.ContextTokens)
	require.Equal(t, 32_768, capability.MaxOutputTokens)
	require.True(t, capability.Vision)

	capability, ok = svc.GetModelCapability(ctx, PlatformCommandCode, "Qwen/Qwen3.8-Max-0902")
	require.True(t, ok)
	require.True(t, capability.PDFInput)
	require.Equal(t, 131_072, capability.MaxOutputTokens)

	capability, ok = svc.GetModelCapability(ctx, PlatformCommandCode, "deepseek/deepseek-v4.1-flash")
	require.True(t, ok)
	require.Equal(t, 384_000, capability.MaxOutputTokens)
}

func TestCommandCodeModelCapability_CanonicalDropsLimitsWhenContextDiffers(t *testing.T) {
	remote := &capabilityRemote{canonical: []byte(canonicalCatalogJSON)}
	svc := &PricingService{
		modelCapabilities: newModelCapabilityCatalog(remote, nil),
		commandCodeCatalog: commandCodeCatalogForTest(map[string]int{
			// 模型级目录的 laguna-s-2.1 是 1M，Command Code 只提供 256K。
			"poolside/laguna-s-2.1-free": 262144,
		}),
	}

	capability, ok := svc.GetModelCapability(context.Background(), PlatformCommandCode, "poolside/laguna-s-2.1-free")
	require.True(t, ok)
	require.True(t, capability.Reasoning)
	require.Zero(t, capability.ContextTokens, "不同源时不得借用上下文窗口")
	require.Zero(t, capability.MaxOutputTokens, "不同源时不得借用输出上限")
}

func TestCommandCodeModelCapability_ProviderFallbackWhenCanonicalMisses(t *testing.T) {
	remote := &capabilityRemote{
		body:      []byte(capabilityCatalogJSON),
		canonical: []byte(canonicalCatalogJSON),
	}
	svc := &PricingService{
		modelCapabilities: newModelCapabilityCatalog(remote, nil),
		commandCodeCatalog: commandCodeCatalogForTest(map[string]int{
			// 贡献者变体只在 provider 级目录，模型级目录没有。
			"meta/muse-spark-1.2-contributor": 1_000_000,
		}),
	}

	capability, ok := svc.GetModelCapability(context.Background(), PlatformCommandCode, "meta/muse-spark-1.2-contributor")
	require.True(t, ok)
	require.True(t, capability.Reasoning)
	require.True(t, capability.Vision)
	// 上下文同源，因此数值限额一并借用（实际展示时仍以 Command Code 官方窗口为准）。
	require.Equal(t, 131_072, capability.MaxOutputTokens)
}

func TestCommandCodeModelCapability_CanonicalFailureDegradesToProviderCatalog(t *testing.T) {
	// 模型级目录不可用时，provider 级目录仍必须可用。
	remote := &capabilityRemote{body: []byte(capabilityCatalogJSON)}
	svc := &PricingService{
		modelCapabilities: newModelCapabilityCatalog(remote, nil),
		commandCodeCatalog: commandCodeCatalogForTest(map[string]int{
			"deepseek/deepseek-v4-pro": 1000000,
		}),
	}

	capability, ok := svc.GetModelCapability(context.Background(), PlatformCommandCode, "deepseek/deepseek-v4-pro")
	require.True(t, ok)
	require.True(t, capability.Reasoning)
	require.Equal(t, 384_000, capability.MaxOutputTokens)
	require.Equal(t, 1, remote.canonicalCalls, "模型级目录只尝试一次")
}
