<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-7xl space-y-6 p-4 sm:p-6">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">
            {{ t('admin.pelicanTests.title') }}
          </h1>
          <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-dark-400">
            {{ t('admin.pelicanTests.description') }}
          </p>
        </div>
        <button class="btn btn-secondary" :disabled="loading" @click="loadHistory">
          <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          <span class="ml-1">{{ t('common.refresh') }}</span>
        </button>
      </div>

      <section class="rounded-xl border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-800">
        <div class="flex flex-wrap items-center justify-between gap-3 px-4 py-3 sm:px-6">
          <div>
            <h2 class="text-lg font-medium text-gray-900 dark:text-white">
              {{ t('admin.pelicanTests.plans.title') }}
            </h2>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
              {{ t('admin.pelicanTests.plans.empty') }}
            </p>
          </div>
          <button class="btn btn-primary" @click="openCreatePlan">
            <Icon name="plus" size="md" class="mr-1" />
            {{ t('admin.pelicanTests.plans.create') }}
          </button>
        </div>
        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
            <thead>
              <tr class="text-left text-xs uppercase tracking-wide text-gray-500 dark:text-dark-400">
                <th class="px-4 py-3">{{ t('admin.pelicanTests.plans.account') }}</th>
                <th class="px-4 py-3">{{ t('admin.pelicanTests.plans.model') }}</th>
                <th class="px-4 py-3">{{ t('admin.pelicanTests.plans.cron') }}</th>
                <th class="px-4 py-3">{{ t('admin.pelicanTests.plans.nextRun') }}</th>
                <th class="px-4 py-3">{{ t('admin.pelicanTests.plans.lastRun') }}</th>
                <th class="px-4 py-3">{{ t('admin.pelicanTests.plans.lastStatus') }}</th>
                <th class="px-4 py-3 text-right">{{ t('admin.pelicanTests.plans.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-if="plans.length === 0">
                <td colspan="7" class="px-4 py-8 text-center text-gray-500 dark:text-dark-400">
                  {{ t('admin.pelicanTests.plans.empty') }}
                </td>
              </tr>
              <tr v-for="plan in plans" :key="plan.id" class="hover:bg-gray-50 dark:hover:bg-dark-700/40">
                <td class="px-4 py-3 text-gray-700 dark:text-gray-200">
                  #{{ plan.account_id }} {{ plan.account_name }}
                  <span
                    class="ml-2 rounded-full px-2 py-0.5 text-[11px]"
                    :class="plan.enabled
                      ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                      : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-300'"
                  >
                    {{ plan.enabled ? t('admin.pelicanTests.plans.enabled') : t('admin.pelicanTests.plans.paused') }}
                  </span>
                </td>
                <td class="px-4 py-3 text-gray-600 dark:text-gray-300">{{ plan.model || '-' }}</td>
                <td class="px-4 py-3 font-mono text-xs text-gray-600 dark:text-gray-300">{{ plan.cron_expression }}</td>
                <td class="px-4 py-3 text-gray-500 dark:text-dark-400">
                  {{ plan.next_run_at ? formatDateTime(plan.next_run_at) : t('admin.pelicanTests.plans.never') }}
                </td>
                <td class="px-4 py-3 text-gray-500 dark:text-dark-400">
                  {{ plan.last_run_at ? formatDateTime(plan.last_run_at) : t('admin.pelicanTests.plans.never') }}
                </td>
                <td class="px-4 py-3">
                  <span
                    class="rounded-full px-2 py-0.5 text-xs font-medium"
                    :class="plan.last_status === 'success'
                      ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                      : plan.last_status === 'failed'
                        ? 'bg-rose-50 text-rose-700 dark:bg-rose-900/30 dark:text-rose-300'
                        : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-300'"
                    :title="plan.last_error || ''"
                  >
                    {{ plan.last_status === 'success'
                      ? t('admin.pelicanTests.status.success')
                      : plan.last_status === 'failed'
                        ? t('admin.pelicanTests.status.failed')
                        : '-' }}
                  </span>
                </td>
                <td class="px-4 py-3 text-right">
                  <div class="flex flex-wrap items-center justify-end gap-2">
                    <button class="btn btn-secondary" :disabled="runningPlanId === plan.id" @click="runPlanNow(plan)">
                      {{ runningPlanId === plan.id
                        ? t('admin.pelicanTests.plans.running')
                        : t('admin.pelicanTests.plans.runNow') }}
                    </button>
                    <button class="btn btn-secondary" @click="openEditPlan(plan)">
                      {{ t('admin.pelicanTests.plans.edit') }}
                    </button>
                    <button class="btn btn-danger" :disabled="deletingPlanId === plan.id" @click="askDeletePlan(plan)">
                      {{ t('admin.pelicanTests.plans.delete') }}
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
          {{ t('admin.pelicanTests.form.title') }}
        </h2>
        <div class="mt-4 grid gap-4 lg:grid-cols-2">
          <div>
            <label class="block text-sm font-medium text-gray-900 dark:text-white">
              {{ t('admin.pelicanTests.form.account') }}
            </label>
            <select v-model="form.accountId" class="input mt-2 w-full">
              <option value="">{{ t('admin.pelicanTests.form.accountPlaceholder') }}</option>
              <option v-for="account in accounts" :key="account.id" :value="account.id">
                #{{ account.id }} {{ account.name }} · {{ account.platform }}
              </option>
            </select>
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-900 dark:text-white">
              {{ t('admin.pelicanTests.form.model') }}
            </label>
            <input
              v-model="form.model"
              type="text"
              class="input mt-2 w-full"
              :placeholder="t('admin.pelicanTests.form.modelPlaceholder')"
            />
          </div>
          <div class="lg:col-span-2">
            <div class="flex items-center justify-between gap-3">
              <label class="block text-sm font-medium text-gray-900 dark:text-white">
                {{ t('admin.pelicanTests.form.prompt') }}
              </label>
              <button class="text-xs text-primary-600 hover:underline dark:text-primary-400" @click="resetPrompt">
                {{ t('admin.pelicanTests.form.resetPrompt') }}
              </button>
            </div>
            <textarea
              v-model="form.prompt"
              rows="5"
              class="input mt-2 w-full font-mono text-xs"
              :placeholder="t('admin.pelicanTests.form.promptPlaceholder')"
            ></textarea>
          </div>
        </div>
        <div class="mt-4 flex justify-end">
          <button class="btn btn-primary" :disabled="running" @click="runTest">
            <Icon v-if="!running" name="play" size="md" class="mr-1" />
            {{ running ? t('admin.pelicanTests.form.running') : t('admin.pelicanTests.form.run') }}
          </button>
        </div>
      </section>

      <section class="rounded-xl border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-800">
        <div class="flex items-center justify-between px-4 py-3 sm:px-6">
          <h2 class="text-lg font-medium text-gray-900 dark:text-white">
            {{ t('admin.pelicanTests.history.title') }}
          </h2>
          <span class="text-xs text-gray-500 dark:text-dark-400">
            {{ t('admin.codexTickets.manage.pagination.info', { page, pages: totalPages, total }) }}
          </span>
        </div>
        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
            <thead>
              <tr class="text-left text-xs uppercase tracking-wide text-gray-500 dark:text-dark-400">
                <th class="px-4 py-3">{{ t('admin.pelicanTests.history.id') }}</th>
                <th class="px-4 py-3">{{ t('admin.pelicanTests.history.account') }}</th>
                <th class="px-4 py-3">{{ t('admin.pelicanTests.history.model') }}</th>
                <th class="px-4 py-3">{{ t('admin.pelicanTests.history.status') }}</th>
                <th class="px-4 py-3">{{ t('admin.pelicanTests.history.latency') }}</th>
                <th class="px-4 py-3">{{ t('admin.pelicanTests.history.createdAt') }}</th>
                <th class="px-4 py-3 text-right">{{ t('admin.pelicanTests.history.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-if="loading">
                <td colspan="7" class="px-4 py-10 text-center text-gray-500 dark:text-dark-400">
                  {{ t('common.loading') }}
                </td>
              </tr>
              <tr v-else-if="items.length === 0">
                <td colspan="7" class="px-4 py-10 text-center text-gray-500 dark:text-dark-400">
                  {{ t('admin.pelicanTests.history.empty') }}
                </td>
              </tr>
              <tr v-for="item in items" :key="item.id" class="hover:bg-gray-50 dark:hover:bg-dark-700/40">
                <td class="px-4 py-3 text-gray-500 dark:text-dark-400">#{{ item.id }}</td>
                <td class="px-4 py-3 text-gray-700 dark:text-gray-200">
                  #{{ item.account_id }} {{ item.account_name }}
                </td>
                <td class="px-4 py-3 text-gray-600 dark:text-gray-300">{{ item.model || '-' }}</td>
                <td class="px-4 py-3">
                  <span
                    class="rounded-full px-2 py-0.5 text-xs font-medium"
                    :class="item.status === 'success'
                      ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                      : 'bg-rose-50 text-rose-700 dark:bg-rose-900/30 dark:text-rose-300'"
                    :title="item.error_message || ''"
                  >
                    {{ item.status === 'success'
                      ? t('admin.pelicanTests.status.success')
                      : t('admin.pelicanTests.status.failed') }}
                  </span>
                </td>
                <td class="px-4 py-3 text-gray-600 dark:text-gray-300">{{ item.latency_ms }} ms</td>
                <td class="px-4 py-3 text-gray-500 dark:text-dark-400">{{ formatDateTime(item.created_at) }}</td>
                <td class="px-4 py-3 text-right">
                  <button class="btn btn-secondary" @click="openPreview(item.id)">
                    {{ t('admin.pelicanTests.history.view') }}
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="flex items-center justify-end gap-2 border-t border-gray-200 px-4 py-3 dark:border-dark-700">
          <button class="btn btn-secondary" :disabled="page <= 1 || loading" @click="goPage(page - 1)">
            {{ t('admin.codexTickets.manage.pagination.prev') }}
          </button>
          <button class="btn btn-secondary" :disabled="page >= totalPages || loading" @click="goPage(page + 1)">
            {{ t('admin.codexTickets.manage.pagination.next') }}
          </button>
        </div>
      </section>
    </div>

    <BaseDialog :show="showPreview" :title="t('admin.pelicanTests.preview.title')" width="full" @close="showPreview = false">
      <div class="space-y-3">
        <p class="text-xs text-gray-500 dark:text-dark-400">
          {{ t('admin.pelicanTests.preview.sandboxNote') }}
        </p>
        <div v-if="previewLoading" class="py-16 text-center text-sm text-gray-500 dark:text-dark-400">
          {{ t('admin.pelicanTests.preview.loading') }}
        </div>
        <div v-else-if="previewError" class="py-16 text-center text-sm text-rose-600 dark:text-rose-400">
          {{ previewError }}
        </div>
        <div v-else-if="!previewOutput" class="py-16 text-center text-sm text-gray-500 dark:text-dark-400">
          {{ t('admin.pelicanTests.preview.empty') }}
        </div>
        <iframe
          v-else
          :srcdoc="previewOutput"
          sandbox="allow-scripts"
          class="h-[70vh] w-full rounded-lg border border-gray-200 bg-white dark:border-dark-700"
          referrerpolicy="no-referrer"
        ></iframe>
      </div>
    </BaseDialog>

    <BaseDialog
      :show="showPlanDialog"
      :title="editingPlan ? t('admin.pelicanTests.plans.editor.editTitle') : t('admin.pelicanTests.plans.editor.createTitle')"
      width="normal"
      @close="showPlanDialog = false"
    >
      <div class="space-y-4">
        <div>
          <label class="block text-sm font-medium text-gray-900 dark:text-white">
            {{ t('admin.pelicanTests.plans.editor.account') }}
          </label>
          <select v-model="planForm.accountId" class="input mt-2 w-full">
            <option value="">{{ t('admin.pelicanTests.form.accountPlaceholder') }}</option>
            <option v-for="account in accounts" :key="account.id" :value="account.id">
              #{{ account.id }} {{ account.name }} · {{ account.platform }}
            </option>
          </select>
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-900 dark:text-white">
            {{ t('admin.pelicanTests.plans.editor.model') }}
          </label>
          <input v-model="planForm.model" type="text" class="input mt-2 w-full" />
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-900 dark:text-white">
            {{ t('admin.pelicanTests.plans.editor.prompt') }}
          </label>
          <textarea v-model="planForm.prompt" rows="4" class="input mt-2 w-full font-mono text-xs"></textarea>
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-900 dark:text-white">
            {{ t('admin.pelicanTests.plans.editor.cron') }}
          </label>
          <input v-model="planForm.cronExpression" type="text" class="input mt-2 w-full font-mono text-xs" placeholder="0 9 * * *" />
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
            {{ t('admin.pelicanTests.plans.editor.cronHint') }}
          </p>
        </div>
        <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-200">
          <input v-model="planForm.enabled" type="checkbox" class="h-4 w-4" />
          {{ t('admin.pelicanTests.plans.editor.enabled') }}
        </label>
      </div>
      <template #footer>
        <div class="flex justify-end gap-2">
          <button class="btn btn-secondary" @click="showPlanDialog = false">
            {{ t('admin.pelicanTests.plans.editor.cancel') }}
          </button>
          <button class="btn btn-primary" :disabled="savingPlan" @click="savePlan">
            {{ t('admin.pelicanTests.plans.editor.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="showDeletePlanDialog"
      :title="t('admin.pelicanTests.plans.deleteTitle')"
      :message="t('admin.pelicanTests.plans.deleteConfirm')"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="confirmDeletePlan"
      @cancel="showDeletePlanDialog = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'
import { adminAPI, type PelicanTestListItem, type PelicanTestPlan, type PelicanTestRecord } from '@/api/admin'
import type { AccountListItem } from '@/types'

const DEFAULT_PROMPT =
  '用单个 HTML 文件 + 内联 SVG 画一只骑自行车的鹈鹕，要求构图完整、比例协调、细节丰富。只输出完整 HTML，不要解释，不要 Markdown 代码块，不要使用外部图片或脚本。'

const { t } = useI18n()
const appStore = useAppStore()

const accounts = ref<AccountListItem[]>([])
const items = ref<PelicanTestListItem[]>([])
const loading = ref(false)
const running = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))

const showPreview = ref(false)
const previewLoading = ref(false)
const previewError = ref('')
const previewOutput = ref('')

const form = reactive({
  accountId: '' as number | '',
  model: '',
  prompt: DEFAULT_PROMPT
})

const plans = ref<PelicanTestPlan[]>([])
const showPlanDialog = ref(false)
const editingPlan = ref<PelicanTestPlan | null>(null)
const savingPlan = ref(false)
const runningPlanId = ref<number | null>(null)
const deletingPlanId = ref<number | null>(null)
const showDeletePlanDialog = ref(false)
const pendingDeletePlan = ref<PelicanTestPlan | null>(null)
const planForm = reactive({
  accountId: '' as number | '',
  model: '',
  prompt: DEFAULT_PROMPT,
  cronExpression: '0 9 * * *',
  enabled: true
})

function errorMessage(error: unknown): string {
  if (error && typeof error === 'object' && 'message' in error) {
    return String((error as { message?: unknown }).message || '')
  }
  return String(error)
}

async function loadAccounts() {
  try {
    const result = await adminAPI.accounts.list(1, 200, { platform: 'openai' })
    accounts.value = result.items
  } catch (error) {
    appStore.showError(t('admin.pelicanTests.messages.loadAccountsFailed', { error: errorMessage(error) }))
  }
}

async function loadPlans() {
  try {
    plans.value = await adminAPI.pelicanTests.listPelicanPlans()
  } catch (error) {
    appStore.showError(t('admin.pelicanTests.messages.loadFailed', { error: errorMessage(error) }))
  }
}

function openCreatePlan() {
  editingPlan.value = null
  planForm.accountId = ''
  planForm.model = ''
  planForm.prompt = DEFAULT_PROMPT
  planForm.cronExpression = '0 9 * * *'
  planForm.enabled = true
  showPlanDialog.value = true
}

function openEditPlan(plan: PelicanTestPlan) {
  editingPlan.value = plan
  planForm.accountId = plan.account_id
  planForm.model = plan.model
  planForm.prompt = plan.prompt
  planForm.cronExpression = plan.cron_expression
  planForm.enabled = plan.enabled
  showPlanDialog.value = true
}

async function savePlan() {
  if (!planForm.accountId || !planForm.cronExpression.trim()) {
    appStore.showError(t('admin.pelicanTests.plans.editor.required'))
    return
  }
  savingPlan.value = true
  try {
    const payload = {
      account_id: Number(planForm.accountId),
      model: planForm.model.trim(),
      prompt: planForm.prompt,
      cron_expression: planForm.cronExpression.trim(),
      enabled: planForm.enabled
    }
    if (editingPlan.value) {
      await adminAPI.pelicanTests.updatePelicanPlan(editingPlan.value.id, payload)
      appStore.showSuccess(t('admin.pelicanTests.plans.messages.updated'))
    } else {
      await adminAPI.pelicanTests.createPelicanPlan(payload)
      appStore.showSuccess(t('admin.pelicanTests.plans.messages.created'))
    }
    showPlanDialog.value = false
    await loadPlans()
  } catch (error) {
    appStore.showError(t('admin.pelicanTests.plans.messages.saveFailed', { error: errorMessage(error) }))
  } finally {
    savingPlan.value = false
  }
}

async function runPlanNow(plan: PelicanTestPlan) {
  runningPlanId.value = plan.id
  try {
    const record = await adminAPI.pelicanTests.runPelicanPlanNow(plan.id)
    if (record.status === 'success') {
      appStore.showSuccess(t('admin.pelicanTests.plans.messages.runDone'))
    } else {
      appStore.showError(t('admin.pelicanTests.messages.failed', { error: record.error_message || '-' }))
    }
    previewOutput.value = record.output
    previewError.value = ''
    showPreview.value = true
    await Promise.all([loadPlans(), loadHistory()])
  } catch (error) {
    appStore.showError(t('admin.pelicanTests.plans.messages.runFailed', { error: errorMessage(error) }))
  } finally {
    runningPlanId.value = null
  }
}

function askDeletePlan(plan: PelicanTestPlan) {
  pendingDeletePlan.value = plan
  showDeletePlanDialog.value = true
}

async function confirmDeletePlan() {
  const plan = pendingDeletePlan.value
  if (!plan) return
  showDeletePlanDialog.value = false
  deletingPlanId.value = plan.id
  try {
    await adminAPI.pelicanTests.deletePelicanPlan(plan.id)
    appStore.showSuccess(t('admin.pelicanTests.plans.messages.deleted'))
    await loadPlans()
  } catch (error) {
    appStore.showError(t('admin.pelicanTests.plans.messages.deleteFailed', { error: errorMessage(error) }))
  } finally {
    deletingPlanId.value = null
    pendingDeletePlan.value = null
  }
}

async function loadHistory() {
  loading.value = true
  try {
    const result = await adminAPI.pelicanTests.listPelicanTests(page.value, pageSize.value)
    items.value = result.items
    total.value = result.total
  } catch (error) {
    appStore.showError(t('admin.pelicanTests.messages.loadFailed', { error: errorMessage(error) }))
  } finally {
    loading.value = false
  }
}

function goPage(next: number) {
  if (next < 1 || next > totalPages.value) return
  page.value = next
  void loadHistory()
}

function resetPrompt() {
  form.prompt = DEFAULT_PROMPT
}

async function runTest() {
  if (!form.accountId) {
    appStore.showError(t('admin.pelicanTests.messages.selectAccount'))
    return
  }
  running.value = true
  try {
    const record: PelicanTestRecord = await adminAPI.pelicanTests.runPelicanTest({
      account_id: Number(form.accountId),
      model: form.model.trim(),
      prompt: form.prompt
    })
    if (record.status === 'success') {
      appStore.showSuccess(t('admin.pelicanTests.messages.done'))
    } else {
      appStore.showError(
        t('admin.pelicanTests.messages.failed', { error: record.error_message || '-' })
      )
    }
    previewOutput.value = record.output
    previewError.value = ''
    showPreview.value = true
    page.value = 1
    await loadHistory()
  } catch (error) {
    appStore.showError(t('admin.pelicanTests.messages.failed', { error: errorMessage(error) }))
  } finally {
    running.value = false
  }
}

async function openPreview(id: number) {
  showPreview.value = true
  previewLoading.value = true
  previewError.value = ''
  previewOutput.value = ''
  try {
    const record = await adminAPI.pelicanTests.getPelicanTest(id)
    previewOutput.value = record.output
    if (!record.output) {
      previewError.value = record.error_message
        ? t('admin.pelicanTests.preview.loadFailed') + ': ' + record.error_message
        : ''
    }
  } catch (error) {
    previewError.value = t('admin.pelicanTests.preview.loadFailed') + ': ' + errorMessage(error)
  } finally {
    previewLoading.value = false
  }
}

onMounted(async () => {
  await Promise.all([loadAccounts(), loadHistory(), loadPlans()])
})
</script>
