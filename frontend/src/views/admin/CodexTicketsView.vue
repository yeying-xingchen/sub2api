<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-7xl space-y-6 p-4 sm:p-6">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">
            {{ t('admin.codexTickets.title') }}
          </h1>
          <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-dark-400">
            {{ t('admin.codexTickets.description') }}
          </p>
          <p class="mt-2 text-xs text-gray-400 dark:text-dark-500">
            {{
              overview?.last_round_at
                ? t('admin.codexTickets.lastRound', { time: formatDateTime(overview.last_round_at) })
                : t('admin.codexTickets.never')
            }}
          </p>
        </div>
        <div class="flex items-center gap-2">
          <button class="btn btn-secondary" :disabled="loading" @click="load">
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            <span class="ml-1">{{ t('admin.codexTickets.actions.refresh') }}</span>
          </button>
          <button class="btn btn-primary" :disabled="harvesting" @click="harvestAll">
            <Icon name="play" size="md" class="mr-1" />
            {{ harvesting ? t('admin.codexTickets.actions.harvesting') : t('admin.codexTickets.actions.harvestAll') }}
          </button>
        </div>
      </div>

      <section class="rounded-xl border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-800 sm:p-6">
        <div class="flex items-center justify-between gap-4">
          <h2 class="text-lg font-medium text-gray-900 dark:text-white">
            {{ t('admin.codexTickets.settings.title') }}
          </h2>
          <button class="btn btn-primary" :disabled="saving" @click="saveSettings">
            {{ saving ? t('admin.codexTickets.actions.saving') : t('admin.codexTickets.actions.save') }}
          </button>
        </div>

        <div class="mt-4 grid gap-5 lg:grid-cols-2">
          <label class="flex items-start gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-700">
            <input v-model="form.enabled" type="checkbox" class="mt-1 h-4 w-4" />
            <span>
              <span class="block text-sm font-medium text-gray-900 dark:text-white">
                {{ t('admin.codexTickets.settings.enabled') }}
              </span>
              <span class="mt-1 block text-xs text-gray-500 dark:text-dark-400">
                {{ t('admin.codexTickets.settings.enabledHint') }}
              </span>
            </span>
          </label>

          <label class="flex items-start gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-700">
            <input v-model="form.shared" type="checkbox" class="mt-1 h-4 w-4" />
            <span>
              <span class="block text-sm font-medium text-gray-900 dark:text-white">
                {{ t('admin.codexTickets.settings.shared') }}
              </span>
              <span class="mt-1 block text-xs text-gray-500 dark:text-dark-400">
                {{ t('admin.codexTickets.settings.sharedHint') }}
              </span>
            </span>
          </label>

          <label class="flex items-start gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-700">
            <input v-model="form.fail_closed" type="checkbox" class="mt-1 h-4 w-4" />
            <span>
              <span class="block text-sm font-medium text-gray-900 dark:text-white">
                {{ t('admin.codexTickets.settings.failClosed') }}
              </span>
              <span class="mt-1 block text-xs text-gray-500 dark:text-dark-400">
                {{ t('admin.codexTickets.settings.failClosedHint') }}
              </span>
            </span>
          </label>

          <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-700">
            <label class="block text-sm font-medium text-gray-900 dark:text-white">
              {{ t('admin.codexTickets.settings.models') }}
            </label>
            <input v-model="modelsText" class="input mt-2 w-full" placeholder="gpt-6-astra, gpt-5.6-sol" />
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
              {{ t('admin.codexTickets.settings.modelsHint') }}
            </p>
          </div>

          <div class="grid gap-4 sm:grid-cols-3 lg:col-span-2">
            <div>
              <label class="block text-sm font-medium text-gray-900 dark:text-white">
                {{ t('admin.codexTickets.settings.interval') }}
              </label>
              <input v-model.number="form.harvest_interval_seconds" type="number" min="30" max="3600" class="input mt-2 w-full" />
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                {{ t('admin.codexTickets.settings.intervalHint') }}
              </p>
            </div>
            <div>
              <label class="block text-sm font-medium text-gray-900 dark:text-white">
                {{ t('admin.codexTickets.settings.ttl') }}
              </label>
              <input v-model.number="form.ttl_seconds" type="number" min="60" max="86400" class="input mt-2 w-full" />
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                {{ t('admin.codexTickets.settings.ttlHint') }}
              </p>
            </div>
            <div>
              <label class="block text-sm font-medium text-gray-900 dark:text-white">
                {{ t('admin.codexTickets.settings.probeTimeout') }}
              </label>
              <input v-model.number="form.probe_timeout_seconds" type="number" min="5" max="120" class="input mt-2 w-full" />
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                {{ t('admin.codexTickets.settings.probeTimeoutHint') }}
              </p>
            </div>
          </div>
        </div>
      </section>

      <section class="rounded-xl border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-800 sm:p-6">
        <h2 class="text-lg font-medium text-gray-900 dark:text-white">
          {{ t('admin.codexTickets.accounts.title') }}
        </h2>
        <p v-if="!overview || overview.accounts.length === 0" class="mt-3 text-sm text-gray-500 dark:text-dark-400">
          {{ t('admin.codexTickets.accounts.empty') }}
        </p>
        <div v-else class="mt-4 overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
            <thead>
              <tr class="text-left text-xs uppercase tracking-wide text-gray-500 dark:text-dark-400">
                <th class="px-3 py-2">{{ t('admin.codexTickets.accounts.account') }}</th>
                <th class="px-3 py-2">{{ t('admin.codexTickets.accounts.tickets') }}</th>
                <th class="px-3 py-2 text-right">{{ t('admin.codexTickets.accounts.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="account in overview.accounts" :key="account.id">
                <td class="px-3 py-3">
                  <div class="font-medium text-gray-900 dark:text-white">#{{ account.id }} {{ account.name }}</div>
                  <div class="text-xs text-gray-500 dark:text-dark-400">{{ account.status }}</div>
                </td>
                <td class="px-3 py-3">
                  <div class="flex flex-wrap gap-2">
                    <span
                      v-for="ticket in account.tickets"
                      :key="ticket.model"
                      class="inline-flex items-center gap-1 rounded-full px-2 py-1 text-xs"
                      :class="ticketClass(ticket)"
                    >
                      <span class="font-medium">{{ ticket.model }}</span>
                      <span>·</span>
                      <span v-if="ticket.own">{{ t('admin.codexTickets.status.own') }}</span>
                      <span v-else-if="ticket.shared">
                        {{ t('admin.codexTickets.status.shared') }}
                        <template v-if="ticket.source_account_id">
                          {{ t('admin.codexTickets.status.source', { id: ticket.source_account_id }) }}
                        </template>
                      </span>
                      <span v-else>{{ t('admin.codexTickets.status.missing') }}</span>
                      <span v-if="ticket.ready">
                        · {{ t('admin.codexTickets.status.expiresIn', { time: formatRemaining(ticket.remaining_seconds) }) }}
                      </span>
                    </span>
                  </div>
                  <div v-if="account.iq" class="mt-2 flex flex-wrap items-center gap-2 text-xs">
                    <span class="rounded-full px-2 py-0.5 font-medium" :class="iqClass(account.iq)">
                      {{ t('admin.codexTickets.iq.weight') }} · {{ iqLabel(account.iq.verdict) }}
                    </span>
                    <span class="text-gray-500 dark:text-dark-400">
                      {{ t('admin.codexTickets.iq.latency', { ms: account.iq.latency_ms }) }}
                    </span>
                    <span class="text-gray-500 dark:text-dark-400">
                      {{ t('admin.codexTickets.iq.ticket', { length: account.iq.ticket_length }) }}
                    </span>
                    <span v-if="account.iq.short_ticket" class="text-amber-600 dark:text-amber-400">
                      {{ t('admin.codexTickets.iq.shortTicket') }}
                    </span>
                  </div>
                  <p v-if="account.iq" class="mt-1 max-w-xl text-xs text-gray-500 dark:text-dark-400">
                    {{ account.iq.reason }}
                  </p>
                </td>
                <td class="px-3 py-3 text-right">
                  <div class="flex flex-col items-end gap-2">
                    <button
                      class="btn btn-secondary"
                      :disabled="busyAccountId === account.id"
                      @click="harvestOne(account.id)"
                    >
                      {{
                        busyAccountId === account.id
                          ? t('admin.codexTickets.actions.harvesting')
                          : t('admin.codexTickets.actions.harvest')
                      }}
                    </button>
                    <button
                      class="btn btn-secondary"
                      :disabled="testingAccountId === account.id"
                      @click="testIQ(account)"
                    >
                      {{
                        testingAccountId === account.id
                          ? t('admin.codexTickets.actions.iqTesting')
                          : t('admin.codexTickets.actions.iqTest')
                      }}
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="rounded-xl border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-800 sm:p-6">
        <h2 class="text-lg font-medium text-gray-900 dark:text-white">
          {{ t('admin.codexTickets.pool.title') }}
        </h2>
        <p v-if="!overview || overview.pool.length === 0" class="mt-3 text-sm text-gray-500 dark:text-dark-400">
          {{ t('admin.codexTickets.pool.empty') }}
        </p>
        <div v-else class="mt-4 overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
            <thead>
              <tr class="text-left text-xs uppercase tracking-wide text-gray-500 dark:text-dark-400">
                <th class="px-3 py-2">{{ t('admin.codexTickets.pool.model') }}</th>
                <th class="px-3 py-2">{{ t('admin.codexTickets.pool.source') }}</th>
                <th class="px-3 py-2">{{ t('admin.codexTickets.pool.remaining') }}</th>
                <th class="px-3 py-2">{{ t('admin.codexTickets.pool.length') }}</th>
                <th class="px-3 py-2">{{ t('admin.codexTickets.pool.captured') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="entry in overview.pool" :key="`${entry.model}-${entry.source_account_id}`">
                <td class="px-3 py-2 font-medium text-gray-900 dark:text-white">{{ entry.model }}</td>
                <td class="px-3 py-2 text-gray-600 dark:text-gray-300">
                  #{{ entry.source_account_id }} {{ entry.source_account_name || '' }}
                </td>
                <td class="px-3 py-2 text-gray-600 dark:text-gray-300">{{ formatRemaining(entry.remaining_seconds) }}</td>
                <td class="px-3 py-2 text-gray-600 dark:text-gray-300">{{ entry.length }}</td>
                <td class="px-3 py-2 text-gray-500 dark:text-dark-400">{{ formatDateTime(entry.captured_at) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'
import {
  codexTicketsAPI,
  type CodexTicketAccountStatus,
  type CodexIQTestResult,
  type CodexTicketModelStatus,
  type CodexTicketOverview,
  type CodexTicketSettings
} from '@/api/admin'

const { t } = useI18n()
const appStore = useAppStore()

const overview = ref<CodexTicketOverview | null>(null)
const loading = ref(false)
const saving = ref(false)
const harvesting = ref(false)
const busyAccountId = ref<number | null>(null)
const testingAccountId = ref<number | null>(null)
const modelsText = ref('')

const form = reactive<CodexTicketSettings>({
  enabled: false,
  shared: true,
  fail_closed: false,
  models: [],
  harvest_interval_seconds: 180,
  ttl_seconds: 3600,
  probe_timeout_seconds: 25
})

function applySettings(settings: CodexTicketSettings) {
  Object.assign(form, settings)
  modelsText.value = (settings.models || []).join(', ')
}

function parseModels(): string[] {
  return modelsText.value
    .split(/[\s,，;；]+/)
    .map((item) => item.trim())
    .filter(Boolean)
}

function ticketClass(ticket: CodexTicketModelStatus) {
  if (!ticket.ready) {
    return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-300'
  }
  return ticket.own
    ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    : 'bg-sky-50 text-sky-700 dark:bg-sky-900/30 dark:text-sky-300'
}

function formatRemaining(seconds: number): string {
  if (!seconds || seconds <= 0) return '0m'
  if (seconds >= 3600) {
    const hours = Math.floor(seconds / 3600)
    const minutes = Math.floor((seconds % 3600) / 60)
    return `${hours}h ${minutes}m`
  }
  return `${Math.max(1, Math.floor(seconds / 60))}m`
}

function errorMessage(error: unknown): string {
  if (error && typeof error === 'object' && 'message' in error) {
    return String((error as { message?: unknown }).message || '')
  }
  return String(error)
}

async function load() {
  loading.value = true
  try {
    overview.value = await codexTicketsAPI.getOverview()
    applySettings(overview.value.settings)
  } catch (error) {
    appStore.showError(t('admin.codexTickets.messages.loadFailed', { error: errorMessage(error) }))
  } finally {
    loading.value = false
  }
}

async function saveSettings() {
  saving.value = true
  try {
    const models = parseModels()
    const saved = await codexTicketsAPI.updateSettings({
      ...form,
      models
    })
    applySettings(saved)
    appStore.showSuccess(t('admin.codexTickets.messages.saved'))
    await load()
  } catch (error) {
    appStore.showError(t('admin.codexTickets.messages.failed', { error: errorMessage(error) }))
  } finally {
    saving.value = false
  }
}

function reportHarvest(summary: { succeeded: number; failed: number; skipped: number }) {
  appStore.showSuccess(
    t('admin.codexTickets.messages.harvested', {
      ok: summary.succeeded,
      failed: summary.failed,
      skipped: summary.skipped
    })
  )
}

async function harvestAll() {
  harvesting.value = true
  try {
    const summary = await codexTicketsAPI.harvest({})
    reportHarvest(summary)
    await load()
  } catch (error) {
    appStore.showError(t('admin.codexTickets.messages.failed', { error: errorMessage(error) }))
  } finally {
    harvesting.value = false
  }
}

function iqLabel(verdict: CodexIQTestResult['verdict']): string {
  if (verdict === 'healthy') return t('admin.codexTickets.status.iqHealthy')
  if (verdict === 'degraded') return t('admin.codexTickets.status.iqDegraded')
  return t('admin.codexTickets.status.iqInconclusive')
}

function iqClass(result: CodexIQTestResult) {
  if (result.verdict === 'healthy') {
    return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
  }
  if (result.verdict === 'degraded') {
    return 'bg-rose-50 text-rose-700 dark:bg-rose-900/30 dark:text-rose-300'
  }
  return 'bg-amber-50 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
}

async function testIQ(account: CodexTicketAccountStatus) {
  testingAccountId.value = account.id
  try {
    const result = await codexTicketsAPI.runIQTest(account.id)
    appStore.showSuccess(
      t('admin.codexTickets.messages.iqDone', {
        verdict: iqLabel(result.verdict),
        ms: result.latency_ms
      })
    )
    await load()
  } catch (error) {
    appStore.showError(t('admin.codexTickets.messages.failed', { error: errorMessage(error) }))
  } finally {
    testingAccountId.value = null
  }
}

async function harvestOne(accountId: number) {
  busyAccountId.value = accountId
  try {
    const summary = await codexTicketsAPI.harvestAccount(accountId, parseModels())
    reportHarvest(summary)
    await load()
  } catch (error) {
    appStore.showError(t('admin.codexTickets.messages.failed', { error: errorMessage(error) }))
  } finally {
    busyAccountId.value = null
  }
}

onMounted(load)
</script>
