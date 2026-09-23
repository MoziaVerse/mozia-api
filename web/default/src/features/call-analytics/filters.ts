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
import { z } from 'zod'

import type { AnalyticsFilters } from './api'

const optionalID = z
  .string()
  .refine(
    (value) =>
      value === '' ||
      (/^[1-9]\d*$/.test(value) && Number.isSafeInteger(Number(value))),
    'Enter a positive integer ID.'
  )

export const analyticsFilterSchema = z
  .object({
    start: z.string(),
    end: z.string(),
    user: optionalID,
    model: z.string(),
    channel: optionalID,
    outcome: z.enum(['', 'success', 'error', 'cancelled', 'unknown']),
  })
  .refine(
    (value) => {
      const start = new Date(value.start).getTime()
      const end = new Date(value.end).getTime()
      return (
        Number.isFinite(start) &&
        Number.isFinite(end) &&
        end > start &&
        end - start <= 31 * 86400000
      )
    },
    { path: ['end'], message: 'Select a valid time range of up to 31 days.' }
  )

export type AnalyticsFilterForm = z.infer<typeof analyticsFilterSchema>

export function analyticsQueryFilters(
  value: AnalyticsFilterForm
): AnalyticsFilters {
  return {
    start_timestamp: Math.floor(new Date(value.start).getTime() / 1000),
    end_timestamp: Math.floor(new Date(value.end).getTime() / 1000),
    user_id: value.user ? Number(value.user) : undefined,
    model_name: value.model.trim() || undefined,
    channel: value.channel ? Number(value.channel) : undefined,
    outcome: value.outcome || undefined,
  }
}

export function requestLogTimeRange(
  startTimestamp: number,
  endTimestamp: number,
  attempts: readonly { created_at: number }[]
): { startTime: number; endTime: number } {
  const earliestAttempt = attempts.reduce(
    (earliest, attempt) => Math.min(earliest, attempt.created_at),
    startTimestamp
  )
  return { startTime: earliestAttempt * 1000, endTime: endTimestamp * 1000 }
}
