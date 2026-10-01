import { describe, expect, it } from 'vitest'
import { validateOpenAILoginCredentials } from '../openaiAutoReauth'

const prefix = 'person@example.com----password----'

describe('OpenAI upstream login credential validation', () => {
  it.each([
    'person@example.com|password|JBSWY3DPEHPK3PXP',
    'person@example.com|password|otpauth://totp/OpenAI?secret=JBSWY3DPEHPK3PXP',
    'person@example.com----pass|word----JBSWY3DPEHPK3PXP'
  ])('accepts pipes as fallback and prefers four-hyphen separators', (raw) => {
    expect(validateOpenAILoginCredentials(raw)).toBeNull()
  })

  it('accepts exactly 8192 bytes and rejects a longer credential line', () => {
    const emailPrefix = 'person@example.com----'
    const secretSuffix = '----JBSWY3DPEHPK3PXP'
    const raw = emailPrefix + 'x'.repeat(8192 - emailPrefix.length - secretSuffix.length) + secretSuffix
    expect(raw.length).toBe(8192)
    expect(validateOpenAILoginCredentials(raw)).toBeNull()
    expect(validateOpenAILoginCredentials(raw.replace('----x', '----xx'))).toBe('admin.accounts.openai.autoReauth.invalidFormat')
  })

  it('enforces the UTF-8 byte limit for multibyte passwords', () => {
    const raw = 'person@example.com----' + '密'.repeat(2800) + '----JBSWY3DPEHPK3PXP'
    expect(raw.length).toBeLessThan(8192)
    expect(validateOpenAILoginCredentials(raw)).toBe('admin.accounts.openai.autoReauth.invalidFormat')
  })

  it.each([
    'JBSWY3DPEHPK3PXP',
    'jbsw y3dp ehpk 3pxp',
    'otpauth://totp/OpenAI:person?secret=JBSWY3DPEHPK3PXP&issuer=OpenAI'
  ])('accepts a persistent secret: %s', (secret) => {
    expect(validateOpenAILoginCredentials(prefix + secret)).toBeNull()
  })

  it.each(['123456', '', 'otpauth://totp/OpenAI?secret=123456', 'otpauth://totp/OpenAI', 'otpauth://hotp/OpenAI?secret=JBSWY3DPEHPK3PXP', 'not-a-secret'])('rejects an invalid or temporary secret: %s', (secret) => {
    expect(validateOpenAILoginCredentials(prefix + secret)).toBe('admin.accounts.openai.autoReauth.invalidSecret')
  })

  it.each(['person----password----JBSWY3DPEHPK3PXP', 'person@example.com---- ----JBSWY3DPEHPK3PXP', prefix + 'JBSWY3DPEHPK3PXP\n' + prefix + 'JBSWY3DPEHPK3PXP'])('rejects incomplete or multiline credentials', (raw) => {
    expect(validateOpenAILoginCredentials(raw)).toBe('admin.accounts.openai.autoReauth.invalidFormat')
  })
})
