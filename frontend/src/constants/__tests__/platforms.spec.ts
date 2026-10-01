import { describe, expect, it } from 'vitest'
import { ACCOUNT_TYPE_OPTIONS, CONCRETE_PLATFORM_OPTIONS, GROUP_PLATFORM_OPTIONS } from '@/constants/platforms'

const concretePlatforms = [
  'anthropic',
  'openai',
  'gemini',
  'antigravity',
  'grok',
  'kimi',
  'zhipu',
  'deepseek',
  'minimax',
  'opencode_go'
]

describe('platform option catalogs', () => {
  it('exposes every concrete account platform', () => {
    expect(CONCRETE_PLATFORM_OPTIONS.map((option) => option.value)).toEqual(concretePlatforms)
  })

  it('exposes every account type supported by the account model', () => {
    expect(ACCOUNT_TYPE_OPTIONS.map((option) => option.value)).toEqual([
      'oauth',
      'setup-token',
      'apikey',
      'upstream',
      'bedrock',
      'service_account'
    ])
  })
  it('adds composite for group-backed filters', () => {
    expect(GROUP_PLATFORM_OPTIONS.map((option) => option.value)).toEqual([
      ...concretePlatforms,
      'composite'
    ])
  })
})
