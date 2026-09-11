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
import { useId } from 'react'
import { useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { MultiSelect } from '@/components/multi-select'
import {
  FieldSet,
  FieldLegend,
  FieldDescription,
  Field,
  FieldLabel,
} from '@/components/ui/field'

import type { SupplierRoutingData } from '../api'
import type { SupplierConfigValues } from '../lib/config-schema'
import { supplierModelChannels } from '../lib/resource-schema'

export function ModelChannels(props: {
  data: SupplierRoutingData
  poolIndex: number
  model: string
}) {
  const { t } = useTranslation()
  const id = useId()
  const form = useFormContext<SupplierConfigValues>()
  const pool = useWatch({
    control: form.control,
    name: `pools.${props.poolIndex}`,
  })
  const bindings = pool.bindings ?? []
  const channels = supplierModelChannels(props.data, pool, props.model)
  const selected = bindings
    .filter((binding) => binding.model === props.model)
    .map((binding) => String(binding.channel_id))
  const reasons = {
    unsupported: t(
      'This channel type is not yet supported by supplier routing.'
    ),
    supplier: t('This channel belongs to another supplier.'),
    pool: t('This model is already associated with another resource pool.'),
  }
  return (
    <FieldSet>
      <FieldLegend variant='label'>{t('2. Associate channels')}</FieldLegend>
      <FieldDescription>
        {t(
          'Select one or more channels. All associated channels share this capacity; adding channels does not multiply the limits.'
        )}
      </FieldDescription>
      {props.model ? (
        <Field>
          <FieldLabel className='sr-only' htmlFor={id}>
            {t('Search channels by name or ID')}
          </FieldLabel>
          <MultiSelect
            id={id}
            placeholder={t('Search channels by name or ID')}
            emptyText={t(
              'No matching channels. Check the channel model list or your search.'
            )}
            options={channels.map((channel) => ({
              value: String(channel.id),
              label: `${channel.name} · #${channel.id}`,
              disabled:
                Boolean(channel.reason) &&
                !selected.includes(String(channel.id)),
              description: [
                channel.status !== 1 ? t('Disabled') : '',
                channel.reason ? reasons[channel.reason] : '',
              ]
                .filter(Boolean)
                .join(' · '),
            }))}
            selected={selected}
            maxVisibleChips={3}
            onChange={(values) =>
              form.setValue(
                `pools.${props.poolIndex}.bindings`,
                [
                  ...bindings.filter(
                    (binding) => binding.model !== props.model
                  ),
                  ...values.map((value) => ({
                    channel_id: Number(value),
                    model: props.model,
                  })),
                ],
                { shouldDirty: true }
              )
            }
          />
          <FieldDescription>
            {t('{{selected}} selected · {{total}} matching channels', {
              selected: selected.length,
              total: channels.length,
            })}
          </FieldDescription>
        </Field>
      ) : (
        <FieldDescription>
          {t('Choose a platform model to see its channels.')}
        </FieldDescription>
      )}
    </FieldSet>
  )
}
