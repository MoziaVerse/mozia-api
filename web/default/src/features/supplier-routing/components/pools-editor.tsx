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
import {
  Controller,
  useFieldArray,
  useFormContext,
  useWatch,
} from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import {
  Field,
  FieldLabel,
  FieldDescription,
  FieldGroup,
  FieldSet,
  FieldLegend,
} from '@/components/ui/field'

import type { SupplierRoutingData } from '../api'
import {
  newSupplierModel,
  type SupplierConfigValues,
} from '../lib/config-schema'
import { ModelChannels } from './bindings-editor'
import { ConfigField, ConfigSwitch } from './config-fields'

function PoolModels(props: { poolIndex: number; data: SupplierRoutingData }) {
  const { t } = useTranslation()
  const form = useFormContext<SupplierConfigValues>()
  const id = useId()
  const list = useFieldArray({
    control: form.control,
    name: `pools.${props.poolIndex}.models`,
    keyName: 'formKey',
  })
  const [models, bindings] = useWatch({
    control: form.control,
    name: [
      `pools.${props.poolIndex}.models`,
      `pools.${props.poolIndex}.bindings`,
    ],
  })
  const platformModels = [
    ...new Set(
      props.data.channels.flatMap((channel) =>
        channel.models
          .split(',')
          .map((name) => name.trim())
          .filter(Boolean)
      )
    ),
  ].sort()
  return (
    <FieldSet>
      <FieldLegend>{t('Model information')}</FieldLegend>
      {list.fields.map((item, i) => {
        const path = `pools.${props.poolIndex}.models.${i}` as const
        const options = platformModels
          .filter(
            (name) =>
              !models.some((model, index) => index !== i && model.name === name)
          )
          .map((name) => ({ value: name, label: name }))
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
                disabled={list.fields.length === 1}
                onClick={() => {
                  form.setValue(
                    `pools.${props.poolIndex}.bindings`,
                    (bindings ?? []).filter((b) => b.model !== models[i]?.name),
                    { shouldDirty: true }
                  )
                  list.remove(i)
                }}
              >
                {t('Delete')}
              </Button>
            </div>
            <Field
              data-invalid={Boolean(
                form.getFieldState(`${path}.name`, form.formState).error
              )}
            >
              <FieldLabel htmlFor={`${id}-${i}`}>
                {t('1. Choose a platform model')}
              </FieldLabel>
              <Controller
                control={form.control}
                name={`${path}.name`}
                render={({ field }) => (
                  <Combobox
                    id={`${id}-${i}`}
                    options={options}
                    value={field.value}
                    placeholder={t('Search models from existing channels')}
                    allowCustomValue={false}
                    onValueChange={(name) => {
                      if ((name ?? '') === field.value) return
                      form.setValue(
                        `pools.${props.poolIndex}.bindings`,
                        (bindings ?? []).filter((b) => b.model !== field.value),
                        { shouldDirty: true }
                      )
                      field.onChange(name ?? '')
                    }}
                  />
                )}
              />
              {form.getFieldState(`${path}.name`, form.formState).error && (
                <FieldDescription>
                  {t('Check this value and its allowed range.')}
                </FieldDescription>
              )}
            </Field>
            <ModelChannels
              key={models[i]?.name}
              data={props.data}
              poolIndex={props.poolIndex}
              model={models[i]?.name ?? ''}
            />
            <FieldSet>
              <FieldLegend variant='label'>
                {t('3. Verified specifications')}
              </FieldLegend>
              <div className='grid gap-4 sm:grid-cols-2'>
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
            </FieldSet>
          </div>
        )
      })}
      <Button
        type='button'
        variant='outline'
        disabled={list.fields.length >= 128}
        onClick={() => list.append(newSupplierModel())}
      >
        {t('Add another model to this shared capacity')}
      </Button>
    </FieldSet>
  )
}

export function PoolFields(props: {
  index: number
  data: SupplierRoutingData
  isNew: boolean
}) {
  const { t } = useTranslation()
  const i = props.index
  const form = useFormContext<SupplierConfigValues>()
  const supplierId = useWatch({
    control: form.control,
    name: `pools.${i}.supplier_id`,
  })
  return (
    <FieldGroup>
      <FieldSet>
        <FieldLegend>{t('Basic information')}</FieldLegend>
        {props.isNew ? (
          <ConfigField
            name={`pools.${i}.supplier_id`}
            type='number'
            label={t('Supplier')}
            options={(props.data.config.suppliers ?? []).map((supplier) => ({
              value: supplier.id,
              label: `${supplier.name} · #${supplier.id}`,
            }))}
            onValueChange={() =>
              form.setValue(`pools.${i}.bindings`, [], { shouldDirty: true })
            }
          />
        ) : (
          <FieldDescription>
            {t('Supplier')}:{' '}
            {props.data.config.suppliers?.find(
              (supplier) => supplier.id === supplierId
            )?.name ?? `#${supplierId}`}
          </FieldDescription>
        )}
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
      </FieldSet>
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
      <PoolModels poolIndex={i} data={props.data} />
      <ConfigSwitch
        name={`pools.${i}.enabled`}
        label={t('Pool available')}
        description={t(
          'Enable only after verifying the model specifications and capacity limits.'
        )}
      />
    </FieldGroup>
  )
}
