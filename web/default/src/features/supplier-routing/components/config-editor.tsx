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
import { Link } from '@tanstack/react-router'
import { isAxiosError } from 'axios'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  sideDrawerContentClassName,
  sideDrawerHeaderClassName,
  sideDrawerFormClassName,
} from '@/components/drawer-layout'
import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from '@/components/ui/sheet'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

import {
  deleteSupplierResource,
  saveSupplierResource,
  getSupplierResourceStatus,
  previewSupplierModels,
  type SupplierResource,
  type SupplierResourceKind,
  type SupplierRoutingData,
} from '../api'
import {
  supplierRoutingMode,
  type SupplierConfigValues,
} from '../lib/config-schema'
import { ResourceEditor, type ResourceSelection } from './resource-editor'
import { ResourcePoolList } from './resource-pool-list'
import { SupplierList } from './supplier-list'

type Workspace = {
  supplierId?: number
  section: 'basic' | 'pool' | 'rules' | 'settings'
  resource?: ResourceSelection
}

export function SupplierConfigEditor(props: {
  view: 'suppliers' | 'pools'
  supplierId?: number
  data: SupplierRoutingData
  canPublish: boolean
  canPreview: boolean
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [workspace, setWorkspace] = useState<Workspace | null>(null)
  const [dirty, setDirty] = useState(false)
  const [navigation, setNavigation] = useState<{
    next: Workspace | null
  } | null>(null)
  const [deleting, setDeleting] = useState<{
    kind: SupplierResourceKind
    resource: SupplierResource
    name: string
  } | null>(null)
  const config: SupplierConfigValues = {
    ...props.data.config,
    suppliers: props.data.config.suppliers ?? [],
    pools: props.data.config.pools ?? [],
    bindings: props.data.config.bindings ?? [],
    rules: props.data.config.rules ?? [],
  }
  const status = useQuery({
    queryKey: ['supplier-status'],
    queryFn: getSupplierResourceStatus,
    refetchInterval: 5000,
  })
  const change = useMutation({
    mutationFn: (value: NonNullable<typeof deleting> & { enabled?: boolean }) =>
      value.enabled === undefined
        ? deleteSupplierResource(value.kind, value.resource)
        : saveSupplierResource({
            kind: value.kind,
            resource: value.resource,
            payload: { enabled: value.enabled },
            idempotencyKey: '',
          }),
    onSuccess: (result, value) => {
      setDeleting(null)
      let message =
        value.enabled === undefined ? t('Record deleted.') : t('Record saved.')
      if (result.application === 'pending') {
        message = t('Saved. Waiting for routing to apply.')
      }
      toast.success(message)
      void queryClient.invalidateQueries({ queryKey: ['supplier-routing'] })
      void queryClient.invalidateQueries({ queryKey: ['supplier-status'] })
    },
    onError: (error) => {
      toast.error(
        isAxiosError(error)
          ? (error.response?.data?.message ?? error.message)
          : error.message
      )
      setDeleting(null)
      void queryClient.invalidateQueries({ queryKey: ['supplier-routing'] })
    },
  })
  const preview = useMutation({ mutationFn: previewSupplierModels })
  const navigate = (next: Workspace | null) => {
    if (dirty) {
      setNavigation({ next })
      return
    }
    setWorkspace(next)
    setDirty(false)
    preview.reset()
  }
  const askDelete = (
    kind: SupplierResourceKind,
    resource: unknown,
    name: string
  ) => setDeleting({ kind, resource: resource as SupplierResource, name })
  const supplier = config.suppliers.find(
    (supplier) => supplier.id === workspace?.supplierId
  )
  const bindings = config.bindings.filter(
    (binding) => binding.pool_id === workspace?.resource?.id
  )
  const modes = {
    legacy: t('Original channel routing'),
    observe: t('Observe without switching traffic'),
    canary: t('Gradual customer rollout'),
    active: t('Dynamic routing for all matched customers'),
  }
  const isSupplier = workspace?.section === 'basic'
  const isPool = workspace?.section === 'pool'
  const busy = change.isPending
  let drawerTitle = t('Routing policies')
  if (isSupplier) drawerTitle = supplier?.name || t('Create supplier')
  if (isPool) {
    drawerTitle =
      workspace?.resource?.id === undefined
        ? t('Add resource pool')
        : t('Resource pool configuration')
  }
  const onSaved = (resource: SupplierResource) => {
    setDirty(false)
    if (!workspace) return
    if (
      workspace.resource?.kind === 'supplier' &&
      workspace.resource.id === undefined
    ) {
      const id = Number(resource.id)
      setWorkspace({
        supplierId: id,
        section: 'basic',
        resource: { kind: 'supplier', id },
      })
    } else if (
      workspace.resource?.kind !== 'supplier' &&
      workspace.resource?.kind !== 'settings'
    ) {
      setWorkspace(isPool ? null : { ...workspace, resource: undefined })
    }
  }
  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>
          {props.view === 'pools' ? t('Resource pools') : t('Suppliers')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <div className='space-y-4'>
            <div className='flex flex-wrap items-center gap-3'>
              <Badge variant='secondary'>
                {modes[supplierRoutingMode(config)]}
              </Badge>
              <span className='text-muted-foreground text-sm'>
                {t('Routing revision')} #
                {status.data?.applied_revision ?? config.revision}
              </span>
              <div className='ml-auto flex gap-2'>
                {props.view === 'suppliers' && (
                  <Button
                    variant='outline'
                    onClick={() =>
                      navigate({
                        section: 'settings',
                        resource: { kind: 'settings' },
                      })
                    }
                  >
                    {t('Routing policies')}
                  </Button>
                )}
                {props.canPublish && (
                  <Button
                    disabled={
                      busy ||
                      (props.view === 'pools' && !config.suppliers.length)
                    }
                    onClick={() =>
                      navigate(
                        props.view === 'pools'
                          ? {
                              section: 'pool',
                              resource: {
                                kind: 'pool',
                                supplierId: props.supplierId,
                              },
                            }
                          : { section: 'basic', resource: { kind: 'supplier' } }
                      )
                    }
                  >
                    {props.view === 'pools'
                      ? t('Add resource pool')
                      : t('Create supplier')}
                  </Button>
                )}
              </div>
            </div>
            {status.data?.application === 'pending' && (
              <Alert>
                <AlertDescription>
                  {t('Saved. Waiting for routing to apply.')} #
                  {status.data.target_revision}
                </AlertDescription>
              </Alert>
            )}
            {status.error && <p role='alert'>{status.error.message}</p>}
            {props.view === 'pools' ? (
              <>
                <p className='text-muted-foreground text-sm'>
                  {t(
                    'Manage shared capacity, model specifications and channel associations for each resource pool.'
                  )}
                </p>
                {!config.suppliers.length && (
                  <Button variant='outline' render={<Link to='/suppliers' />}>
                    {t('Create a supplier first')}
                  </Button>
                )}
                <ResourcePoolList
                  config={config}
                  canPublish={props.canPublish}
                  busy={busy}
                  onEdit={(id) =>
                    navigate({
                      section: 'pool',
                      resource: { kind: 'pool', id },
                    })
                  }
                  onDelete={(id) => {
                    const record = config.pools.find((pool) => pool.id === id)
                    if (record) askDelete('pool', record, record.name)
                  }}
                  onToggle={(id) => {
                    const record = config.pools.find((pool) => pool.id === id)
                    if (record) {
                      change.mutate({
                        kind: 'pool',
                        resource: record as unknown as SupplierResource,
                        name: record.name,
                        enabled: !record.enabled,
                      })
                    }
                  }}
                />
              </>
            ) : (
              <SupplierList
                config={config}
                readOnly={!props.canPublish || busy}
                onEdit={(id) =>
                  navigate({
                    supplierId: id,
                    section: 'basic',
                    resource: { kind: 'supplier', id },
                  })
                }
                onDelete={(id) => {
                  const record = config.suppliers.find((item) => item.id === id)
                  if (record) askDelete('supplier', record, record.name)
                }}
              />
            )}
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>
      <Sheet
        open={workspace !== null}
        onOpenChange={(open) => {
          if (!open) navigate(null)
        }}
      >
        <SheetContent className={sideDrawerContentClassName('sm:max-w-3xl')}>
          <SheetHeader className={sideDrawerHeaderClassName()}>
            <SheetTitle>{drawerTitle}</SheetTitle>
            <SheetDescription>
              {isPool
                ? t(
                    'Configure basic information and shared capacity first, then add models and associated channels. Save the entire resource pool once.'
                  )
                : t(
                    'Manage supplier details and routing policies. Shared capacity is configured on the Resource pools page.'
                  )}
            </SheetDescription>
          </SheetHeader>
          <div className={sideDrawerFormClassName()}>
            {workspace && (
              <>
                {!isSupplier && !isPool && (
                  <Tabs
                    value={workspace.section}
                    onValueChange={(value) => {
                      const section = String(value) as 'settings' | 'rules'
                      navigate({
                        section,
                        resource:
                          section === 'settings'
                            ? { kind: 'settings' }
                            : undefined,
                      })
                    }}
                  >
                    <TabsList variant='line'>
                      <TabsTrigger value='settings'>
                        {t('Traffic routing')}
                      </TabsTrigger>
                      <TabsTrigger value='rules'>
                        {t('Supplier routing rules')}
                      </TabsTrigger>
                    </TabsList>
                  </Tabs>
                )}
                {isSupplier && workspace.supplierId !== undefined && (
                  <Button
                    className='my-3'
                    variant='outline'
                    render={
                      <Link
                        to='/resource-pools'
                        search={{ supplier: [String(workspace.supplierId)] }}
                      />
                    }
                  >
                    {t('View resource pools')}
                  </Button>
                )}
                {workspace.resource &&
                  workspace.resource.kind !== 'supplier' &&
                  workspace.resource.kind !== 'settings' &&
                  !isPool && (
                    <Button
                      className='my-3'
                      variant='ghost'
                      onClick={() =>
                        navigate({ ...workspace, resource: undefined })
                      }
                    >
                      {t('Return to list')}
                    </Button>
                  )}
                {workspace.resource ? (
                  <>
                    <ResourceEditor
                      key={`${workspace.resource.kind}:${workspace.resource.id ?? 'new'}:${workspace.supplierId ?? ''}`}
                      selection={workspace.resource}
                      data={props.data}
                      canPublish={props.canPublish}
                      onDirtyChange={setDirty}
                      onSaved={onSaved}
                    />
                    {isPool &&
                      props.canPreview &&
                      workspace.resource?.id !== undefined && (
                        <details>
                          <summary className='cursor-pointer text-sm'>
                            {t('Preview supplier model declarations')}
                          </summary>
                          <div className='my-3 flex flex-wrap gap-2'>
                            {props.data.channels
                              .filter((channel) =>
                                bindings.some(
                                  (binding) => binding.channel_id === channel.id
                                )
                              )
                              .map((channel) => (
                                <Button
                                  key={channel.id}
                                  variant='outline'
                                  disabled={preview.isPending}
                                  onClick={() => preview.mutate(channel.id)}
                                >
                                  {channel.name}
                                </Button>
                              ))}
                          </div>
                          {preview.error && (
                            <p role='alert'>{preview.error.message}</p>
                          )}
                          {preview.data !== undefined && (
                            <pre className='max-h-80 overflow-auto rounded border p-3 text-xs'>
                              {JSON.stringify(preview.data, null, 2)}
                            </pre>
                          )}
                        </details>
                      )}
                  </>
                ) : (
                  <div className='space-y-4 py-4'>
                    {workspace.section === 'rules' && (
                      <>
                        {config.rules.map((rule) => (
                          <div
                            className='flex flex-wrap items-center justify-between gap-3 rounded-lg border p-4'
                            key={rule.id}
                          >
                            <div>
                              <p className='font-medium'>
                                {rule.model} · {rule.group || t('All groups')}
                              </p>
                              <p className='text-muted-foreground text-sm'>
                                {rule.id}
                              </p>
                            </div>
                            <div className='flex gap-2'>
                              <Button
                                variant='outline'
                                size='sm'
                                onClick={() =>
                                  navigate({
                                    section: 'rules',
                                    resource: { kind: 'rule', id: rule.id },
                                  })
                                }
                              >
                                {t('Edit')}
                              </Button>
                              {props.canPublish && (
                                <Button
                                  variant='ghost'
                                  size='sm'
                                  disabled={busy}
                                  onClick={() =>
                                    askDelete('rule', rule, rule.model)
                                  }
                                >
                                  {t('Delete')}
                                </Button>
                              )}
                            </div>
                          </div>
                        ))}
                        {props.canPublish && (
                          <Button
                            variant='outline'
                            disabled={!config.bindings.length}
                            onClick={() =>
                              navigate({
                                ...workspace,
                                resource: { kind: 'rule' },
                              })
                            }
                          >
                            {t('Add routing rule')}
                          </Button>
                        )}
                        {!config.bindings.length && (
                          <p className='text-muted-foreground text-sm'>
                            {t(
                              'Add suppliers and channel bindings before creating a rule.'
                            )}
                          </p>
                        )}
                      </>
                    )}
                  </div>
                )}
              </>
            )}
          </div>
        </SheetContent>
      </Sheet>
      <ConfirmDialog
        open={navigation !== null}
        onOpenChange={(open) => {
          if (!open) setNavigation(null)
        }}
        title={t('Discard unsaved changes?')}
        desc={t(
          'Only the current record has unsaved changes. Stay here to save it, or discard this draft.'
        )}
        confirmText={t('Discard changes')}
        handleConfirm={() => {
          if (navigation) setWorkspace(navigation.next)
          setNavigation(null)
          setDirty(false)
        }}
      />
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => {
          if (!open) setDeleting(null)
        }}
        title={t('Delete')}
        desc={
          deleting?.kind === 'pool'
            ? t(
                'Delete resource pool {{name}} and remove its channel associations? The channels themselves will be kept.',
                { name: deleting.name }
              )
            : (deleting?.name ?? '')
        }
        destructive
        isLoading={busy}
        confirmText={t('Delete')}
        handleConfirm={() => {
          if (deleting) change.mutate(deleting)
        }}
      />
    </>
  )
}
