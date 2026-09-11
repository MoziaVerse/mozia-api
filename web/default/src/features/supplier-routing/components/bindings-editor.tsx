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
import { useFieldArray, useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

import type { SupplierRoutingData } from '../api'
import type { SupplierConfigValues } from '../lib/config-schema'
import { ConfigField } from './config-fields'

export function BindingsEditor(props: {
  channels: SupplierRoutingData['channels']
  supplierId: number
}) {
  const { t } = useTranslation()
  const form = useFormContext<SupplierConfigValues>()
  const list = useFieldArray({
    control: form.control,
    name: 'bindings',
    keyName: 'formKey',
  })
  const [allPools, bindings] = useWatch({
    control: form.control,
    name: ['pools', 'bindings'],
  })
  const pools = allPools.filter((pool) => pool.supplier_id === props.supplierId)
  const channels = props.channels.filter(
    (channel) =>
      channel.type === 1 &&
      !bindings.some(
        (binding) =>
          binding.channel_id === channel.id &&
          allPools.some(
            (pool) =>
              pool.id === binding.pool_id &&
              pool.supplier_id !== props.supplierId
          )
      )
  )
  return (
    <div className='space-y-4'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Bind an existing OpenAI-compatible channel and model to its resource pool. Each channel/model pair belongs to one pool.'
        )}
      </p>
      {list.fields.map((item, i) => {
        const binding = bindings[i]
        if (!pools.some((pool) => pool.id === binding?.pool_id)) return null
        const channel = channels.find((c) => c.id === binding?.channel_id)
        const pool = pools.find((p) => p.id === binding?.pool_id)
        const models =
          channel?.models
            .split(',')
            .map((m) => m.trim())
            .filter((m) => pool?.models.some((spec) => spec.name === m)) ?? []
        return (
          <Card key={item.formKey}>
            <CardHeader className='flex-row items-center justify-between'>
              <CardTitle>
                {t('Channel and model binding')} {i + 1}
              </CardTitle>
              <Button
                type='button'
                variant='ghost'
                size='sm'
                onClick={() => list.remove(i)}
              >
                {t('Delete')}
              </Button>
            </CardHeader>
            <CardContent className='grid gap-4 sm:grid-cols-3'>
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
            </CardContent>
          </Card>
        )
      })}
      <Button
        type='button'
        variant='outline'
        disabled={
          !pools.length || !channels.length || list.fields.length >= 256
        }
        onClick={() =>
          list.append({
            channel_id: channels[0].id,
            pool_id: pools[0].id,
            model: '',
          })
        }
      >
        {t('Add channel binding')}
      </Button>
      {(!pools.length || !channels.length) && (
        <p className='text-muted-foreground text-sm'>
          {t('Create a resource pool and an OpenAI-compatible channel first.')}
        </p>
      )}
    </div>
  )
}
