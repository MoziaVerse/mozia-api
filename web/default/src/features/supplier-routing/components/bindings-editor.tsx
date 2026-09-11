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
import { useId, useState } from 'react'
import { useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import {
  FieldSet,
  FieldLegend,
  FieldDescription,
  FieldGroup,
  Field,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'

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
  const [search, setSearch] = useState('')
  const form = useFormContext<SupplierConfigValues>()
  const pool = useWatch({
    control: form.control,
    name: `pools.${props.poolIndex}`,
  })
  const bindings = pool.bindings ?? []
  const channels = supplierModelChannels(props.data, pool, props.model)
  const visible = channels.filter((c) =>
    `${c.name} #${c.id}`.toLowerCase().includes(search.trim().toLowerCase())
  )
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
        <>
          <Field>
            <FieldLabel className='sr-only' htmlFor={`${id}-search`}>
              {t('Search channels by name or ID')}
            </FieldLabel>
            <Input
              id={`${id}-search`}
              placeholder={t('Search channels by name or ID')}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
          </Field>
          <FieldDescription>
            {t('{{selected}} selected · {{total}} matching channels', {
              selected: bindings.filter((b) => b.model === props.model).length,
              total: channels.length,
            })}
          </FieldDescription>
          <FieldGroup className='max-h-60 gap-0 overflow-y-auto rounded-lg border'>
            {visible.map((channel) => {
              const checked = bindings.some(
                (b) => b.model === props.model && b.channel_id === channel.id
              )
              return (
                <Field
                  key={channel.id}
                  orientation='horizontal'
                  className='items-start border-b p-3 last:border-b-0'
                  data-disabled={Boolean(channel.reason)}
                >
                  <Checkbox
                    id={`${id}-${channel.id}`}
                    checked={checked}
                    disabled={Boolean(channel.reason) && !checked}
                    onCheckedChange={(selected) => {
                      const remaining = bindings.filter(
                        (b) =>
                          b.model !== props.model || b.channel_id !== channel.id
                      )
                      form.setValue(
                        `pools.${props.poolIndex}.bindings`,
                        selected
                          ? [
                              ...remaining,
                              { channel_id: channel.id, model: props.model },
                            ]
                          : remaining,
                        { shouldDirty: true }
                      )
                    }}
                  />
                  <div className='min-w-0 flex-1'>
                    <FieldLabel
                      htmlFor={`${id}-${channel.id}`}
                      className='flex flex-wrap gap-2 font-normal'
                    >
                      {channel.name} · #{channel.id}
                      {channel.status !== 1 && (
                        <Badge variant='outline'>{t('Disabled')}</Badge>
                      )}
                    </FieldLabel>
                    {channel.reason && (
                      <FieldDescription>
                        {reasons[channel.reason]}
                      </FieldDescription>
                    )}
                  </div>
                </Field>
              )
            })}
            {!visible.length && (
              <p className='text-muted-foreground p-3 text-sm'>
                {t(
                  'No matching channels. Check the channel model list or your search.'
                )}
              </p>
            )}
          </FieldGroup>
        </>
      ) : (
        <FieldDescription>
          {t('Choose a platform model to see its channels.')}
        </FieldDescription>
      )}
    </FieldSet>
  )
}
