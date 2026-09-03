import type { AccountPlatform, GroupPlatform } from '@/types'

export interface PlatformOption<T extends string = string> {
  value: T
  label: string
}

const CORE_PLATFORM_OPTIONS = [
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'antigravity', label: 'Antigravity' },
  { value: 'grok', label: 'Grok' }
] as const satisfies readonly PlatformOption<AccountPlatform>[]

const CN_PLATFORM_OPTIONS = [
  { value: 'kimi', label: 'Kimi' },
  { value: 'zhipu', label: 'Zhipu GLM' },
  { value: 'deepseek', label: 'DeepSeek' }
] as const satisfies readonly PlatformOption<AccountPlatform>[]

/** Platforms supported as Composite model route targets. */
export const COMPOSITE_ROUTE_PLATFORM_OPTIONS = [
  ...CORE_PLATFORM_OPTIONS,
  ...CN_PLATFORM_OPTIONS
] as const satisfies readonly PlatformOption<AccountPlatform>[]

/** Concrete account platforms, including locally supported providers. */
export const CONCRETE_PLATFORM_OPTIONS = [
  ...CORE_PLATFORM_OPTIONS,
  { value: 'opencode_go', label: 'OpenCode Go' },
  { value: 'clinepass', label: 'ClinePass' },
  { value: 'openrouter', label: 'OpenRouter' },
  { value: 'commandcode', label: 'Command Code' },
  ...CN_PLATFORM_OPTIONS
] as const satisfies readonly PlatformOption<AccountPlatform>[]

/** Platforms that can own a group. */
export const GROUP_PLATFORM_OPTIONS = [
  ...CONCRETE_PLATFORM_OPTIONS,
  { value: 'composite', label: 'Composite' }
] as const satisfies readonly PlatformOption<GroupPlatform>[]
