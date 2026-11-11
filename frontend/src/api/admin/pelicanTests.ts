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

export default { listPelicanTests, runPelicanTest, getPelicanTest }
