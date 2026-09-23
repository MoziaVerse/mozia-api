/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { api } from '@/lib/api'

export type CallOutcome = 'success' | 'error' | 'cancelled' | 'unknown'

export interface AnalyticsFilters {
  start_timestamp: number
  end_timestamp: number
  user_id?: number
  model_name?: string
  channel?: number
  outcome?: CallOutcome
}

export interface AnalyticsSummary {
  requests: number
  success: number
  errors: number
  cancelled: number
  unknown: number
  success_rate: number | null
  error_rate: number | null
  quota: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  cache_hit_rate: number | null
  avg_rpm: number | null
  peak_rpm: number | null
  avg_tpm: number | null
  peak_tpm: number | null
  avg_frt_ms: number | null
  p95_frt_ms: number | null
  avg_duration_ms: number | null
  p95_duration_ms: number | null
  retried_requests: number
  recovered_requests: number
}

export interface AnalyticsRequest {
  request_id: string
  user_id: number
  username: string
  completed_at: number
  model_name: string
  effective_model: string
  channel_id: number
  outcome: CallOutcome
  status_code: number
  quota: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  cache_usage_reported: boolean
  frt_ms: number | null
  duration_ms: number | null
  attempts: number
  routing_rule_id: string
  final_recorded: boolean
  error_code: string
  attempt_logs: {
    channel_id: number
    created_at: number
    status_code: number
    error_code: string
    type: 'consume' | 'error'
  }[]
}

export interface CallAnalytics {
  start_timestamp: number
  end_timestamp: number
  time_basis: 'completed_at'
  summary: AnalyticsSummary
  trend_interval_seconds: number
  trend: {
    timestamp: number
    requests: number
    success: number
    errors: number
    tokens: number
    quota: number
  }[]
  users: (AnalyticsSummary & { user_id: number; username: string })[]
  channels: (AnalyticsSummary & { channel_id: number })[]
  errors: { status_code: number; error_code: string; count: number }[]
  requests: {
    items: AnalyticsRequest[]
    total: number
    p: number
    page_size: number
  }
  coverage: {
    final_recorded_requests: number
    inferred_requests: number
    unknown_requests: number
    frt_samples: number
    cache_samples: number
    scanned_logs: number
  }
  warnings: string[]
}

export async function getCallAnalytics(
  filters: AnalyticsFilters,
  page: number,
  signal?: AbortSignal
): Promise<CallAnalytics> {
  const response = await api.get<{
    success: boolean
    message?: string
    data: CallAnalytics
  }>('/api/log/call-report', {
    params: { ...filters, p: page, page_size: 20 },
    signal,
  })
  if (!response.data.success) {
    throw new Error(response.data.message || 'Failed to load call analytics')
  }
  return response.data.data
}

export async function searchAnalyticsUsers(
  keyword: string
): Promise<{ id: number; username: string }[]> {
  const response = await api.get<{
    success: boolean
    message?: string
    data: { id: number; username: string }[]
  }>('/api/log/call-report/users', { params: { keyword } })
  if (!response.data.success) {
    throw new Error(response.data.message || 'Failed to load users')
  }
  return response.data.data
}
