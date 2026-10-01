import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Account } from '@/types'

const mocks = vi.hoisted(() => ({
  update: vi.fn(),
  apply: vi.fn(),
  generate: vi.fn(),
  exchange: vi.fn(),
  refresh: vi.fn(),
  showError: vi.fn()
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: mocks.showError, showSuccess: vi.fn() })
}))
vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      update: mocks.update,
      applyOAuthCredentials: mocks.apply,
      generateAuthUrl: mocks.generate,
      exchangeCode: mocks.exchange,
      refreshOpenAIToken: mocks.refresh
    }
  }
}))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

import ReAuthAccountModal from '../ReAuthAccountModal.vue'

const BaseDialog = defineComponent({
  props: ['show'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})
const OAuthFlow = defineComponent({
  name: 'OAuthAuthorizationFlow',
  data: () => ({ authCode: 'code', oauthState: 'state', projectId: '', inputMethod: 'manual' }),
  emits: ['generate-url', 'validate-refresh-token'],
  methods: { reset() {} },
  template: '<button data-testid="generate-url" @click="$emit(\'generate-url\')">Generate</button>'
})

const account = (credentials: Record<string, unknown> = {}): Account => ({
  id: 7,
  name: 'OpenAI OAuth',
  platform: 'openai',
  type: 'oauth',
  credentials,
  proxy_id: null,
  extra: {}
} as Account)

function mountModal(value = account()) {
  return mount(ReAuthAccountModal, {
    props: { show: true, account: value },
    global: { stubs: { BaseDialog, OAuthAuthorizationFlow: OAuthFlow, Icon: true } }
  })
}

const raw = 'person@example.com----password----JBSWY3DPEHPK3PXP'

describe('ReAuthAccountModal OpenAI automatic reauthorization', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.update.mockResolvedValue(account({ openai_auto_reauth_enabled: true, openai_login_credentials_configured: true }))
    mocks.apply.mockResolvedValue(account())
    mocks.generate.mockResolvedValue({ auth_url: 'https://auth.openai.com/authorize?state=state', session_id: 'session' })
    mocks.exchange.mockResolvedValue({ access_token: 'new-access', refresh_token: 'new-refresh' })
    mocks.refresh.mockResolvedValue({ access_token: 'refreshed-access' })
  })

  it('saves only login settings without exchanging or replacing OAuth tokens', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="openai-login-credentials"]').setValue(raw)
    await wrapper.get('[data-testid="openai-auto-reauth-toggle"]').trigger('click')
    await wrapper.get('[data-testid="save-openai-auto-reauth"]').trigger('click')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(7, { credentials: {
      openai_auto_reauth_enabled: true,
      openai_login_credentials: raw
    } })
    expect(mocks.exchange).not.toHaveBeenCalled()
    expect(mocks.apply).not.toHaveBeenCalled()
    expect(wrapper.emitted('reauthorized')).toHaveLength(1)
    wrapper.unmount()
  })

  it('disables automatic reauthorization without deleting the saved login secret', async () => {
    const wrapper = mountModal(account({ openai_auto_reauth_enabled: true, openai_login_credentials_configured: true }))
    expect(wrapper.get<HTMLInputElement>('[data-testid="openai-login-credentials"]').element.value).toBe('')
    await wrapper.get('[data-testid="openai-auto-reauth-toggle"]').trigger('click')
    await wrapper.get('[data-testid="save-openai-auto-reauth"]').trigger('click')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(7, { credentials: { openai_auto_reauth_enabled: false } })
    wrapper.unmount()
  })

  it('keeps the manual code exchange and adds new settings to its OAuth credentials', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="openai-login-credentials"]').setValue(raw)
    await wrapper.get('[data-testid="openai-auto-reauth-toggle"]').trigger('click')
    await wrapper.get('[data-testid="generate-url"]').trigger('click')
    await flushPromises()
    const complete = wrapper.findAll('button').find((button) => button.text() === 'admin.accounts.oauth.completeAuth')!
    await complete.trigger('click')
    await flushPromises()
    expect(mocks.exchange).toHaveBeenCalled()
    expect(mocks.apply).toHaveBeenCalledWith(7, expect.objectContaining({ type: 'oauth', credentials: expect.objectContaining({
      access_token: 'new-access', refresh_token: 'new-refresh',
      openai_auto_reauth_enabled: true, openai_login_credentials: raw
    }) }))
    wrapper.unmount()
  })

  it('retains saved login credentials during a manual refresh-token reauthorization', async () => {
    const wrapper = mountModal(account({ openai_auto_reauth_enabled: true, openai_login_credentials_configured: true }))
    wrapper.getComponent(OAuthFlow).vm.$emit('validate-refresh-token', 'existing-refresh')
    await flushPromises()
    expect(mocks.apply).toHaveBeenCalledWith(7, expect.objectContaining({ credentials: expect.objectContaining({
      access_token: 'refreshed-access', openai_auto_reauth_enabled: true
    }) }))
    expect(mocks.apply.mock.calls[0][1].credentials).not.toHaveProperty('openai_login_credentials')
    wrapper.unmount()
  })

  it('rejects one-time codes without submitting', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="openai-login-credentials"]').setValue('person@example.com----password----123456')
    await wrapper.get('[data-testid="save-openai-auto-reauth"]').trigger('click')
    expect(mocks.update).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenCalledWith('admin.accounts.openai.autoReauth.invalidSecret')
    wrapper.unmount()
  })

  it.each(['anthropic', 'grok', 'gemini', 'antigravity'] as const)('hides OpenAI settings for %s', (platform) => {
    const wrapper = mountModal({ ...account(), platform })
    expect(wrapper.find('[data-testid="openai-auto-reauth-settings"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="save-openai-auto-reauth"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows the backend automatic reauthorization failure status', () => {
    const wrapper = mountModal({ ...account(), extra: { openai_auto_reauth: { status: 'failed', error: 'Reauthorization failed' } } })
    expect(wrapper.get('[role="status"]').text()).toContain('Reauthorization failed')
    wrapper.unmount()
  })
})
