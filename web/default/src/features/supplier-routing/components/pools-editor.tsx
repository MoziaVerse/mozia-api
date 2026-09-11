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
import { FieldSet, FieldLegend } from '@/components/ui/field'

import {
  newSupplierModel,
  type SupplierConfigValues,
} from '../lib/config-schema'
import { ConfigField, ConfigSwitch } from './config-fields'

function PoolModels(props: { poolIndex: number; poolId: number }) {
  const { t } = useTranslation()
  const form = useFormContext<SupplierConfigValues>()
  const list = useFieldArray({
    control: form.control,
    name: `pools.${props.poolIndex}.models`,
    keyName: 'formKey',
  })
  const [models, bindings] = useWatch({
    control: form.control,
    name: [`pools.${props.poolIndex}.models`, 'bindings'],
  })
  return (
    <FieldSet>
      <FieldLegend>{t('Verified models')}</FieldLegend>
      {list.fields.map((item, i) => {
        const path = `pools.${props.poolIndex}.models.${i}` as const
        const used = bindings.some(
          (b) => b.pool_id === props.poolId && b.model === models[i]?.name
        )
        return (
          <div className='space-y-4 rounded-lg border p-4' key={item.formKey}>
            <div className='flex items-center justify-between gap-2'>
              <span className='text-sm font-medium'>
                {models[i]?.name || t('New model')}
              </span>
              <Button
                type='button'
                variant='ghost'
                size='sm'
                disabled={list.fields.length === 1 || used}
                onClick={() => list.remove(i)}
              >
                {t('Delete')}
              </Button>
            </div>
            <div className='grid gap-4 sm:grid-cols-2'>
              <ConfigField name={`${path}.name`} label={t('Model name')} />
              <ConfigField
                name={`${path}.version`}
                label={t('Verified model version')}
              />
              <ConfigField
                name={`${path}.context_tokens`}
                type='number'
                min={1}
                max={10000000}
                label={t('Context token limit')}
              />
              <ConfigField
                name={`${path}.max_output_tokens`}
                type='number'
                min={1}
                max={10000000}
                label={t('Maximum output tokens')}
              />
              <ConfigSwitch
                name={`${path}.tools`}
                label={t('Tool calls supported')}
              />
              <ConfigSwitch
                name={`${path}.json`}
                label={t('Structured JSON output supported')}
              />
            </div>
            <details>
              <summary className='cursor-pointer text-sm'>
                {t('Per-model capacity limits')}
              </summary>
              <p className='text-muted-foreground my-3 text-sm'>
                {t(
                  'Zero inherits the shared pool limit. Model limits cannot exceed the pool limits.'
                )}
              </p>
              <div className='grid gap-4 sm:grid-cols-3'>
                <ConfigField
                  name={`${path}.limits.concurrency`}
                  type='number'
                  min={0}
                  max={10000}
                  label={t('Concurrent requests')}
                />
                <ConfigField
                  name={`${path}.limits.rpm`}
                  type='number'
                  min={0}
                  max={100000}
                  label='RPM'
                />
                <ConfigField
                  name={`${path}.limits.tpm`}
                  type='number'
                  min={0}
                  max={1000000000}
                  label='TPM'
                />
              </div>
            </details>
          </div>
        )
      })}
      <Button
        type='button'
        variant='outline'
        disabled={list.fields.length >= 128}
        onClick={() => list.append(newSupplierModel())}
      >
        {t('Add verified model')}
      </Button>
    </FieldSet>
  )
}

export function PoolsEditor({ supplierId }: { supplierId: number }) {
  const { t } = useTranslation()
  const form = useFormContext<SupplierConfigValues>()
  const list = useFieldArray({
    control: form.control,
    name: 'pools',
    keyName: 'formKey',
  })
  const [pools, bindings] = useWatch({
    control: form.control,
    name: ['pools', 'bindings'],
  })
  return (
    <div className='space-y-4'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'A resource pool represents shared capacity in a supplier cluster. Channels and keys using that cluster share the same limits.'
        )}
      </p>
      {list.fields.map((item, i) => {
        if (pools[i]?.supplier_id !== supplierId) return null
        const used = bindings.some((b) => b.pool_id === item.id)
        return (
          <Card key={item.formKey}>
            <CardHeader className='flex-row items-center justify-between gap-3'>
              <CardTitle>
                {pools[i]?.name || t('New resource pool')} · #{item.id}
              </CardTitle>
              <Button
                type='button'
                variant='ghost'
                size='sm'
                disabled={used}
                title={
                  used
                    ? t('Remove references before deleting this item.')
                    : undefined
                }
                onClick={() => list.remove(i)}
              >
                {t('Delete')}
              </Button>
            </CardHeader>
            <CardContent className='space-y-5'>
              <div className='grid gap-4 sm:grid-cols-2'>
                <ConfigField name={`pools.${i}.name`} label={t('Pool name')} />
                <ConfigField
                  name={`pools.${i}.failure_domain`}
                  label={t('Failure domain')}
                  description={t(
                    'Use the same identifier for pools affected by the same datacenter outage.'
                  )}
                />
                <ConfigField
                  name={`pools.${i}.acceptance`}
                  label={t('Acceptance or load-test reference')}
                  description={t(
                    'Required before enabling a pool. Enter the reference to your verified capacity report.'
                  )}
                />
              </div>
              <FieldSet>
                <FieldLegend>{t('Shared capacity limits')}</FieldLegend>
                <div className='grid gap-4 sm:grid-cols-3'>
                  <ConfigField
                    name={`pools.${i}.limits.concurrency`}
                    type='number'
                    min={1}
                    max={10000}
                    label={t('Concurrent requests')}
                  />
                  <ConfigField
                    name={`pools.${i}.limits.rpm`}
                    type='number'
                    min={1}
                    max={100000}
                    label='RPM'
                    description={t('Request starts in the last 60 seconds.')}
                  />
                  <ConfigField
                    name={`pools.${i}.limits.tpm`}
                    type='number'
                    min={1}
                    max={1000000000}
                    label='TPM'
                    description={t(
                      'Input and output token budget reserved for requests started in the last 60 seconds.'
                    )}
                  />
                </div>
              </FieldSet>
              <div className='grid gap-4 sm:grid-cols-2'>
                <ConfigField
                  name={`pools.${i}.max_execution_seconds`}
                  type='number'
                  min={1}
                  max={3600}
                  label={t('Maximum upstream execution (seconds)')}
                  description={t(
                    'Use the timeout guaranteed by the supplier, including queue time.'
                  )}
                />
                <ConfigField
                  name={`pools.${i}.input_safety_percent`}
                  type='number'
                  min={100}
                  max={200}
                  label={t('Input token safety margin (%)')}
                />
              </div>
              <PoolModels poolIndex={i} poolId={item.id} />
              <ConfigSwitch
                name={`pools.${i}.enabled`}
                label={t('Pool available')}
                description={t(
                  'Enable only after verifying the model specifications and capacity limits.'
                )}
              />
            </CardContent>
          </Card>
        )
      })}
      <Button
        type='button'
        variant='outline'
        disabled={list.fields.length >= 128}
        onClick={() =>
          list.append({
            id: Math.max(0, ...pools.map((p) => p.id)) + 1,
            supplier_id: supplierId,
            name: '',
            failure_domain: '',
            enabled: false,
            limits: { concurrency: 10, rpm: 100, tpm: 100000 },
            max_execution_seconds: 120,
            input_safety_percent: 110,
            acceptance: '',
            models: [newSupplierModel()],
          })
        }
      >
        {t('Add resource pool')}
      </Button>
    </div>
  )
}
