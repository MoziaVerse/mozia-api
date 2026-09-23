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
import assert from 'node:assert/strict'
import { test } from 'node:test'

import {
  analyticsFilterSchema,
  analyticsQueryFilters,
  requestLogTimeRange,
} from './filters'

const input = {
  start: '2026-09-22T12:00',
  end: '2026-09-23T12:00',
  user: '',
  channel: '',
  model: '',
  outcome: '' as const,
}

test('analytics filters preserve exact model and selected IDs without adding implicit constraints', () => {
  const all = analyticsQueryFilters(analyticsFilterSchema.parse(input))
  assert.equal(all.end_timestamp - all.start_timestamp, 86400)
  assert.equal(all.user_id, undefined)
  assert.equal(all.channel, undefined)
  assert.equal(all.model_name, undefined)
  assert.equal(all.outcome, undefined)
  const selected = analyticsQueryFilters(
    analyticsFilterSchema.parse({
      ...input,
      user: '7073',
      channel: '422',
      model: ' moonshotai/kimi-k3 ',
      outcome: 'unknown',
    })
  )
  assert.equal(selected.user_id, 7073)
  assert.equal(selected.channel, 422)
  assert.equal(selected.model_name, 'moonshotai/kimi-k3')
  assert.equal(selected.outcome, 'unknown')
})

test('analytics rejects invalid and overlong ranges and malformed IDs before querying', () => {
  for (const invalid of [
    { start: 'invalid' },
    { end: input.start },
    { end: '2026-10-24T12:00' },
    { user: '7073x' },
    { channel: '-1' },
    { user: '9007199254740993' },
  ]) {
    assert.equal(
      analyticsFilterSchema.safeParse({ ...input, ...invalid }).success,
      false
    )
  }
  assert.equal(
    analyticsFilterSchema.safeParse({ ...input, end: '2026-10-23T12:00' })
      .success,
    true
  )
})

test('usage log links use milliseconds and include retries before the analysis window', () => {
  const start = Date.parse('2026-09-23T00:00:00Z') / 1000
  const end = start + 86400
  assert.deepEqual(requestLogTimeRange(start, end, []), {
    startTime: start * 1000,
    endTime: end * 1000,
  })
  assert.deepEqual(
    requestLogTimeRange(start, end, [
      { created_at: start + 1 },
      { created_at: start - 180 },
      { created_at: start - 60 },
    ]),
    {
      startTime: Date.parse('2026-09-22T23:57:00Z'),
      endTime: Date.parse('2026-09-24T00:00:00Z'),
    }
  )
})
