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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { useEffect, useRef, useState } from 'react'
import { FormProvider, useForm, type FieldPath } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ZodError } from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { FormNavigationGuard } from '@/features/system-settings/components/form-navigation-guard'

import {
  getSupplierResource,
  getSupplierRevisions,
  restoreSupplierResource,
  saveSupplierResource,
  type SupplierResource,
  type SupplierResourceKind,
  type SupplierResourceResult,
  type SupplierRoutingData,
} from '../api'
import type { SupplierConfigValues } from '../lib/config-schema'
import {
  editableSupplierResource,
  parseSupplierResourceJSON,
  supplierFormResource,
  supplierResourceFormValues,
} from '../lib/resource-schema'
import { BindingFields } from './bindings-editor'
import { PoolFields } from './pools-editor'
import { RoutingControls } from './routing-controls'
import { RuleFields } from './rules-editor'
import { SupplierFields } from './suppliers-editor'

export type ResourceSelection = {
  kind: SupplierResourceKind
  id?: string | number
  supplierId?: number
}
type EditorProps = {
  selection: ResourceSelection
  data: SupplierRoutingData
  canPublish: boolean
  onDirtyChange: (dirty: boolean) => void
  onSaved: (resource: SupplierResource) => void
}

export function ResourceEditor(props: EditorProps) {
  const { t } = useTranslation()
  const detail = useQuery({
    queryKey: ['supplier-resource', props.selection.kind, props.selection.id],
    queryFn: () =>
      getSupplierResource(props.selection.kind, props.selection.id),
    enabled:
      props.selection.id !== undefined || props.selection.kind === 'settings',
    refetchOnWindowFocus: false,
    staleTime: 0,
  })
  if (props.selection.id === undefined && props.selection.kind !== 'settings') {
    return <ResourceForm {...props} />
  }
  if (detail.error) return <p role='alert'>{detail.error.message}</p>
  if (!detail.data) return <p>{t('Loading...')}</p>
  return <ResourceForm {...props} resource={detail.data} />
}

function ResourceForm(props: EditorProps & { resource?: SupplierResource }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const kind = props.selection.kind
  const [base, setBase] = useState(props.resource)
  const form = useForm<SupplierConfigValues>({
    defaultValues: supplierResourceFormValues(
      props.data,
      kind,
      base,
      props.selection.supplierId
    ),
    mode: 'onSubmit',
  })
  const [mode, setMode] = useState<'visual' | 'json'>('visual')
  const [raw, setRaw] = useState<string | null>(null)
  const [rawBlocked, setRawBlocked] = useState(false)
  const [message, setMessage] = useState('')
  const [needsReload, setNeedsReload] = useState(false)
  const [uncertainCreate, setUncertainCreate] = useState(false)
  const [confirmReload, setConfirmReload] = useState(false)
  const [restoreRevision, setRestoreRevision] = useState<number | null>(null)
  const idempotency = useRef({ body: '', key: '' })
  const dirty =
    form.formState.isDirty ||
    (raw !== null &&
      raw !==
        JSON.stringify(supplierFormResource(kind, form.getValues()), null, 2))
  const onDirtyChange = props.onDirtyChange
  useEffect(() => {
    onDirtyChange(dirty)
  }, [dirty, onDirtyChange])
  const prefix = {
    supplier: 'suppliers.0',
    pool: 'pools.0',
    binding: 'bindings.0',
    rule: 'rules.0',
    settings: '',
  }[kind]
  const saved = (result: SupplierResourceResult) => {
    setBase(result.resource)
    form.reset(
      supplierResourceFormValues(
        props.data,
        kind,
        result.resource,
        props.selection.supplierId
      )
    )
    setRaw(null)
    setRawBlocked(false)
    setMessage('')
    setNeedsReload(false)
    setUncertainCreate(false)
    props.onDirtyChange(false)
    toast.success(
      result.application === 'pending'
        ? t('Saved. Waiting for routing to apply.')
        : t('Record saved.')
    )
    void queryClient.invalidateQueries({ queryKey: ['supplier-routing'] })
    void queryClient.invalidateQueries({ queryKey: ['supplier-status'] })
    void queryClient.invalidateQueries({ queryKey: ['supplier-revisions'] })
    props.onSaved(result.resource)
  }
  const failed = (error: Error) => {
    const response = isAxiosError(error) ? error.response : undefined
    const problem = response?.data as
      | {
          message?: string
          field_errors?: Record<string, string>
          code?: string
        }
      | undefined
    setMessage(problem?.message ?? error.message)
    for (const [path, message] of Object.entries(problem?.field_errors ?? {})) {
      if (path) {
        form.setError(
          (prefix
            ? `${prefix}.${path}`
            : path) as FieldPath<SupplierConfigValues>,
          { message }
        )
      }
    }
    if (!response || response.status >= 500 || response.status === 412) {
      if (base) setNeedsReload(true)
      else setUncertainCreate(true)
    } else if (response.status !== 409) {
      // A rejected request has not committed; a corrected create gets a fresh key.
      idempotency.current.body = ''
    }
  }
  const save = useMutation({
    mutationFn: (payload: Record<string, unknown>) => {
      const body = JSON.stringify(payload)
      if (idempotency.current.body !== body) {
        idempotency.current = {
          body,
          key:
            globalThis.crypto?.randomUUID?.() ??
            `${Date.now()}-${Math.random().toString(36).slice(2)}`,
        }
      }
      return saveSupplierResource({
        kind,
        resource: base,
        supplierId: props.selection.supplierId,
        payload,
        idempotencyKey: idempotency.current.key,
      })
    },
    onSuccess: saved,
    onError: failed,
  })
  const restore = useMutation({
    mutationFn: (revision: number) => {
      if (!base) throw new Error('Record not loaded')
      return restoreSupplierResource(kind, base, revision)
    },
    onSuccess: saved,
    onError: failed,
  })
  const reload = useMutation({
    mutationFn: () => getSupplierResource(kind, base?.id),
    onSuccess: (resource) => {
      setBase(resource)
      form.reset(
        supplierResourceFormValues(
          props.data,
          kind,
          resource,
          props.selection.supplierId
        )
      )
      setRaw(null)
      setRawBlocked(false)
      setMessage('')
      setNeedsReload(false)
      setConfirmReload(false)
      props.onDirtyChange(false)
    },
    onError: failed,
  })
  const revisions = useQuery({
    queryKey: ['supplier-revisions', kind, base?.id],
    queryFn: () => getSupplierRevisions(kind, base?.id ?? 'settings'),
    enabled: Boolean(base) && (kind === 'rule' || kind === 'settings'),
  })
  const busy = save.isPending || restore.isPending || reload.isPending
  const publish = () => {
    form.clearErrors()
    setMessage('')
    try {
      const payload = parseSupplierResourceJSON(
        kind,
        raw ?? JSON.stringify(supplierFormResource(kind, form.getValues()))
      )
      const original = base ? editableSupplierResource(kind, base) : undefined
      const changed = original
        ? Object.fromEntries(
            Object.entries(payload).filter(
              ([key, value]) =>
                JSON.stringify(value) !== JSON.stringify(original[key])
            )
          )
        : payload
      save.mutate(changed)
    } catch (error) {
      if (error instanceof ZodError) {
        for (const issue of error.issues) {
          const path = issue.path.join('.')
          if (path) {
            form.setError(
              (prefix
                ? `${prefix}.${path}`
                : path) as FieldPath<SupplierConfigValues>,
              { message: t('Check this value and its allowed range.') }
            )
          }
        }
        setMessage(
          t('Check JSON configuration at {{path}}.', {
            path: error.issues[0]?.path.join('.') || t('Configuration'),
          })
        )
      } else setMessage(t('Enter valid JSON before saving this record.'))
    }
  }
  const switchMode = (next: 'visual' | 'json') => {
    if (next === 'json') {
      if (raw === null) {
        setRaw(
          JSON.stringify(supplierFormResource(kind, form.getValues()), null, 2)
        )
      }
      setMode(next)
      return
    }
    if (raw !== null) {
      try {
        const payload = parseSupplierResourceJSON(kind, raw, true)
        for (const [key, value] of Object.entries(payload)) {
          form.setValue(
            (prefix
              ? `${prefix}.${key}`
              : key) as FieldPath<SupplierConfigValues>,
            value as never,
            { shouldDirty: true }
          )
        }
        setRaw(null)
        setRawBlocked(false)
        setMessage('')
      } catch {
        setRawBlocked(true)
        setMessage(
          t(
            'JSON draft retained. Fix its structure in JSON mode to resume visual editing.'
          )
        )
      }
    }
    setMode(next)
  }
  let saveLabel = t('Save this record')
  if (kind === 'rule') saveLabel = t('Publish this rule')
  if (kind === 'settings') saveLabel = t('Apply routing settings')
  return (
    <FormProvider {...form}>
      <FormNavigationGuard when={dirty} />
      <form
        className='space-y-5'
        onSubmit={(event) => {
          event.preventDefault()
          publish()
        }}
      >
        <p className='text-muted-foreground text-sm'>
          {t(
            'Only this record is saved. Other suppliers, pools and rules are unchanged.'
          )}
        </p>
        <div className='flex gap-2'>
          <Button
            type='button'
            size='sm'
            variant={mode === 'visual' ? 'secondary' : 'ghost'}
            onClick={() => switchMode('visual')}
          >
            {t('Visual configuration')}
          </Button>
          <Button
            type='button'
            size='sm'
            variant={mode === 'json' ? 'secondary' : 'ghost'}
            onClick={() => switchMode('json')}
          >
            {t('Advanced JSON editor')}
          </Button>
        </div>
        {message && (
          <p role='alert' className='text-destructive text-sm'>
            {message}
          </p>
        )}
        {uncertainCreate && (
          <p role='alert'>
            {t(
              'The save result is uncertain. Retry the same request to recover its record without creating a duplicate.'
            )}
          </p>
        )}
        {needsReload && (
          <div className='space-y-2'>
            <p>
              {t(
                'Reload this record before saving again. Your draft is still available to copy.'
              )}
            </p>
            <Button
              type='button'
              variant='outline'
              disabled={busy}
              onClick={() => setConfirmReload(true)}
            >
              {t('Reload record')}
            </Button>
          </div>
        )}
        <fieldset
          disabled={
            !props.canPublish ||
            busy ||
            uncertainCreate ||
            (mode === 'visual' && rawBlocked)
          }
        >
          {mode === 'json' ? (
            <div className='space-y-2'>
              <Label htmlFor='supplier-record-json'>{t('Record JSON')}</Label>
              <Textarea
                id='supplier-record-json'
                className='min-h-96 font-mono text-xs'
                value={raw ?? ''}
                onChange={(event) => setRaw(event.target.value)}
                spellCheck={false}
              />
            </div>
          ) : (
            <>
              {kind === 'supplier' && <SupplierFields index={0} />}
              {kind === 'pool' && <PoolFields index={0} />}
              {kind === 'binding' && (
                <BindingFields
                  channels={props.data.channels}
                  allBindings={props.data.config.bindings ?? []}
                  supplierId={props.selection.supplierId ?? 0}
                />
              )}
              {kind === 'rule' && <RuleFields index={0} />}
              {kind === 'settings' && <RoutingControls />}
            </>
          )}
        </fieldset>
        {props.canPublish && (
          <div className='bg-background sticky bottom-0 border-t py-3'>
            <Button
              type='submit'
              disabled={
                busy ||
                needsReload ||
                (rawBlocked && mode === 'visual') ||
                (!dirty && Boolean(base))
              }
            >
              {busy ? t('Saving...') : saveLabel}
            </Button>
          </div>
        )}
        {base && (kind === 'rule' || kind === 'settings') && (
          <details className='rounded-lg border p-4'>
            <summary className='cursor-pointer text-sm'>
              {t('Routing versions')}
            </summary>
            <p className='text-muted-foreground my-3 text-sm'>
              {t(
                'Restore only this rule or these settings. Current pool limits and bindings remain in place.'
              )}
            </p>
            {revisions.error && <p role='alert'>{revisions.error.message}</p>}
            {revisions.data?.map((revision) => (
              <div
                className='flex items-center justify-between py-2'
                key={revision.id}
              >
                <span>
                  #{revision.id} ·{' '}
                  {new Date(revision.created_at * 1000).toLocaleString()}
                </span>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  disabled={!props.canPublish || dirty || busy || needsReload}
                  onClick={() => setRestoreRevision(revision.id)}
                >
                  {t('Restore')}
                </Button>
              </div>
            ))}
          </details>
        )}
      </form>
      <ConfirmDialog
        open={confirmReload}
        onOpenChange={setConfirmReload}
        title={t('Discard unsaved changes?')}
        desc={t('Reloading replaces this draft with the latest saved record.')}
        confirmText={t('Reload record')}
        isLoading={reload.isPending}
        handleConfirm={() => reload.mutate()}
      />
      <ConfirmDialog
        open={restoreRevision !== null}
        onOpenChange={(open) => {
          if (!open) setRestoreRevision(null)
        }}
        title={t('Restore this record?')}
        desc={t(
          'Restore only this rule or these settings. Current pool limits and bindings remain in place.'
        )}
        confirmText={t('Restore')}
        handleConfirm={() => {
          if (restoreRevision !== null) restore.mutate(restoreRevision)
          setRestoreRevision(null)
        }}
      />
    </FormProvider>
  )
}
