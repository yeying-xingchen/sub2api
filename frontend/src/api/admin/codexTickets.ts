/**
 * Admin Codex ticket（打票 / 全站共用票）API.
 */

import { apiClient } from '../client'

export interface CodexTicketSettings {
  enabled: boolean
  shared: boolean
  fail_closed: boolean
  models: string[]
  harvest_interval_seconds: number
  ttl_seconds: number
  probe_timeout_seconds: number
}

export interface CodexTicketModelStatus {
  model: string
  ready: boolean
  own: boolean
  shared: boolean
  source_account_id?: number
  source_account_name?: string
  length?: number
  blocks?: number
  remaining_seconds: number
  captured_at?: string
  expires_at?: string
}

export interface CodexIQTestResult {
  account_id: number
  model: string
  verdict: 'healthy' | 'degraded' | 'inconclusive'
  reason: string
  failure?: string
  detail?: string
  mint_status: number
  continue_status: number
  minted: boolean
  new_ticket: boolean
  ticket_length: number
  continue_ticket_length: number
  short_ticket?: boolean
  latency_ms: number
  tested_at: string
}

export interface CodexTicketAccountStatus {
  id: number
  name: string
  status: string
  platform: string
  tickets: CodexTicketModelStatus[]
  iq?: CodexIQTestResult
}

export interface CodexTicketPoolEntry {
  model: string
  source_account_id: number
  source_account_name?: string
  length: number
  blocks?: number
  acquisition?: string
  captured_at: string
  expires_at: string
  remaining_seconds: number
}

export interface CodexTicketListItem {
  id: number
  account_id: number
  account_name?: string
  model: string
  status: 'valid' | 'expired'
  length: number
  blocks?: number
  acquisition?: string
  captured_at: string
  expires_at: string
  remaining_seconds: number
}

export interface CodexTicketListResult {
  items: CodexTicketListItem[]
  total: number
  page: number
  page_size: number
}

export interface CodexTicketListParams {
  page?: number
  page_size?: number
  account_id?: number
  model?: string
  status?: '' | 'valid' | 'expired'
}

export interface CodexTicketOverview {
  settings: CodexTicketSettings
  accounts: CodexTicketAccountStatus[]
  pool: CodexTicketPoolEntry[]
  last_round_at?: string
}

export interface CodexTicketHarvestResult {
  account_id: number
  account_name?: string
  model: string
  ok: boolean
  already_ready?: boolean
  status?: number
  length?: number
  blocks?: number
  error?: string
}

export interface CodexTicketHarvestSummary {
  started_at: string
  finished_at: string
  probed: number
  succeeded: number
  skipped: number
  failed: number
  results: CodexTicketHarvestResult[]
}

export async function getOverview(signal?: AbortSignal): Promise<CodexTicketOverview> {
  const { data } = await apiClient.get<CodexTicketOverview>('/admin/accounts/codex-tickets', { signal })
  return data
}

export async function updateSettings(settings: CodexTicketSettings): Promise<CodexTicketSettings> {
  const { data } = await apiClient.put<CodexTicketSettings>('/admin/accounts/codex-tickets/settings', settings)
  return data
}

export async function harvest(payload: {
  account_id?: number
  models?: string[]
}): Promise<CodexTicketHarvestSummary> {
  const { data } = await apiClient.post<CodexTicketHarvestSummary>(
    '/admin/accounts/codex-tickets/harvest',
    payload,
    { timeout: 300000 }
  )
  return data
}

export async function harvestAccount(
  accountId: number,
  models?: string[]
): Promise<CodexTicketHarvestSummary> {
  const { data } = await apiClient.post<CodexTicketHarvestSummary>(
    `/admin/accounts/${accountId}/codex-tickets/harvest`,
    { models: models ?? [] },
    { timeout: 300000 }
  )
  return data
}

export async function getAccountStatus(accountId: number): Promise<CodexTicketAccountStatus> {
  const { data } = await apiClient.get<CodexTicketAccountStatus>(
    `/admin/accounts/${accountId}/codex-tickets`
  )
  return data
}

export async function listTickets(params: CodexTicketListParams = {}): Promise<CodexTicketListResult> {
  const { data } = await apiClient.get<CodexTicketListResult>('/admin/codex-tickets', { params })
  return data
}

export async function deleteTicket(id: number): Promise<void> {
  await apiClient.delete(`/admin/codex-tickets/${id}`)
}

export async function refreshTicket(id: number): Promise<CodexTicketHarvestSummary> {
  const { data } = await apiClient.post<CodexTicketHarvestSummary>(
    `/admin/codex-tickets/${id}/refresh`,
    {},
    { timeout: 180000 }
  )
  return data
}

export async function runIQTest(accountId: number, model?: string): Promise<CodexIQTestResult> {
  const { data } = await apiClient.post<CodexIQTestResult>(
    `/admin/accounts/${accountId}/iq-test`,
    { model: model ?? '' },
    { timeout: 180000 }
  )
  return data
}

export async function getIQTest(accountId: number): Promise<CodexIQTestResult> {
  const { data } = await apiClient.get<CodexIQTestResult>(`/admin/accounts/${accountId}/iq-test`)
  return data
}

export default {
  getOverview,
  updateSettings,
  harvest,
  harvestAccount,
  getAccountStatus,
  runIQTest,
  getIQTest,
  listTickets,
  deleteTicket,
  refreshTicket
}
