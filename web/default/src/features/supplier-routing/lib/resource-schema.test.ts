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
