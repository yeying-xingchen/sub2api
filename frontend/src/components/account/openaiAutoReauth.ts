import { reactive } from 'vue'
import type { OpenAIAutoReauthCredentialInput } from '@/types'

export interface OpenAIAutoReauthFormState {
  enabled: boolean
  loginCredentials: string
  configured: boolean
}

const messagePrefix = 'admin.accounts.openai.autoReauth.'
export const OPENAI_LOGIN_CREDENTIALS_MAX_LENGTH = 8192

/** Validate the long-lived upstream TOTP secret, never a one-time code. */
export function validateOpenAILoginCredentials(value: string): string | null {
  if (value.length > OPENAI_LOGIN_CREDENTIALS_MAX_LENGTH || new TextEncoder().encode(value).length > OPENAI_LOGIN_CREDENTIALS_MAX_LENGTH || /[\r\n]/.test(value)) {
    return `${messagePrefix}invalidFormat`
  }
  const parts = value.split(value.includes('----') ? '----' : '|')
  if (parts.length !== 3 || !/^[^\s@]+@[^\s@]+$/.test(parts[0].trim()) || !parts[1].trim()) {
    return `${messagePrefix}invalidFormat`
  }
  let secret = parts[2].trim()
  if (/^otpauth:\/\//i.test(secret)) {
    try {
      const uri = new URL(secret)
      if (uri.protocol !== 'otpauth:' || uri.hostname !== 'totp') return `${messagePrefix}invalidSecret`
      secret = uri.searchParams.get('secret') || ''
    } catch {
      return `${messagePrefix}invalidSecret`
    }
  }
  const normalized = secret.replace(/\s/g, '').replace(/=+$/, '')
  if (normalized.length < 16 || !/^[a-z2-7]+$/i.test(normalized)) return `${messagePrefix}invalidSecret`
  return null
}

export function useOpenAIAutoReauth() {
  const state = reactive<OpenAIAutoReauthFormState>({ enabled: false, loginCredentials: '', configured: false })
  const reset = (credentials?: Record<string, unknown>) => {
    state.enabled = credentials?.openai_auto_reauth_enabled === true
    state.configured = credentials?.openai_login_credentials_configured === true
    // Write-only: never initialize plaintext from an API response.
    state.loginCredentials = ''
  }
  const validationError = (): string | null => {
    if (state.loginCredentials.trim()) return validateOpenAILoginCredentials(state.loginCredentials)
    if (state.enabled && !state.configured) return `${messagePrefix}required`
    return null
  }
  const apply = (credentials: Record<string, unknown>): void => {
    delete credentials.openai_login_credentials
    delete credentials.openai_login_credentials_configured
    delete credentials.openai_login_credentials_encrypted
    const patch: OpenAIAutoReauthCredentialInput = { openai_auto_reauth_enabled: state.enabled }
    // Blank retains the saved secret, including when turning the switch off.
    if (state.loginCredentials.trim()) patch.openai_login_credentials = state.loginCredentials
    Object.assign(credentials, patch)
  }
  return { state, reset, validationError, apply }
}
