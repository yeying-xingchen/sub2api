<template>
  <div class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-600" data-testid="openai-auto-reauth-settings">
    <div class="flex items-center justify-between gap-3">
      <div>
        <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.accounts.openai.autoReauth.title') }}</p>
        <p class="input-hint">{{ t('admin.accounts.openai.autoReauth.description') }}</p>
      </div>
      <Toggle
        :model-value="enabled"
        :disabled="disabled"
        :aria-label="t('admin.accounts.openai.autoReauth.title')"
        data-testid="openai-auto-reauth-toggle"
        @update:model-value="$emit('update:enabled', $event)"
      />
    </div>
    <label class="block">
      <span class="input-label">{{ t('admin.accounts.openai.autoReauth.credentialsLabel') }}</span>
      <input
        :value="loginCredentials"
        type="password"
        maxlength="8192"
        class="input"
        autocomplete="new-password"
        autocapitalize="off"
        :spellcheck="false"
        :disabled="disabled"
        :placeholder="t('admin.accounts.openai.autoReauth.placeholder')"
        data-testid="openai-login-credentials"
        @input="$emit('update:loginCredentials', ($event.target as HTMLInputElement).value)"
      />
    </label>
    <p class="input-hint">{{ t('admin.accounts.openai.autoReauth.secretHint') }}</p>
    <p v-if="configured" class="text-xs text-emerald-600 dark:text-emerald-400" data-testid="openai-login-credentials-configured">
      {{ t('admin.accounts.openai.autoReauth.configured') }}
    </p>
    <p class="input-hint">{{ t('admin.accounts.openai.autoReauth.retainHint') }}</p>
    <p v-if="creating" class="input-hint">{{ t('admin.accounts.openai.autoReauth.createHint') }}</p>
    <p v-if="status?.status" class="text-xs text-gray-600 dark:text-gray-400" role="status">
      {{ t(`admin.accounts.openai.autoReauth.status.${status.status}`) }}
      <span v-if="status.status === 'failed' && status.error">: {{ status.error }}</span>
    </p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import type { OpenAIAutoReauthStatus } from '@/types'

defineProps<{
  enabled: boolean
  loginCredentials: string
  configured?: boolean
  creating?: boolean
  disabled?: boolean
  status?: OpenAIAutoReauthStatus
}>()
defineEmits<{
  'update:enabled': [value: boolean]
  'update:loginCredentials': [value: string]
}>()
const { t } = useI18n()
</script>
