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

import type { SupplierRoutingData } from '../api'
import {
  editableSupplierResource,
  parseSupplierResourceJSON,
  supplierFormResource,
  supplierResourceFormValues,
  supplierModelChannels,
} from './resource-schema'

test('record JSON permits incomplete business values when switching, and validates on save', () => {
  const raw = '{"name":"","enabled":false,"contact":""}'
  assert.equal(parseSupplierResourceJSON('supplier', raw, true).name, '')
  assert.throws(() => parseSupplierResourceJSON('supplier', raw))
  assert.throws(() =>
    parseSupplierResourceJSON('supplier', '{"name":{},"enabled":false}', true)
  )
  assert.throws(() =>
    parseSupplierResourceJSON(
      'supplier',
      '{"name":"A","enabled":false,"version":99}'
    )
  )
})

test('an editor saves only its selected record and preserves explicit false, zero and empty text', () => {
  const data: SupplierRoutingData = {
    config: {
      revision: 3,
      enabled: true,
      shadow: true,
      canary_percent: 10,
      suppliers: [
        { id: 1, name: 'A', enabled: true },
        { id: 2, name: 'B', enabled: true },
      ],
      pools: [],
      bindings: [],
      rules: [],
    },
    channels: [],
    revisions: [],
    settings: {
      version: 1,
      runtime_revision: 3,
      enabled: true,
      shadow: true,
      canary_percent: 10,
    },
  }
  const values = supplierResourceFormValues(data, 'supplier', {
    id: 1,
    version: 7,
    runtime_revision: 3,
    name: 'A',
    enabled: false,
    contact: '',
  })
  assert.deepEqual(supplierFormResource('supplier', values), {
    name: 'A',
    enabled: false,
    contact: '',
  })
  assert.equal(data.config.suppliers?.[1].name, 'B')
  assert.deepEqual(
    editableSupplierResource('settings', {
      version: 7,
      enabled: false,
      shadow: false,
      canary_percent: 0,
    }),
    { enabled: false, shadow: false, canary_percent: 0 }
  )
})

test('pool JSON round trip saves specifications and multiple channel associations in one record', () => {
  const data: SupplierRoutingData = {
    config: {
      revision: 1,
      enabled: false,
      shadow: false,
      canary_percent: 0,
      suppliers: [],
      pools: [],
      bindings: [],
      rules: [],
    },
    settings: {
      version: 1,
      runtime_revision: 1,
      enabled: false,
      shadow: false,
      canary_percent: 0,
    },
    channels: [],
    revisions: [],
  }
  const values = supplierResourceFormValues(data, 'pool', undefined, 2)
  const pool = values.pools[0]
  pool.name = 'Shared capacity'
  pool.failure_domain = 'dc-a'
  pool.models[0].name = 'test'
  pool.models[0].version = 'v1'
  pool.bindings = [
    { channel_id: 1, model: 'test' },
    { channel_id: 300, model: 'test' },
  ]
  const payload = supplierFormResource('pool', values)
  assert.deepEqual(
    parseSupplierResourceJSON('pool', JSON.stringify(payload)),
    payload
  )
  assert.deepEqual(payload.bindings, pool.bindings)
  assert.equal('supplier_id' in payload, false)
  assert.equal('id' in payload, false)
  const restored = supplierResourceFormValues(data, 'pool', {
    ...payload,
    id: 9,
    supplier_id: 2,
    version: 2,
    runtime_revision: 2,
  })
  assert.deepEqual(restored.pools[0].bindings, pool.bindings)
  pool.models[0].version = ''
  const incomplete = JSON.stringify(supplierFormResource('pool', values))
  assert.doesNotThrow(() => parseSupplierResourceJSON('pool', incomplete, true))
  assert.throws(() => parseSupplierResourceJSON('pool', incomplete))
})

test('model choices show all matching channel types with occupancy reasons, without truncation', () => {
  const data: SupplierRoutingData = {
    config: {
      revision: 1,
      enabled: false,
      shadow: false,
      canary_percent: 0,
      suppliers: [],
      pools: [],
      bindings: [],
      rules: [],
    },
    settings: {
      version: 1,
      runtime_revision: 1,
      enabled: false,
      shadow: false,
      canary_percent: 0,
    },
    channels: [
      { id: 1, name: 'Current', type: 1, status: 1, models: 'test,other' },
      { id: 2, name: 'Unsupported', type: 14, status: 1, models: 'test' },
      {
        id: 3,
        name: 'Other supplier',
        type: 1,
        status: 1,
        models: 'test,other',
      },
      { id: 4, name: 'Other pool', type: 1, status: 1, models: 'test' },
      { id: 5, name: 'Different model', type: 1, status: 1, models: 'other' },
      { id: 999, name: 'Disabled', type: 1, status: 2, models: ' test ' },
    ],
    revisions: [],
  }
  const pool = supplierResourceFormValues(data, 'pool', undefined, 1).pools[0]
  pool.id = 10
  data.config.pools = [
    pool,
    { ...pool, id: 20, supplier_id: 2 },
    { ...pool, id: 30 },
  ]
  data.config.bindings = [
    { channel_id: 1, pool_id: 10, model: 'test' },
    { channel_id: 3, pool_id: 20, model: 'other' },
    { channel_id: 4, pool_id: 30, model: 'test' },
  ]
  assert.deepEqual(
    supplierModelChannels(data, pool, 'test').map((c) => [c.id, c.reason]),
    [
      [1, undefined],
      [2, 'unsupported'],
      [3, 'supplier'],
      [4, 'pool'],
      [999, undefined],
    ]
  )
})
