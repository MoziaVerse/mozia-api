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
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { FormProvider, useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ZodError } from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  sideDrawerContentClassName,
  sideDrawerHeaderClassName,
  sideDrawerFormClassName,
  sideDrawerFooterClassName,
} from '@/components/drawer-layout'
import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
  SheetFooter,
} from '@/components/ui/sheet'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { FormNavigationGuard } from '@/features/system-settings/components/form-navigation-guard'

import {
  previewSupplierModels,
  rollbackSupplierRouting,
  saveSupplierRouting,
  type SupplierRoutingData,
  type SupplierConfig,
} from '../api'
import {
  parseSupplierConfigJSON,
  parseSupplierDraftJSON,
  supplierConfigSchema,
  type SupplierConfigValues,
} from '../lib/config-schema'
import { BindingsEditor } from './bindings-editor'
import { PoolsEditor } from './pools-editor'
import { RoutingControls } from './routing-controls'
import { RulesEditor } from './rules-editor'
import { SupplierList } from './supplier-list'
import { SupplierFields } from './suppliers-editor'

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
    reValidateMode: 'onSubmit',
    defaultValues: {
      ...config,
      suppliers: config.suppliers ?? [],
      pools: config.pools ?? [],
      bindings: config.bindings ?? [],
      rules: config.rules ?? [],
    },
  })
  const [suppliers, pools, bindings] = useWatch({
    control: form.control,
    name: ['suppliers', 'pools', 'bindings'],
  })
  const [editor, setEditor] = useState('visual')
  const [jsonDraft, setJSONDraft] = useState<string | null>(null)
  const [jsonError, setJSONError] = useState('')
  const [supplierId, setSupplierId] = useState<number | null>(null)
  const [supplierTab, setSupplierTab] = useState('basic')
  const [routingTab, setRoutingTab] = useState<string | null>(null)
  const [discard, setDiscard] = useState(false)
  const [deleteId, setDeleteId] = useState<number | null>(null)
  const preview = useMutation({ mutationFn: previewSupplierModels })
  const save = useMutation({
    mutationFn: (next: SupplierConfig) => saveSupplierRouting(next),
    onSuccess: (next) => {
      toast.success(t('Supplier routing published'))
      form.reset({
        ...next,
        suppliers: next.suppliers ?? [],
        pools: next.pools ?? [],
        bindings: next.bindings ?? [],
        rules: next.rules ?? [],
      })
      setJSONDraft(null)
      setJSONError('')
      setSupplierId(null)
      setRoutingTab(null)
      void queryClient.invalidateQueries({ queryKey: ['supplier-routing'] })
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
  const dirty = form.formState.isDirty || jsonDraft !== null
  const readOnly = !props.canPublish || save.isPending || rollback.isPending
  const visualReadOnly = readOnly || jsonDraft !== null
  const supplierIndex = suppliers.findIndex(
    (supplier) => supplier.id === supplierId
  )
  const selectedSupplier = suppliers[supplierIndex]
  const editSupplier = (id: number) => {
    setSupplierId(id)
    setSupplierTab('basic')
    preview.reset()
  }
  const publish = () => {
    if (readOnly) return
    form.clearErrors()
    setJSONError('')
    if (jsonDraft !== null) {
      try {
        save.mutate({
          ...parseSupplierConfigJSON(jsonDraft),
          revision: config.revision,
        })
      } catch (error) {
        setJSONError(
          error instanceof ZodError
            ? t('Check JSON configuration at {{path}}.', {
                path: error.issues[0].path.join('.') || t('Configuration'),
              })
            : t('Enter valid JSON before publishing.')
        )
        setEditor('json')
      }
      return
    }
    void form.handleSubmit(
      (values) => save.mutate({ ...values, revision: config.revision }),
      (errors) => {
        const values = form.getValues()
        const invalidSupplier = values.suppliers.findIndex(
          (_s, i) => errors.suppliers?.[i]
        )
        const invalidPool = values.pools.findIndex((_p, i) => errors.pools?.[i])
        const invalidBinding = values.bindings.findIndex(
          (_b, i) => errors.bindings?.[i]
        )
        let owner: number | undefined
        let tab = 'basic'
        if (invalidSupplier >= 0) owner = values.suppliers[invalidSupplier].id
        else if (invalidPool >= 0) {
          owner = values.pools[invalidPool].supplier_id
          tab = 'capacity'
        } else if (invalidBinding >= 0) {
          owner = values.pools.find(
            (pool) => pool.id === values.bindings[invalidBinding].pool_id
          )?.supplier_id
          tab = 'bindings'
        }
        if (
          owner !== undefined &&
          values.suppliers.some((supplier) => supplier.id === owner)
        ) {
          setRoutingTab(null)
          setSupplierId(owner)
          setSupplierTab(tab)
        } else if (
          errors.rules ||
          errors.enabled ||
          errors.shadow ||
          errors.canary_percent
        ) {
          setSupplierId(null)
          setRoutingTab(errors.rules ? 'rules' : 'controls')
        } else {
          setSupplierId(null)
          setRoutingTab(null)
          setEditor('json')
        }
        form.setError('root', {
          message: t('Review the highlighted fields before publishing.'),
        })
      }
    )()
  }
  const errorNotice = form.formState.errors.root && (
    <p role='alert' className='text-destructive text-sm'>
      {form.formState.errors.root.message}
    </p>
  )
  return (
    <FormProvider {...form}>
      <FormNavigationGuard when={dirty} />
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Suppliers')}</SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button variant='outline' render={<Link to='/supplier-monitor' />}>
            {t('Open supplier monitoring')}
          </Button>
          <Button variant='outline' onClick={() => setRoutingTab('controls')}>
            {t('Routing policies')}
          </Button>
          <Button
            disabled={
              visualReadOnly || editor === 'json' || suppliers.length >= 128
            }
            onClick={() => {
              const id =
                Math.max(0, ...suppliers.map((supplier) => supplier.id)) + 1
              form.setValue(
                'suppliers',
                [
                  ...suppliers,
                  {
                    id,
                    name: '',
                    enabled: false,
                    region: '',
                    contact: '',
                    terms: '',
                    data_policy: '',
                  },
                ],
                { shouldDirty: true }
              )
              editSupplier(id)
            }}
          >
            {t('Create supplier')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='space-y-5'>
            <p className='text-muted-foreground text-sm'>
              {t(
                'Manage partner suppliers here. Open a supplier to configure its capacity, models and channels.'
              )}
            </p>
            <div className='flex flex-wrap items-center gap-3 rounded-lg border p-3'>
              <Badge variant={dirty ? 'outline' : 'secondary'}>
                {dirty ? t('Unpublished changes') : t('Published')}
              </Badge>
              <span className='text-muted-foreground flex-1 text-sm'>
                {t(
                  'Closing a drawer keeps this page draft. Save and publish applies all current changes.'
                )}
              </span>
              {dirty && (
                <Button
                  variant='ghost'
                  disabled={readOnly}
                  onClick={() => setDiscard(true)}
                >
                  {t('Discard changes')}
                </Button>
              )}
              {props.canPublish && (
                <Button disabled={readOnly || !dirty} onClick={publish}>
                  {save.isPending ? t('Saving...') : t('Save and publish')}
                </Button>
              )}
            </div>
            {errorNotice}
            <Tabs
              value={editor}
              onValueChange={(value) => {
                if (value === 'visual' && jsonDraft !== null) {
                  try {
                    const next = parseSupplierDraftJSON(jsonDraft)
                    form.reset(
                      { ...next, revision: config.revision },
                      { keepDefaultValues: true }
                    )
                    setJSONDraft(null)
                  } catch {
                    /* Preserve raw JSON until the user publishes or discards it. */
                  }
                }
                setJSONError('')
                setEditor(String(value))
              }}
            >
              <TabsList>
                <TabsTrigger value='visual'>
                  {t('Visual configuration')}
                </TabsTrigger>
                <TabsTrigger value='json'>{t('Advanced JSON')}</TabsTrigger>
              </TabsList>
              <TabsContent value='visual' className='space-y-4'>
                {jsonDraft !== null && (
                  <Alert>
                    <AlertDescription>
                      {t(
                        'JSON draft preserved. This list shows the last displayable draft; edit or discard JSON to resume visual editing.'
                      )}
                      <div className='mt-2 flex gap-2'>
                        <Button
                          variant='outline'
                          size='sm'
                          onClick={() => setEditor('json')}
                        >
                          {t('Advanced JSON')}
                        </Button>
                        <Button
                          variant='outline'
                          size='sm'
                          disabled={readOnly}
                          onClick={() => {
                            setJSONDraft(null)
                            setJSONError('')
                          }}
                        >
                          {t('Discard JSON edits')}
                        </Button>
                      </div>
                    </AlertDescription>
                  </Alert>
                )}
                <SupplierList
                  readOnly={visualReadOnly}
                  onEdit={editSupplier}
                  onDelete={setDeleteId}
                />
              </TabsContent>
              <TabsContent value='json' className='space-y-3'>
                <Label htmlFor='supplier-config-json'>
                  {t('Complete routing configuration')}
                </Label>
                <p className='text-muted-foreground text-sm'>
                  {t(
                    'Switch editors freely while drafting. Configuration is validated when you save and publish.'
                  )}
                </p>
                <Textarea
                  id='supplier-config-json'
                  rows={24}
                  spellCheck={false}
                  className='font-mono text-xs'
                  value={jsonDraft ?? JSON.stringify(form.getValues(), null, 2)}
                  readOnly={readOnly}
                  onChange={(event) => setJSONDraft(event.target.value)}
                  aria-invalid={Boolean(jsonError)}
                />
                {jsonError && (
                  <p role='alert' className='text-destructive text-sm'>
                    {jsonError}
                  </p>
                )}
              </TabsContent>
            </Tabs>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>
      <Sheet
        open={supplierIndex >= 0}
        onOpenChange={(open) => {
          if (!open) setSupplierId(null)
        }}
      >
        <SheetContent className={sideDrawerContentClassName('sm:max-w-3xl')}>
          <SheetHeader className={sideDrawerHeaderClassName()}>
            <SheetTitle>
              {selectedSupplier?.name || t('New supplier')}
            </SheetTitle>
            <SheetDescription>
              {t(
                'Closing a drawer keeps this page draft. Save and publish applies all current changes.'
              )}
            </SheetDescription>
          </SheetHeader>
          <div className={sideDrawerFormClassName()}>
            {errorNotice}
            {selectedSupplier && (
              <Tabs
                value={supplierTab}
                onValueChange={(value) => setSupplierTab(String(value))}
              >
                <TabsList variant='line' className='max-w-full flex-wrap'>
                  <TabsTrigger value='basic'>
                    {t('Basic information')}
                  </TabsTrigger>
                  <TabsTrigger value='capacity'>
                    {t('Capacity and models')}
                  </TabsTrigger>
                  <TabsTrigger value='bindings'>
                    {t('Channel bindings')}
                  </TabsTrigger>
                </TabsList>
                <TabsContent value='basic'>
                  <fieldset disabled={visualReadOnly}>
                    <SupplierFields index={supplierIndex} />
                  </fieldset>
                </TabsContent>
                <TabsContent value='capacity'>
                  <fieldset disabled={visualReadOnly}>
                    <PoolsEditor
                      key={selectedSupplier.id}
                      supplierId={selectedSupplier.id}
                    />
                  </fieldset>
                </TabsContent>
                <TabsContent value='bindings' className='space-y-5'>
                  <fieldset disabled={visualReadOnly}>
                    <BindingsEditor
                      key={selectedSupplier.id}
                      supplierId={selectedSupplier.id}
                      channels={props.data.channels}
                    />
                  </fieldset>
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
                          .filter((channel) =>
                            bindings.some(
                              (binding) =>
                                binding.channel_id === channel.id &&
                                pools.some(
                                  (pool) =>
                                    pool.id === binding.pool_id &&
                                    pool.supplier_id === selectedSupplier.id
                                )
                            )
                          )
                          .map((channel) => (
                            <Button
                              key={channel.id}
                              variant='outline'
                              size='sm'
                              disabled={preview.isPending}
                              onClick={() => preview.mutate(channel.id)}
                            >
                              {channel.name} · {channel.id}
                            </Button>
                          ))}
                      </div>
                      {preview.error && (
                        <p role='alert'>{preview.error.message}</p>
                      )}
                      {preview.data !== undefined && (
                        <pre className='mt-2 max-h-80 overflow-auto rounded border p-3 text-xs'>
                          {JSON.stringify(preview.data, null, 2)}
                        </pre>
                      )}
                    </details>
                  )}
                </TabsContent>
              </Tabs>
            )}
          </div>
          <SheetFooter className={sideDrawerFooterClassName()}>
            <Button variant='outline' onClick={() => setSupplierId(null)}>
              {t('Return to list')}
            </Button>
            {props.canPublish && (
              <Button disabled={visualReadOnly || !dirty} onClick={publish}>
                {t('Save and publish')}
              </Button>
            )}
          </SheetFooter>
        </SheetContent>
      </Sheet>
      <Sheet
        open={routingTab !== null}
        onOpenChange={(open) => {
          if (!open) setRoutingTab(null)
        }}
      >
        <SheetContent className={sideDrawerContentClassName('sm:max-w-3xl')}>
          <SheetHeader className={sideDrawerHeaderClassName()}>
            <SheetTitle>{t('Routing policies')}</SheetTitle>
            <SheetDescription>
              {t(
                'Choose how platform traffic reaches suppliers. Save and publish applies all current changes.'
              )}
            </SheetDescription>
          </SheetHeader>
          <div className={sideDrawerFormClassName()}>
            {errorNotice}
            <Tabs
              value={routingTab ?? 'controls'}
              onValueChange={(value) => setRoutingTab(String(value))}
            >
              <TabsList variant='line'>
                <TabsTrigger value='controls'>
                  {t('Traffic routing')}
                </TabsTrigger>
                <TabsTrigger value='rules'>
                  {t('Supplier routing rules')}
                </TabsTrigger>
                <TabsTrigger value='versions'>
                  {t('Routing versions')}
                </TabsTrigger>
              </TabsList>
              <TabsContent value='controls'>
                <fieldset disabled={visualReadOnly}>
                  <RoutingControls />
                </fieldset>
              </TabsContent>
              <TabsContent value='rules'>
                <fieldset disabled={visualReadOnly}>
                  <RulesEditor />
                </fieldset>
              </TabsContent>
              <TabsContent value='versions' className='space-y-4'>
                <p className='text-muted-foreground text-sm'>
                  {t(
                    'Rollback preserves current resource limits and in-flight reservations.'
                  )}
                </p>
                {dirty && (
                  <p className='text-muted-foreground text-sm'>
                    {t(
                      'Publish or discard the draft before restoring a version.'
                    )}
                  </p>
                )}
                <div className='flex flex-wrap gap-2'>
                  {props.data.revisions.map((revision) => (
                    <Button
                      key={revision.id}
                      variant='outline'
                      size='sm'
                      disabled={
                        readOnly || dirty || revision.id === config.revision
                      }
                      onClick={() => rollback.mutate(revision.id)}
                    >
                      {t('Restore routing version')} {revision.id}
                    </Button>
                  ))}
                </div>
              </TabsContent>
            </Tabs>
          </div>
          <SheetFooter className={sideDrawerFooterClassName()}>
            <Button variant='outline' onClick={() => setRoutingTab(null)}>
              {t('Return to list')}
            </Button>
            {props.canPublish && (
              <Button disabled={visualReadOnly || !dirty} onClick={publish}>
                {t('Save and publish')}
              </Button>
            )}
          </SheetFooter>
        </SheetContent>
      </Sheet>
      <ConfirmDialog
        open={discard}
        onOpenChange={setDiscard}
        title={t('Discard changes')}
        desc={t(
          'Discard all unpublished changes and return to the loaded configuration?'
        )}
        destructive
        handleConfirm={() => {
          form.reset()
          setJSONDraft(null)
          setJSONError('')
          setDiscard(false)
        }}
      />
      <ConfirmDialog
        open={deleteId !== null}
        onOpenChange={(open) => {
          if (!open) setDeleteId(null)
        }}
        title={t('Delete supplier')}
        desc={t('The supplier will be removed when you save and publish.')}
        destructive
        handleConfirm={() => {
          form.setValue(
            'suppliers',
            suppliers.filter((supplier) => supplier.id !== deleteId),
            { shouldDirty: true }
          )
          setDeleteId(null)
        }}
      />
    </FormProvider>
  )
}
