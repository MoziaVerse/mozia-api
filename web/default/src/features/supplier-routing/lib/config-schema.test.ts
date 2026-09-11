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
  newSupplierModel,
  newSupplierRule,
  parseSupplierConfigJSON,
  parseSupplierDraftJSON,
  supplierRoutingMode,
} from './config-schema'

test('JSON editing preserves explicit zero limits, disabled capabilities and extension fields', () => {
  const spec = {
    ...newSupplierModel('model-a'),
    version: 'v1',
    vendor_metadata: { accepted: true },
  }
  const config = {
    revision: 12,
    enabled: false,
    shadow: true,
    canary_percent: 0,
    suppliers: [
      {
        id: 9,
        name: 'Supplier A',
        enabled: false,
        commercial_reference: 'contract-1',
      },
    ],
    pools: [
      {
        id: 21,
        supplier_id: 9,
        name: 'Pool A',
        failure_domain: 'dc-a',
        enabled: false,
        limits: { concurrency: 3, rpm: 10, tpm: 100000 },
        max_execution_seconds: 60,
        input_safety_percent: 110,
        models: [spec],
      },
    ],
    bindings: [{ channel_id: 101, pool_id: 21, model: 'model-a' }],
    rules: [newSupplierRule('rule-a', 9, 'model-a')],
    operator_metadata: { note: 'keep' },
  }
  assert.deepEqual(parseSupplierConfigJSON(JSON.stringify(config)), config)
  assert.equal(
    parseSupplierConfigJSON(JSON.stringify(config)).pools[0].models[0].limits
      .tpm,
    0
  )
  assert.equal(
    parseSupplierConfigJSON(JSON.stringify(config)).pools[0].models[0].tools,
    false
  )
})

test('invalid JSON and malformed configuration cannot replace the form draft', () => {
  const empty = {
    revision: 0,
    enabled: false,
    shadow: true,
    canary_percent: 0,
    suppliers: [],
    pools: [],
    bindings: [],
    rules: [],
  }
  assert.deepEqual(parseSupplierConfigJSON(JSON.stringify(empty)), empty)
  for (const raw of [
    '{',
    'null',
    '[]',
    JSON.stringify({ ...empty, suppliers: {} }),
    JSON.stringify({ ...empty, canary_percent: 10.5 }),
    JSON.stringify({ ...empty, canary_percent: 101 }),
    JSON.stringify({ ...empty, enabled: 'false' }),
    JSON.stringify({
      ...empty,
      bindings: [{ channel_id: '101', pool_id: 21, model: 'model-a' }],
    }),
  ]) {
    assert.throws(() => parseSupplierConfigJSON(raw))
  }
})

test('effective mode matches the backend switch and stable rollout precedence', () => {
  for (const [enabled, shadow, percent, expected] of [
    [false, false, 100, 'legacy'],
    [false, true, 10, 'legacy'],
    [true, true, 100, 'observe'],
    [true, false, 0, 'observe'],
    [true, false, 10, 'canary'],
    [true, false, 100, 'active'],
  ] as const) {
    assert.equal(
      supplierRoutingMode({ enabled, shadow, canary_percent: percent }),
      expected
    )
  }
})

test('unfinished JSON business fields can return to visual editing but cannot publish', () => {
  const draft = {
    revision: 1,
    enabled: false,
    shadow: true,
    canary_percent: 10.5,
    suppliers: [{ id: 1, name: '', enabled: false }],
    pools: [
      {
        id: 1,
        supplier_id: 1,
        name: '',
        failure_domain: '',
        enabled: false,
        limits: { concurrency: 0, rpm: 0, tpm: 0 },
        max_execution_seconds: 0,
        input_safety_percent: 90,
        models: [newSupplierModel()],
        metadata: { keep: true },
      },
    ],
    bindings: [],
    rules: [],
    operator_metadata: { keep: true },
  }
  const text = JSON.stringify(draft)
  assert.deepEqual(parseSupplierDraftJSON(text), draft)
  assert.throws(() => parseSupplierConfigJSON(text))
  const complete = {
    ...draft,
    canary_percent: 10,
    suppliers: [{ id: 1, name: 'Supplier A', enabled: false }],
    pools: [],
  }
  assert.deepEqual(parseSupplierConfigJSON(JSON.stringify(complete)), complete)
})

test('structurally incompatible drafts cannot replace the last displayable configuration', () => {
  const base = {
    revision: 0,
    enabled: false,
    shadow: true,
    canary_percent: 0,
    suppliers: [],
    pools: [],
    bindings: [],
    rules: [],
  }
  for (const text of [
    '{',
    'null',
    JSON.stringify({ ...base, suppliers: {} }),
    JSON.stringify({ ...base, pools: [null] }),
    JSON.stringify({ ...base, enabled: 'false' }),
  ]) {
    assert.throws(() => parseSupplierDraftJSON(text))
  }
})
