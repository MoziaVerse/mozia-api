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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { hasPermission } from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

import { SettingsSection } from '../components/settings-section'
import {
  getSupplierRouting,
  previewSupplierModels,
  getSupplierStats,
  getSupplierAttempts,
  saveSupplierRouting,
  rollbackSupplierRouting,
  reconcileSupplierAttempt,
  type SupplierRoutingData,
  type SupplierConfig,
  type SupplierAttempt,
} from './supplier-routing-api'

const arrayJSON = z.string().refine((value) => {
  try {
    return Array.isArray(JSON.parse(value))
  } catch {
    return false
  }
}, 'Enter a valid JSON array')

const schema = z.object({
  enabled: z.boolean(),
  shadow: z.boolean(),
  canary_percent: z.number().min(0).max(100),
  suppliers: arrayJSON,
  pools: arrayJSON,
  bindings: arrayJSON,
  rules: arrayJSON,
})

function SupplierConfigForm(props: {
  data: SupplierRoutingData
  canPublish: boolean
  canPreview: boolean
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const preview = useMutation({ mutationFn: previewSupplierModels })
  const config = props.data.config
  const fieldLabels = {
    suppliers: t('Suppliers'),
    pools: t('Shared resource pools'),
    bindings: t('Channel and model bindings'),
    rules: t('Supplier routing rules'),
  }
  const form = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema),
    defaultValues: {
      enabled: config.enabled,
      shadow: config.shadow,
      canary_percent: config.canary_percent,
      suppliers: JSON.stringify(config.suppliers ?? [], null, 2),
      pools: JSON.stringify(config.pools ?? [], null, 2),
      bindings: JSON.stringify(config.bindings ?? [], null, 2),
      rules: JSON.stringify(config.rules ?? [], null, 2),
    },
  })
  const save = useMutation({
    mutationFn: (input: { config: SupplierConfig; validate: boolean }) =>
      saveSupplierRouting(input.config, input.validate),
    onSuccess: (_data, input) => {
      toast.success(
        input.validate
          ? t('Configuration is valid')
          : t('Supplier routing published')
      )
      if (!input.validate) {
        void queryClient.invalidateQueries({ queryKey: ['supplier-routing'] })
      }
    },
    onError: (error) => form.setError('root', { message: error.message }),
  })
  const rollback = useMutation({
    mutationFn: (id: number) => rollbackSupplierRouting(config.revision, id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['supplier-routing'] })
    },
    onError: (error) => form.setError('root', { message: error.message }),
  })
  const submit = (validate: boolean) =>
    form.handleSubmit((values) => {
      const next = {
        ...values,
        revision: config.revision,
        suppliers: JSON.parse(values.suppliers),
        pools: JSON.parse(values.pools),
        bindings: JSON.parse(values.bindings),
        rules: JSON.parse(values.rules),
      } as SupplierConfig
      save.mutate({ config: next, validate })
    })
  const example = () => {
    const channel = props.data.channels.find((item) => item.type === 1)
    const model = channel?.models.split(',')[0] || 'model-name'
    const examples = {
      suppliers: [
        {
          id: 1,
          name: 'Supplier A',
          region: '',
          contact: '',
          data_policy: '',
          terms: '',
          enabled: false,
        },
      ],
      pools: [
        {
          id: 1,
          supplier_id: 1,
          name: 'Pool A',
          failure_domain: 'datacenter-a',
          enabled: false,
          limits: { concurrency: 10, rpm: 100, tpm: 100000 },
          max_execution_seconds: 120,
          input_safety_percent: 110,
          acceptance: '',
          models: [
            {
              name: model,
              version: 'verified-version',
              context_tokens: 32000,
              max_output_tokens: 4096,
              tools: false,
              json: false,
              limits: { concurrency: 0, rpm: 0, tpm: 0 },
            },
          ],
        },
      ],
      bindings: [{ channel_id: channel?.id ?? 0, model, pool_id: 1 }],
      rules: [
        {
          id: 'default',
          model,
          group: '',
          user_id: 0,
          mode: 'capacity',
          targets: [{ supplier_id: 1, weight: 100 }],
          max_attempts: 2,
          timeout_seconds: 120,
          health: {
            window_seconds: 60,
            min_samples: 10,
            failure_percent: 20,
            max_ttft_ms: 5000,
            cooldown_seconds: 30,
            trial_percent: 10,
          },
        },
      ],
    }
    for (const field of Object.keys(examples) as (keyof typeof examples)[]) {
      form.setValue(field, JSON.stringify(examples[field], null, 2), {
        shouldDirty: true,
      })
    }
  }

  return (
    <form onSubmit={submit(false)} className='space-y-4'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Shared capacity remains enforced when dynamic routing is disabled. Publish only verified supplier limits.'
        )}
      </p>
      <fieldset
        disabled={!props.canPublish || save.isPending || rollback.isPending}
        className='space-y-4'
      >
        <div className='flex flex-wrap items-center gap-5'>
          <label className='flex items-center gap-2'>
            <input type='checkbox' {...form.register('enabled')} />
            {t('Enable dynamic supplier routing')}
          </label>
          <label className='flex items-center gap-2'>
            <input type='checkbox' {...form.register('shadow')} />
            {t('Observe routing recommendations only')}
          </label>
          <Label htmlFor='supplier-canary'>
            {t('Customer rollout percentage')}
          </Label>
          <Input
            id='supplier-canary'
            type='number'
            min={0}
            max={100}
            className='w-24'
            {...form.register('canary_percent', { valueAsNumber: true })}
          />
        </div>
        <Button type='button' variant='outline' onClick={example}>
          {t('Load disabled configuration example')}
        </Button>
        {(['suppliers', 'pools', 'bindings', 'rules'] as const).map((field) => (
          <div key={field} className='space-y-2'>
            <Label htmlFor={`supplier-${field}`}>{fieldLabels[field]}</Label>
            <Textarea
              id={`supplier-${field}`}
              rows={field === 'pools' ? 15 : 8}
              className='font-mono text-xs'
              {...form.register(field)}
              aria-invalid={Boolean(form.formState.errors[field])}
            />
            {form.formState.errors[field] && (
              <p role='alert' className='text-destructive text-sm'>
                {t('Enter a valid JSON array')}
              </p>
            )}
          </div>
        ))}
        <p className='text-muted-foreground text-sm'>
          {t(
            'Modes: capacity, share, failover. Targets use supplier IDs; bindings use existing channel IDs. Zero model limits inherit the pool limits.'
          )}
        </p>
        <div className='flex gap-2'>
          <Button type='button' variant='outline' onClick={submit(true)}>
            {t('Validate configuration')}
          </Button>
          <Button type='submit'>{t('Publish supplier routing')}</Button>
        </div>
      </fieldset>
      {form.formState.errors.root && (
        <p role='alert' className='text-destructive'>
          {form.formState.errors.root.message}
        </p>
      )}
      {props.canPreview && (
        <details>
          <summary className='cursor-pointer text-sm'>
            {t('Preview supplier model declarations')}
          </summary>
          <p className='text-muted-foreground my-2 text-sm'>
            {t(
              'Declarations are proposals. Confirm model versions, capabilities and load-test results before publishing.'
            )}
          </p>
          <div className='flex flex-wrap gap-2'>
            {props.data.channels
              .filter((channel) => channel.type === 1)
              .map((channel) => (
                <Button
                  key={channel.id}
                  type='button'
                  variant='outline'
                  size='sm'
                  disabled={preview.isPending}
                  onClick={() => preview.mutate(channel.id)}
                >
                  {channel.name} · {channel.id}
                </Button>
              ))}
          </div>
          {preview.error && <p role='alert'>{preview.error.message}</p>}
          {preview.data !== undefined && (
            <pre className='mt-2 max-h-80 overflow-auto rounded border p-3 text-xs'>
              {JSON.stringify(preview.data, null, 2)}
            </pre>
          )}
        </details>
      )}
      <details>
        <summary className='cursor-pointer text-sm'>
          {t('Routing versions')} · {config.revision}
        </summary>
        <div className='mt-2 flex flex-wrap gap-2'>
          {props.data.revisions.map((revision) => (
            <Button
              key={revision.id}
              type='button'
              variant='outline'
              size='sm'
              disabled={
                !props.canPublish ||
                revision.id === config.revision ||
                rollback.isPending
              }
              onClick={() => rollback.mutate(revision.id)}
            >
              {t('Restore routing version')} {revision.id}
            </Button>
          ))}
        </div>
        <p className='text-muted-foreground mt-2 text-sm'>
          {t(
            'Rollback preserves current resource limits and in-flight reservations.'
          )}
        </p>
      </details>
    </form>
  )
}

function SupplierReconciliation(props: {
  attempt: SupplierAttempt
  onClose: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const form = useForm({
    defaultValues: {
      status: 'failed',
      cost: '',
      currency: props.attempt.currency || 'CNY',
      note: '',
    },
  })
  const save = useMutation({
    mutationFn: (values: {
      status: string
      cost: string
      currency: string
      note: string
    }) =>
      reconcileSupplierAttempt(
        props.attempt.id,
        values.status,
        values.cost,
        values.currency,
        values.note
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: ['supplier-routing-attempts'],
      })
      props.onClose()
    },
    onError: (error) => form.setError('root', { message: error.message }),
  })
  return (
    <form
      className='flex flex-wrap items-end gap-3 rounded-md border p-3'
      onSubmit={form.handleSubmit((values) => save.mutate(values))}
    >
      <Label>
        {t('Verified call result')}
        <select
          className='bg-background ml-2 rounded border p-2'
          {...form.register('status')}
        >
          <option value='success'>{t('Success')}</option>
          <option value='failed'>{t('Failed')}</option>
          <option value='cancelled'>{t('Not sent')}</option>
        </select>
      </Label>
      <Label>
        {t('Procurement cost')}
        <Input
          {...form.register('cost', { required: true })}
          inputMode='decimal'
        />
      </Label>
      <Label>
        {t('Currency')}
        <select
          className='bg-background ml-2 rounded border p-2'
          {...form.register('currency')}
        >
          <option value='CNY'>CNY</option>
          <option value='USD'>USD</option>
        </select>
      </Label>
      <Label>
        {t('Reconciliation evidence')}
        <Input {...form.register('note', { required: true })} />
      </Label>
      <Button type='submit' disabled={save.isPending}>
        {t('Save reconciliation')}
      </Button>
      <Button type='button' variant='outline' onClick={props.onClose}>
        {t('Cancel')}
      </Button>
      {form.formState.errors.root && (
        <p role='alert' className='text-destructive w-full'>
          {form.formState.errors.root.message}
        </p>
      )}
    </form>
  )
}

export function SupplierRoutingSection() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const canRead = hasPermission(user, 'channel', 'read')
  const canPublish = hasPermission(user, 'supplier_routing', 'publish')
  const canReadCost = hasPermission(user, 'model_pricing', 'read')
  const canWriteCost = hasPermission(user, 'model_pricing', 'write')
  const [reconcile, setReconcile] = useState<SupplierAttempt | null>(null)
  const config = useQuery({
    queryKey: ['supplier-routing'],
    queryFn: getSupplierRouting,
    enabled: canRead,
    refetchOnWindowFocus: false,
  })
  const stats = useQuery({
    queryKey: ['supplier-routing-stats'],
    queryFn: getSupplierStats,
    enabled: canRead,
    refetchInterval: 30000,
  })
  const attempts = useQuery({
    queryKey: ['supplier-routing-attempts'],
    queryFn: getSupplierAttempts,
    enabled: canReadCost,
    refetchInterval: 30000,
  })
  if (!canRead) return null
  return (
    <SettingsSection title={t('Supplier routing')}>
      {config.isPending && <p>{t('Loading...')}</p>}
      {config.error && <p role='alert'>{config.error.message}</p>}
      {config.data && (
        <SupplierConfigForm
          key={config.data.config.revision}
          data={config.data}
          canPublish={canPublish}
          canPreview={hasPermission(user, 'channel', 'operate')}
        />
      )}
      <h4 className='font-medium'>{t('Supplier traffic this hour')}</h4>
      {stats.error && <p role='alert'>{stats.error.message}</p>}
      {stats.data && (
        <p className='text-muted-foreground text-sm'>
          {t('Requests')}: {stats.data.summary.requests} ·{' '}
          {t('First-attempt successes')}: {stats.data.summary.first_success} ·{' '}
          {t('Final successes')}: {stats.data.summary.final_success} ·{' '}
          {t('Unresolved requests')}: {stats.data.summary.unresolved}
        </p>
      )}
      <div className='overflow-x-auto'>
        <Table>
          <TableHeader>
            <TableRow>
              {[
                t('Supplier'),
                t('Pool'),
                t('Model'),
                t('Group'),
                t('Attempt type'),
                t('Status'),
                t('Requests'),
                t('TTFT (ms)'),
                t('First-dispatch share'),
                t('Health'),
                t('Priority fallback'),
              ].map((label) => (
                <TableHead key={label}>{label}</TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {stats.data?.rows?.map((row) => (
              <TableRow
                key={`${row.pool_id}:${row.model}:${row.group_name}:${row.kind}:${row.status}:${row.priority_fallback}:${row.outcome_class}`}
              >
                <TableCell>{row.supplier_id}</TableCell>
                <TableCell>{row.pool_id}</TableCell>
                <TableCell>{row.model}</TableCell>
                <TableCell>{row.group_name}</TableCell>
                <TableCell>{row.kind}</TableCell>
                <TableCell>
                  {row.status} / {row.outcome_class}
                </TableCell>
                <TableCell>{row.requests}</TableCell>
                <TableCell>{Math.round(row.avg_ttft_ms)}</TableCell>
                <TableCell>
                  {row.kind === 'first'
                    ? `${row.first_share_percent.toFixed(1)}%`
                    : '—'}
                </TableCell>
                <TableCell>
                  {row.health_state}{' '}
                  {row.health_scale > 0 && `${row.health_scale}%`}
                </TableCell>
                <TableCell>
                  {row.priority_fallback ? t('Yes') : t('No')}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      {canReadCost && (
        <>
          <h4 className='font-medium'>
            {t('Recent supplier calls and procurement costs')}
          </h4>
          {attempts.error && <p role='alert'>{attempts.error.message}</p>}
          {reconcile && (
            <SupplierReconciliation
              key={reconcile.id}
              attempt={reconcile}
              onClose={() => setReconcile(null)}
            />
          )}
          <div className='overflow-x-auto'>
            <Table>
              <TableHeader>
                <TableRow>
                  {[
                    t('Request'),
                    t('Channel'),
                    t('Routing reason'),
                    t('Status'),
                    t('Input tokens'),
                    t('Output tokens'),
                    t('Procurement cost'),
                    t('Reconciliation'),
                  ].map((label) => (
                    <TableHead key={label}>{label}</TableHead>
                  ))}
                </TableRow>
              </TableHeader>
              <TableBody>
                {attempts.data?.map((attempt) => (
                  <TableRow key={attempt.id}>
                    <TableCell title={attempt.request_id}>
                      {attempt.request_id.slice(-10)} / {attempt.attempt}
                    </TableCell>
                    <TableCell>{attempt.channel_id}</TableCell>
                    <TableCell>{attempt.reason}</TableCell>
                    <TableCell>{attempt.status}</TableCell>
                    <TableCell>{attempt.input_tokens}</TableCell>
                    <TableCell>{attempt.output_tokens}</TableCell>
                    <TableCell>
                      {attempt.cost || t('Pending reconciliation')}{' '}
                      {attempt.currency}
                    </TableCell>
                    <TableCell>
                      {canWriteCost && attempt.cost_status === 'pending' ? (
                        <Button
                          type='button'
                          variant='outline'
                          size='sm'
                          onClick={() => setReconcile(attempt)}
                        >
                          {t('Reconcile')}
                        </Button>
                      ) : (
                        attempt.cost_status
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </>
      )}
    </SettingsSection>
  )
}
