<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-7xl space-y-6 p-4 sm:p-6">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">
            {{ t('admin.codexTickets.manage.title') }}
          </h1>
          <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-dark-400">
            {{ t('admin.codexTickets.manage.description') }}
          </p>
        </div>
        <button class="btn btn-secondary" :disabled="loading" @click="load">
          <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          <span class="ml-1">{{ t('common.refresh') }}</span>
        </button>
      </div>

      <section class="rounded-xl border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-800">
        <div class="flex flex-wrap items-end gap-3">
          <div class="w-full sm:w-36">
            <label class="mb-1 block text-xs font-medium text-gray-500 dark:text-dark-400">
              {{ t('admin.codexTickets.manage.filters.accountId') }}
            </label>
            <input
              v-model="filters.accountId"
              type="number"
              min="1"
              class="input w-full"
              :placeholder="t('admin.codexTickets.manage.filters.accountIdPlaceholder')"
              @keyup.enter="applyFilters"
            />
          </div>
          <div class="w-full sm:w-48">
            <label class="mb-1 block text-xs font-medium text-gray-500 dark:text-dark-400">
              {{ t('admin.codexTickets.manage.filters.model') }}
            </label>
            <input
              v-model="filters.model"
              type="text"
              class="input w-full"
              :placeholder="t('admin.codexTickets.manage.filters.modelPlaceholder')"
              @keyup.enter="applyFilters"
            />
          </div>
          <div class="w-full sm:w-36">
            <label class="mb-1 block text-xs font-medium text-gray-500 dark:text-dark-400">
              {{ t('admin.codexTickets.manage.filters.status') }}
            </label>
            <select v-model="filters.status" class="input w-full">
              <option value="">{{ t('admin.codexTickets.manage.filters.allStatus') }}</option>
              <option value="valid">{{ t('admin.codexTickets.manage.filters.valid') }}</option>
              <option value="expired">{{ t('admin.codexTickets.manage.filters.expired') }}</option>
            </select>
          </div>
          <div class="flex items-center gap-2">
            <button class="btn btn-primary" :disabled="loading" @click="applyFilters">
              {{ t('admin.codexTickets.manage.filters.search') }}
            </button>
            <button class="btn btn-secondary" :disabled="loading" @click="resetFilters">
              {{ t('admin.codexTickets.manage.filters.reset') }}
            </button>
          </div>
        </div>
      </section>

      <section class="rounded-xl border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-800">
        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
            <thead>
              <tr class="text-left text-xs uppercase tracking-wide text-gray-500 dark:text-dark-400">
                <th class="px-4 py-3">{{ t('admin.codexTickets.manage.table.id') }}</th>
                <th class="px-4 py-3">{{ t('admin.codexTickets.manage.table.model') }}</th>
                <th class="px-4 py-3">{{ t('admin.codexTickets.manage.table.account') }}</th>
                <th class="px-4 py-3">{{ t('admin.codexTickets.manage.table.status') }}</th>
                <th class="px-4 py-3">{{ t('admin.codexTickets.manage.table.length') }}</th>
                <th class="px-4 py-3">{{ t('admin.codexTickets.manage.table.acquisition') }}</th>
                <th class="px-4 py-3">{{ t('admin.codexTickets.manage.table.captured') }}</th>
                <th class="px-4 py-3">{{ t('admin.codexTickets.manage.table.expires') }}</th>
                <th class="px-4 py-3">{{ t('admin.codexTickets.manage.table.remaining') }}</th>
                <th class="px-4 py-3 text-right">{{ t('admin.codexTickets.manage.table.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-if="loading">
                <td colspan="10" class="px-4 py-10 text-center text-gray-500 dark:text-dark-400">
                  {{ t('common.loading') }}
                </td>
              </tr>
              <tr v-else-if="items.length === 0">
                <td colspan="10" class="px-4 py-10 text-center text-gray-500 dark:text-dark-400">
                  {{ t('admin.codexTickets.manage.empty') }}
                </td>
              </tr>
              <tr v-for="ticket in items" :key="ticket.id" class="hover:bg-gray-50 dark:hover:bg-dark-700/40">
                <td class="px-4 py-3 text-gray-500 dark:text-dark-400">#{{ ticket.id }}</td>
                <td class="px-4 py-3 font-medium text-gray-900 dark:text-white">{{ ticket.model }}</td>
                <td class="px-4 py-3 text-gray-600 dark:text-gray-300">
                  #{{ ticket.account_id }} {{ ticket.account_name || '' }}
                </td>
                <td class="px-4 py-3">
                  <span
                    class="rounded-full px-2 py-0.5 text-xs font-medium"
                    :class="ticket.status === 'valid'
                      ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                      : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-300'"
                  >
                    {{ ticket.status === 'valid'
                      ? t('admin.codexTickets.manage.status.valid')
                      : t('admin.codexTickets.manage.status.expired') }}
                  </span>
                </td>
                <td class="px-4 py-3 text-gray-600 dark:text-gray-300">{{ ticket.length }}</td>
                <td class="px-4 py-3 text-gray-500 dark:text-dark-400">{{ ticket.acquisition || '-' }}</td>
                <td class="px-4 py-3 text-gray-500 dark:text-dark-400">{{ formatDateTime(ticket.captured_at) }}</td>
                <td class="px-4 py-3 text-gray-500 dark:text-dark-400">{{ formatDateTime(ticket.expires_at) }}</td>
                <td class="px-4 py-3 text-gray-600 dark:text-gray-300">
                  {{ ticket.status === 'valid' ? formatRemaining(ticket.remaining_seconds) : '-' }}
                </td>
                <td class="px-4 py-3 text-right">
                  <div class="flex items-center justify-end gap-2">
                    <button
                      class="btn btn-secondary"
                      :disabled="refreshingId === ticket.id"
                      @click="refreshOne(ticket)"
                    >
                      {{ refreshingId === ticket.id
                        ? t('admin.codexTickets.manage.actions.refreshing')
                        : t('admin.codexTickets.manage.actions.refresh') }}
                    </button>
                    <button
                      class="btn btn-danger"
                      :disabled="deletingId === ticket.id"
                      @click="askDelete(ticket)"
                    >
                      {{ deletingId === ticket.id
                        ? t('admin.codexTickets.manage.actions.deleting')
                        : t('admin.codexTickets.manage.actions.delete') }}
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div class="flex flex-wrap items-center justify-between gap-3 border-t border-gray-200 px-4 py-3 dark:border-dark-700">
          <span class="text-xs text-gray-500 dark:text-dark-400">
            {{ t('admin.codexTickets.manage.pagination.info', { page: page, pages: totalPages, total }) }}
          </span>
          <div class="flex items-center gap-2">
            <button class="btn btn-secondary" :disabled="page <= 1 || loading" @click="goPage(page - 1)">
              {{ t('admin.codexTickets.manage.pagination.prev') }}
            </button>
            <button class="btn btn-secondary" :disabled="page >= totalPages || loading" @click="goPage(page + 1)">
              {{ t('admin.codexTickets.manage.pagination.next') }}
            </button>
          </div>
        </div>
      </section>
    </div>

    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('admin.codexTickets.manage.actions.delete')"
      :message="t('admin.codexTickets.manage.messages.deleteConfirm')"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="confirmDelete"
      @cancel="showDeleteDialog = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'
import {
  codexTicketsAPI,
  type CodexTicketListItem,
  type CodexTicketListResult
} from '@/api/admin'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const refreshingId = ref<number | null>(null)
const deletingId = ref<number | null>(null)
const showDeleteDialog = ref(false)
const pendingDelete = ref<CodexTicketListItem | null>(null)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const result = ref<CodexTicketListResult | null>(null)
const items = computed(() => result.value?.items ?? [])
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))

const filters = reactive({
  accountId: '',
  model: '',
  status: '' as '' | 'valid' | 'expired'
})

function errorMessage(error: unknown): string {
  if (error && typeof error === 'object' && 'message' in error) {
    return String((error as { message?: unknown }).message || '')
  }
  return String(error)
}

function formatRemaining(seconds: number): string {
  if (!seconds || seconds <= 0) return '0m'
  if (seconds >= 3600) {
    return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
  }
  return `${Math.max(1, Math.floor(seconds / 60))}m`
}

async function load() {
  loading.value = true
  try {
    result.value = await codexTicketsAPI.listTickets({
      page: page.value,
      page_size: pageSize.value,
      account_id: Number(filters.accountId) || undefined,
      model: filters.model.trim() || undefined,
      status: filters.status || undefined
    })
    total.value = result.value.total
  } catch (error) {
    appStore.showError(t('admin.codexTickets.manage.messages.loadFailed', { error: errorMessage(error) }))
  } finally {
    loading.value = false
  }
}

function applyFilters() {
  page.value = 1
  void load()
}

function resetFilters() {
  filters.accountId = ''
  filters.model = ''
  filters.status = ''
  page.value = 1
  void load()
}

function goPage(next: number) {
  if (next < 1 || next > totalPages.value) return
  page.value = next
  void load()
}

async function refreshOne(ticket: CodexTicketListItem) {
  refreshingId.value = ticket.id
  try {
    const summary = await codexTicketsAPI.refreshTicket(ticket.id)
    appStore.showSuccess(
      t('admin.codexTickets.manage.messages.refreshed', {
        ok: summary.succeeded,
        failed: summary.failed
      })
    )
    await load()
  } catch (error) {
    appStore.showError(t('admin.codexTickets.manage.messages.refreshFailed', { error: errorMessage(error) }))
  } finally {
    refreshingId.value = null
  }
}

function askDelete(ticket: CodexTicketListItem) {
  pendingDelete.value = ticket
  showDeleteDialog.value = true
}

async function confirmDelete() {
  const ticket = pendingDelete.value
  if (!ticket) return
  showDeleteDialog.value = false
  deletingId.value = ticket.id
  try {
    await codexTicketsAPI.deleteTicket(ticket.id)
    appStore.showSuccess(t('admin.codexTickets.manage.messages.deleted'))
    await load()
  } catch (error) {
    appStore.showError(t('admin.codexTickets.manage.messages.deleteFailed', { error: errorMessage(error) }))
  } finally {
    deletingId.value = null
    pendingDelete.value = null
  }
}

onMounted(load)
</script>
