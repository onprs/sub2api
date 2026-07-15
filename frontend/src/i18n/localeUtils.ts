export function flattenLocaleKeys(messages: Record<string, unknown>, prefix = ''): string[] {
  const keys: string[] = []

  for (const [name, value] of Object.entries(messages)) {
    const key = prefix ? `${prefix}.${name}` : name
    if (value !== null && typeof value === 'object' && !Array.isArray(value)) {
      keys.push(...flattenLocaleKeys(value as Record<string, unknown>, key))
    } else {
      keys.push(key)
    }
  }

  return keys.sort()
}
