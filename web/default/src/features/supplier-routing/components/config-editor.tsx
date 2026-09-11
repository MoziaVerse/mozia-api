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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { FormProvider, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ZodError } from 'zod'

import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'

import {
  previewSupplierModels,
  rollbackSupplierRouting,
  saveSupplierRouting,
  type SupplierRoutingData,
  type SupplierConfig,
} from '../api'
import {
  parseSupplierConfigJSON,
  supplierConfigSchema,
  type SupplierConfigValues,
} from '../lib/config-schema'
import { BindingsEditor } from './bindings-editor'
import { PoolsEditor } from './pools-editor'
import { RoutingControls } from './routing-controls'
import { RulesEditor } from './rules-editor'
import { SuppliersEditor } from './suppliers-editor'

export function SupplierConfigEditor(props: {
  data: SupplierRoutingData
  canPublish: boolean
  canPreview: boolean
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const config = props.data.config
  const form = useForm<SupplierConfigValues>({
    resolver: zodResolver(supplierConfigSchema, undefined, { raw: true }),
    defaultValues: {
      ...config,
      suppliers: config.suppliers ?? [],
      pools: config.pools ?? [],
      bindings: config.bindings ?? [],
      rules: config.rules ?? [],
    },
  })
  const [editor, setEditor] = useState<'visual' | 'json'>('visual')
  const [section, setSection] = useState('suppliers')
  const [json, setJSON] = useState('')
  const [jsonError, setJSONError] = useState('')
  const preview = useMutation({ mutationFn: previewSupplierModels })
  const save = useMutation({
    mutationFn: (input: { config: SupplierConfig; validate: boolean }) =>
      saveSupplierRouting(input.config, input.validate),
    onSuccess: (_data, input) => {
      toast.success(
        input.validate
          ? t('Configuration is valid')
          : t('Supplier routing published')
      )
      form.clearErrors('root')
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
  const readOnly = !props.canPublish || save.isPending || rollback.isPending
  const readJSON = (): SupplierConfigValues | null => {
    try {
      const next = parseSupplierConfigJSON(json)
      setJSONError('')
      return { ...next, revision: config.revision }
    } catch (error) {
      setJSONError(
        error instanceof ZodError
          ? t('Check JSON configuration at {{path}}.', {
              path: error.issues[0].path.join('.') || t('Configuration'),
            })
          : t('Enter valid JSON before switching editors or publishing.')
      )
      return null
    }
  }
  const submit = (validate: boolean) => {
    if (readOnly) return
    if (editor === 'json') {
      const next = readJSON()
      if (next) save.mutate({ config: next, validate })
      return
    }
    void form.handleSubmit(
      (values) =>
        save.mutate({
          config: { ...values, revision: config.revision },
          validate,
        }),
      (errors) => {
        const first = (
          ['suppliers', 'pools', 'bindings', 'rules'] as const
        ).find((name) => errors[name])
        if (first) setSection(first)
        form.setError('root', {
          message: t('Review the highlighted fields before publishing.'),
        })
      }
    )()
  }
  return (
    <FormProvider {...form}>
      <form
        onSubmit={(event) => {
          event.preventDefault()
          submit(false)
        }}
        className='space-y-6'
      >
        <div className='space-y-6'>
          <Tabs
            value={editor}
            onValueChange={(value) => {
              if (value === 'json') {
                setJSON(JSON.stringify(form.getValues(), null, 2))
                setJSONError('')
                setEditor('json')
                return
              }
              const next = readJSON()
              if (next) {
                form.reset(next, { keepDefaultValues: true })
                setEditor('visual')
              }
            }}
          >
            <TabsList>
              <TabsTrigger value='visual'>
                {t('Visual configuration')}
              </TabsTrigger>
              <TabsTrigger value='json'>{t('Advanced JSON')}</TabsTrigger>
            </TabsList>
            <TabsContent value='visual' className='space-y-6'>
              <fieldset disabled={readOnly}>
                <RoutingControls />
              </fieldset>
              <Tabs
                value={section}
                onValueChange={(value) => setSection(String(value))}
              >
                <TabsList
                  variant='line'
                  className='h-auto max-w-full flex-wrap justify-start'
                >
                  <TabsTrigger value='suppliers'>{t('Suppliers')}</TabsTrigger>
                  <TabsTrigger value='pools'>
                    {t('Shared resource pools')}
                  </TabsTrigger>
                  <TabsTrigger value='bindings'>
                    {t('Channel and model bindings')}
                  </TabsTrigger>
                  <TabsTrigger value='rules'>
                    {t('Supplier routing rules')}
                  </TabsTrigger>
                </TabsList>
                <TabsContent value='suppliers'>
                  <fieldset disabled={readOnly}>
                    <SuppliersEditor />
                  </fieldset>
                </TabsContent>
                <TabsContent value='pools'>
                  <fieldset disabled={readOnly}>
                    <PoolsEditor />
                  </fieldset>
                </TabsContent>
                <TabsContent value='bindings'>
                  <fieldset disabled={readOnly}>
                    <BindingsEditor channels={props.data.channels} />
                  </fieldset>
                </TabsContent>
                <TabsContent value='rules'>
                  <fieldset disabled={readOnly}>
                    <RulesEditor />
                  </fieldset>
                </TabsContent>
              </Tabs>
            </TabsContent>
            <TabsContent value='json' className='space-y-3'>
              <Label htmlFor='supplier-config-json'>
                {t('Complete routing configuration')}
              </Label>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Visual and JSON editors share one configuration. Changes are applied only after publishing. Invalid JSON must be fixed or discarded before returning to the form.'
                )}
              </p>
              <Textarea
                id='supplier-config-json'
                rows={24}
                spellCheck={false}
                className='font-mono text-xs'
                value={json}
                readOnly={readOnly}
                onChange={(event) => setJSON(event.target.value)}
                aria-invalid={Boolean(jsonError)}
              />
              {jsonError && (
                <p role='alert' className='text-destructive text-sm'>
                  {jsonError}
                </p>
              )}
              <Button
                type='button'
                variant='outline'
                onClick={() => {
                  setJSONError('')
                  setEditor('visual')
                }}
              >
                {t('Discard JSON edits')}
              </Button>
            </TabsContent>
          </Tabs>
          <div className='flex flex-wrap items-center gap-3'>
            <Button
              type='button'
              variant='outline'
              disabled={readOnly}
              onClick={() => submit(true)}
            >
              {t('Validate configuration')}
            </Button>
            <Button type='submit' disabled={readOnly}>
              {save.isPending ? t('Saving...') : t('Publish supplier routing')}
            </Button>
            <span className='text-muted-foreground text-xs'>
              {t('Changes take effect only after publishing.')}
            </span>
          </div>
        </div>
        {form.formState.errors.root && (
          <p role='alert' className='text-destructive text-sm'>
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
                  rollback.isPending ||
                  save.isPending
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
    </FormProvider>
  )
}
