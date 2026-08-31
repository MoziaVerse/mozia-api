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
import test from 'node:test'

import { buildOfficialPriceOptions } from './official-pricing-config'
import { parseTaskBillingDraft } from './task-billing-config'

test('official price keys match parametric video comparison rows', () => {
  const taskBilling = parseTaskBillingDraft(
    JSON.stringify({
      version: 1,
      mode: 'parametric',
      dimensions: [
        {
          name: 'duration',
          kind: 'number',
          paths: ['duration'],
          default: 5,
          unit: 1,
          round: 'ceil',
        },
        {
          name: 'resolution',
          kind: 'enum',
          paths: ['resolution'],
          default: '720p',
          values: { '480p': 1, '720p': 2.15 },
        },
      ],
    })
  )

  assert.deepEqual(
    buildOfficialPriceOptions({
      pricingMode: 'parametric',
      laneEnabled: {
        completion: false,
        cache: false,
        createCache: false,
        image: false,
        audioInput: false,
        audioOutput: false,
        videoInput: false,
      },
      referenceVideoPrice: '',
      taskBilling,
    }).map((option) => option.key),
    ['task:second:resolution=480p', 'task:second:resolution=720p']
  )
})
