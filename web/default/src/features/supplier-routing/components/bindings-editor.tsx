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
import { useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import type { SupplierRoutingData } from '../api'
import type { SupplierConfigValues } from '../lib/config-schema'
import { ConfigField } from './config-fields'

export function BindingFields(props: {
  channels: SupplierRoutingData['channels']
  allBindings: SupplierConfigValues['bindings']
  supplierId: number
}) {
  const { t } = useTranslation()
  const form = useFormContext<SupplierConfigValues>()
  const [allPools, binding] = useWatch({
    control: form.control,
    name: ['pools', 'bindings.0'],
  })
  const pools = allPools.filter((pool) => pool.supplier_id === props.supplierId)
  const channels = props.channels.filter(
    (channel) =>
      channel.type === 1 &&
      !props.allBindings.some(
        (binding) =>
          binding.channel_id === channel.id &&
          allPools.some(
            (pool) =>
              pool.id === binding.pool_id &&
              pool.supplier_id !== props.supplierId
          )
      )
  )
  const channel = channels.find((channel) => channel.id === binding.channel_id)
  const pool = pools.find((pool) => pool.id === binding.pool_id)
  const models =
    channel?.models
      .split(',')
      .map((model) => model.trim())
      .filter((model) => pool?.models.some((spec) => spec.name === model)) ?? []
  const i = 0
  return (
    <div className='grid gap-4 sm:grid-cols-3'>
      <ConfigField
        name={`bindings.${i}.channel_id`}
        type='number'
        label={t('Channel')}
        options={channels.map((c) => ({
          value: c.id,
          label: `${c.name} · #${c.id}`,
        }))}
        onValueChange={() =>
          form.setValue(`bindings.${i}.model`, '', {
            shouldDirty: true,
          })
        }
      />
      <ConfigField
        name={`bindings.${i}.pool_id`}
        type='number'
        label={t('Pool')}
        options={pools.map((p) => ({
          value: p.id,
          label: p.name || `#${p.id}`,
        }))}
        onValueChange={() =>
          form.setValue(`bindings.${i}.model`, '', {
            shouldDirty: true,
          })
        }
      />
      <ConfigField
        name={`bindings.${i}.model`}
        label={t('Model')}
        options={models.map((m) => ({ value: m, label: m }))}
        description={
          !models.length
            ? t('The channel and pool need a matching verified model.')
            : undefined
        }
      />
    </div>
  )
}
