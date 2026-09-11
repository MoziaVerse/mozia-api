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
  newSupplierRule,
  type SupplierConfigValues,
} from '../lib/config-schema'
import { ConfigField } from './config-fields'

function RuleTargets(props: { index: number }) {
  const { t } = useTranslation()
  const form = useFormContext<SupplierConfigValues>()
  const list = useFieldArray({
    control: form.control,
    name: `rules.${props.index}.targets`,
    keyName: 'formKey',
  })
  const [suppliers, targets, mode] = useWatch({
    control: form.control,
    name: [
      'suppliers',
      `rules.${props.index}.targets`,
      `rules.${props.index}.mode`,
    ],
  })
  const total = targets.reduce(
    (sum, target) => sum + (Number.isFinite(target.weight) ? target.weight : 0),
    0
  )
  const nextSupplier = suppliers.find(
    (s) => !targets.some((target) => target.supplier_id === s.id)
  )
  return (
    <FieldSet>
      <FieldLegend>{t('Target suppliers')}</FieldLegend>
      {list.fields.map((item, i) => (
        <div key={item.formKey} className='flex flex-wrap items-start gap-3'>
          <div className='min-w-48 flex-1'>
            <ConfigField
              name={`rules.${props.index}.targets.${i}.supplier_id`}
              type='number'
              label={t('Supplier')}
              options={suppliers.map((s) => ({
                value: s.id,
                label: s.name || `#${s.id}`,
              }))}
            />
          </div>
          <div className='w-36'>
            <ConfigField
              name={`rules.${props.index}.targets.${i}.weight`}
              type='number'
              min={1}
              max={10000}
              label={t('Weight')}
              description={
                mode === 'share' && total > 0
                  ? t('Target share: {{percent}}%', {
                      percent: (
                        ((targets[i]?.weight || 0) * 100) /
                        total
                      ).toFixed(1),
                    })
                  : undefined
              }
            />
          </div>
          <Button
            type='button'
            size='sm'
            variant='ghost'
            className='mt-6'
            disabled={list.fields.length === 1}
            onClick={() => list.remove(i)}
          >
            {t('Delete')}
          </Button>
        </div>
      ))}
      <Button
        type='button'
        variant='outline'
        disabled={list.fields.length >= 32 || !nextSupplier}
        onClick={() => {
            if (nextSupplier) {
              list.append({ supplier_id: nextSupplier.id, weight: 100 })
            }
        }}
      >
        {t('Add target supplier')}
      </Button>
    </FieldSet>
  )
}

export function RulesEditor() {
  const { t } = useTranslation()
  const form = useFormContext<SupplierConfigValues>()
  const list = useFieldArray({
    control: form.control,
    name: 'rules',
    keyName: 'formKey',
  })
  const [rules, suppliers, bindings] = useWatch({
    control: form.control,
    name: ['rules', 'suppliers', 'bindings'],
  })
  const models = [...new Set(bindings.map((b) => b.model).filter(Boolean))]
  const descriptions = {
    capacity: t(
      'Prefer pools with lower shared capacity usage within the eligible priority tier.'
    ),
    share: t(
      'Distribute first requests by supplier weights. For example, 50 / 30 / 20 targets 50% / 30% / 20% when capacity and health permit.'
    ),
    failover: t(
      'Use existing channel priorities for primary and backup routing. Lower priorities take over when higher priorities are unavailable.'
    ),
  }
  return (
    <div className='space-y-4'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Rules match model, group and customer. Customer rules take precedence over group rules, followed by model rules.'
        )}
      </p>
      {list.fields.map((item, i) => (
        <Card key={item.formKey}>
          <CardHeader className='flex-row items-center justify-between'>
            <CardTitle>{rules[i]?.id || t('New routing rule')}</CardTitle>
            <Button
              type='button'
              variant='ghost'
              size='sm'
              onClick={() => list.remove(i)}
            >
              {t('Delete')}
            </Button>
          </CardHeader>
          <CardContent className='space-y-5'>
            <div className='grid gap-4 sm:grid-cols-2'>
              <ConfigField name={`rules.${i}.id`} label={t('Rule name')} />
              <ConfigField
                name={`rules.${i}.model`}
                label={t('Model')}
                options={models.map((m) => ({ value: m, label: m }))}
              />
              <ConfigField
                name={`rules.${i}.group`}
                label={t('Group')}
                description={t('Leave blank to match all groups.')}
              />
              <ConfigField
                name={`rules.${i}.user_id`}
                type='number'
                min={0}
                label={t('Customer ID')}
                description={t('Zero matches all customers.')}
              />
            </div>
            <ConfigField
              name={`rules.${i}.mode`}
              label={t('Routing strategy')}
              options={[
                { value: 'capacity', label: t('Capacity balancing') },
                { value: 'share', label: t('Supplier target shares') },
                { value: 'failover', label: t('Primary and backup') },
              ]}
              description={descriptions[rules[i]?.mode ?? 'capacity']}
            />
            <RuleTargets index={i} />
            <div className='grid gap-4 sm:grid-cols-3'>
              <ConfigField
                name={`rules.${i}.max_attempts`}
                type='number'
                min={1}
                max={10}
                label={t('Maximum attempts')}
                description={t('Includes the first attempt and all retries.')}
              />
              <ConfigField
                name={`rules.${i}.timeout_seconds`}
                type='number'
                min={1}
                max={3600}
                label={t('Total timeout (seconds)')}
              />
              <ConfigField
                name={`rules.${i}.max_supplier_percent`}
                type='number'
                min={0}
                max={100}
                label={t('Supplier concentration cap (%)')}
                description={t(
                  'Zero adds no cap. A strict cap can reject requests when alternatives are unavailable.'
                )}
              />
            </div>
            <details>
              <summary className='cursor-pointer text-sm font-medium'>
                {t('Health and recovery thresholds')}
              </summary>
              <div className='mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>
                <ConfigField
                  name={`rules.${i}.health.window_seconds`}
                  type='number'
                  min={10}
                  max={3600}
                  label={t('Health window (seconds)')}
                />
                <ConfigField
                  name={`rules.${i}.health.min_samples`}
                  type='number'
                  min={1}
                  max={10000}
                  label={t('Minimum samples')}
                />
                <ConfigField
                  name={`rules.${i}.health.failure_percent`}
                  type='number'
                  min={1}
                  max={100}
                  label={t('Failure threshold (%)')}
                />
                <ConfigField
                  name={`rules.${i}.health.max_ttft_ms`}
                  type='number'
                  min={1}
                  label={t('First-token threshold (ms)')}
                />
                <ConfigField
                  name={`rules.${i}.health.cooldown_seconds`}
                  type='number'
                  min={1}
                  max={3600}
                  label={t('Cooldown (seconds)')}
                />
                <ConfigField
                  name={`rules.${i}.health.trial_percent`}
                  type='number'
                  min={1}
                  max={100}
                  label={t('Recovery trial percentage')}
                />
              </div>
            </details>
          </CardContent>
        </Card>
      ))}
      <Button
        type='button'
        variant='outline'
        disabled={
          !suppliers.length || !models.length || list.fields.length >= 128
        }
        onClick={() => {
          let n = rules.length + 1
          while (rules.some((r) => r.id === `rule-${n}`)) n++
          list.append(newSupplierRule(`rule-${n}`, suppliers[0].id, models[0]))
        }}
      >
        {t('Add routing rule')}
      </Button>
      {(!suppliers.length || !models.length) && (
        <p className='text-muted-foreground text-sm'>
          {t('Add suppliers and channel bindings before creating a rule.')}
        </p>
      )}
    </div>
  )
}
