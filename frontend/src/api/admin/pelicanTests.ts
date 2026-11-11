import { apiClient } from '../client'

export interface PelicanTestListItem {
  id: number
  account_id: number
  account_name: string
  platform: string
  model: string
  status: 'success' | 'failed'
  error_message?: string
  latency_ms: number
  created_at: string
}

export interface PelicanTestRecord extends PelicanTestListItem {
  prompt: string
  output: string
}

export interface PelicanTestPlan {
  id: number
  account_id: number
  account_name: string
  model: string
  prompt: string
  cron_expression: string
  enabled: boolean
  max_results: number
  last_run_at?: string
  next_run_at?: string
  last_status?: string
  last_error?: string
  created_at: string
  updated_at: string
}

export interface PelicanTestPlanInput {
  account_id: number
  model?: string
  prompt?: string
  cron_expression: string
  enabled: boolean
}

export interface PelicanTestListResult {
  items: PelicanTestListItem[]
  total: number
  page: number
  page_size: number
}

export async function listPelicanTests(page = 1, pageSize = 20): Promise<PelicanTestListResult> {
  const { data } = await apiClient.get<PelicanTestListResult>('/admin/pelican-tests', {
    params: { page, page_size: pageSize }
  })
  return data
}

export async function runPelicanTest(input: {
  account_id: number
  model?: string
  prompt?: string
}): Promise<PelicanTestRecord> {
  const { data } = await apiClient.post<PelicanTestRecord>('/admin/pelican-tests', input, {
    timeout: 600000
  })
  return data
}

export async function getPelicanTest(id: number): Promise<PelicanTestRecord> {
  const { data } = await apiClient.get<PelicanTestRecord>(`/admin/pelican-tests/${id}`)
  return data
}

export async function listPelicanPlans(): Promise<PelicanTestPlan[]> {
  const { data } = await apiClient.get<PelicanTestPlan[]>('/admin/pelican-test-plans')
  return data
}

export async function createPelicanPlan(input: PelicanTestPlanInput): Promise<PelicanTestPlan> {
  const { data } = await apiClient.post<PelicanTestPlan>('/admin/pelican-test-plans', input)
  return data
}

export async function updatePelicanPlan(id: number, input: PelicanTestPlanInput): Promise<PelicanTestPlan> {
  const { data } = await apiClient.put<PelicanTestPlan>(`/admin/pelican-test-plans/${id}`, input)
  return data
}

export async function deletePelicanPlan(id: number): Promise<void> {
  await apiClient.delete(`/admin/pelican-test-plans/${id}`)
}

export async function runPelicanPlanNow(id: number): Promise<PelicanTestRecord> {
  const { data } = await apiClient.post<PelicanTestRecord>(`/admin/pelican-test-plans/${id}/run`, {}, {
    timeout: 600000
  })
  return data
}

export default {
  listPelicanTests,
  runPelicanTest,
  getPelicanTest,
  listPelicanPlans,
  createPelicanPlan,
  updatePelicanPlan,
  deletePelicanPlan,
  runPelicanPlanNow
}
