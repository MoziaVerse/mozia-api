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
import {
  RotateCcw,
  Save,
  Search,
  ShieldCheck,
  UserRoundCog,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { SettingsSection } from '@/features/system-settings/components/settings-section'
import {
  ADMIN_ROLE_KEY,
  normalizeAdminPermissions,
  roleGrants,
  type AdminPermissionMatrix,
  type PermissionCatalog,
} from '@/lib/admin-permissions'
import { cn } from '@/lib/utils'

import {
  getManagedAdmins,
  getOperationsPermissionCatalog,
  saveManagedAdminPermissions,
} from './api'
import type { ManagedAdmin } from './types'

const permissionsQueryKey = ['mozia', 'operations-permissions'] as const
const managedAdminsQueryKey = ['mozia', 'managed-admins'] as const

function PermissionEditor(props: {
  admin: ManagedAdmin
  catalog: PermissionCatalog
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const initialPermissions = normalizeAdminPermissions(
    props.admin.permissions,
    props.catalog
  )
  const [permissions, setPermissions] =
    useState<AdminPermissionMatrix>(initialPermissions)
  const isDirty =
    JSON.stringify(permissions) !== JSON.stringify(initialPermissions)

  const saveMutation = useMutation({
    mutationFn: () => saveManagedAdminPermissions(props.admin.id, permissions),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: managedAdminsQueryKey })
      toast.success(t('Operating permissions saved'))
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to save operating permissions'))
    },
  })

  const setPermission = (
    resource: string,
    action: string,
    allowed: boolean
  ) => {
    setPermissions((current) => ({
      ...current,
      [resource]: {
        ...current[resource],
        [action]: allowed,
      },
    }))
  }

  const resetToDefaults = () => {
    setPermissions(roleGrants(props.catalog, ADMIN_ROLE_KEY))
  }

  return (
    <div className='flex min-w-0 flex-col gap-4'>
      <div className='bg-muted/30 flex flex-col gap-3 rounded-xl border p-4 sm:flex-row sm:items-center sm:justify-between'>
        <div className='min-w-0'>
          <div className='flex flex-wrap items-center gap-2'>
            <h3 className='truncate text-base font-semibold'>
              {props.admin.display_name || props.admin.username}
            </h3>
            <Badge variant='outline'>@{props.admin.username}</Badge>
            {props.admin.status !== 1 && (
              <Badge variant='destructive'>{t('Disabled')}</Badge>
            )}
          </div>
        </div>
        <div className='flex flex-wrap gap-2'>
          <Button
            type='button'
            variant='outline'
            onClick={resetToDefaults}
            disabled={saveMutation.isPending}
          >
            <RotateCcw aria-hidden='true' />
            {t('Restore admin defaults')}
          </Button>
          <Button
            type='button'
            onClick={() => saveMutation.mutate()}
            disabled={!isDirty || saveMutation.isPending}
          >
            <Save aria-hidden='true' />
            {saveMutation.isPending ? t('Saving...') : t('Save permissions')}
          </Button>
        </div>
      </div>

      <div className='grid gap-3 xl:grid-cols-2'>
        {props.catalog.resources.map((resource) => (
          <Card key={resource.resource} size='sm' className='gap-3'>
            <CardHeader className='border-b'>
              <div className='flex items-center gap-2'>
                <ShieldCheck
                  className='text-primary size-4'
                  aria-hidden='true'
                />
                <span className='font-medium'>{t(resource.label_key)}</span>
              </div>
            </CardHeader>
            <CardContent className='space-y-1'>
              {resource.actions.map((action) => {
                const checked =
                  permissions[resource.resource]?.[action.action] === true
                return (
                  <label
                    key={action.action}
                    className={cn(
                      'flex cursor-pointer items-start gap-3 rounded-lg px-2 py-2.5 transition-colors hover:bg-muted/60',
                      checked && 'bg-primary/5'
                    )}
                  >
                    <Checkbox
                      checked={checked}
                      onCheckedChange={(value) =>
                        setPermission(
                          resource.resource,
                          action.action,
                          value === true
                        )
                      }
                      aria-label={t(action.label_key)}
                    />
                    <span className='min-w-0 flex-1'>
                      <span className='flex flex-wrap items-center gap-2 text-sm font-medium'>
                        {t(action.label_key)}
                      </span>
                      <span className='text-muted-foreground mt-0.5 block text-xs leading-relaxed'>
                        {t(action.description_key)}
                      </span>
                    </span>
                  </label>
                )
              })}
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  )
}

export function MoziaOperationsPermissionsSection() {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const [selectedAdminId, setSelectedAdminId] = useState<number | null>(null)
  const catalogQuery = useQuery({
    queryKey: permissionsQueryKey,
    queryFn: getOperationsPermissionCatalog,
  })
  const adminsQuery = useQuery({
    queryKey: managedAdminsQueryKey,
    queryFn: getManagedAdmins,
  })

  const admins = adminsQuery.data?.items ?? []
  const normalizedSearch = search.trim().toLowerCase()
  const filteredAdmins = normalizedSearch
    ? admins.filter((admin) =>
        `${admin.username} ${admin.display_name}`
          .toLowerCase()
          .includes(normalizedSearch)
      )
    : admins
  const activeAdminId = selectedAdminId ?? filteredAdmins[0]?.id ?? null
  const activeAdmin =
    admins.find((admin) => admin.id === activeAdminId) ?? filteredAdmins[0]

  if (catalogQuery.isLoading || adminsQuery.isLoading) {
    return <Skeleton className='h-96 w-full' />
  }

  if (catalogQuery.isError || adminsQuery.isError) {
    return (
      <Alert variant='destructive'>
        <AlertTitle>{t('Failed to load operating permissions')}</AlertTitle>
        <AlertDescription>
          {t('Refresh the page and try again.')}
        </AlertDescription>
      </Alert>
    )
  }

  return (
    <SettingsSection title={t('Operations Permissions')}>
      <Alert>
        <ShieldCheck aria-hidden='true' />
        <AlertTitle>{t('Server-enforced access control')}</AlertTitle>
        <AlertDescription>
          {t(
            'Permissions are checked by every protected API request. Give each operator an individual administrator account so audit logs identify the actual person.'
          )}
        </AlertDescription>
      </Alert>

      <div className='grid min-h-[32rem] gap-4 lg:grid-cols-[17rem_minmax(0,1fr)]'>
        <Card className='h-fit lg:sticky lg:top-4'>
          <CardHeader className='border-b'>
            <div className='flex items-center gap-2 font-medium'>
              <UserRoundCog className='size-4' aria-hidden='true' />
              {t('Administrators')}
              <Badge variant='secondary'>{admins.length}</Badge>
            </div>
            <div className='relative mt-2'>
              <Search
                className='text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2'
                aria-hidden='true'
              />
              <Input
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder={t('Search administrators')}
                className='pl-8'
              />
            </div>
          </CardHeader>
          <CardContent className='max-h-[32rem] space-y-1 overflow-y-auto'>
            {filteredAdmins.length === 0 ? (
              <p className='text-muted-foreground py-8 text-center text-sm'>
                {t('No administrators found')}
              </p>
            ) : (
              filteredAdmins.map((admin) => (
                <button
                  key={admin.id}
                  type='button'
                  onClick={() => setSelectedAdminId(admin.id)}
                  className={cn(
                    'flex w-full items-center justify-between gap-3 rounded-lg px-3 py-2.5 text-left transition-colors hover:bg-muted',
                    activeAdmin?.id === admin.id &&
                      'bg-primary text-primary-foreground hover:bg-primary/90'
                  )}
                >
                  <span className='min-w-0'>
                    <span className='block truncate text-sm font-medium'>
                      {admin.display_name || admin.username}
                    </span>
                    <span
                      className={cn(
                        'block truncate text-xs text-muted-foreground',
                        activeAdmin?.id === admin.id &&
                          'text-primary-foreground/70'
                      )}
                    >
                      @{admin.username}
                    </span>
                  </span>
                  {admin.status !== 1 && (
                    <span className='bg-destructive size-2 shrink-0 rounded-full' />
                  )}
                </button>
              ))
            )}
          </CardContent>
        </Card>

        {activeAdmin && catalogQuery.data ? (
          <PermissionEditor
            key={activeAdmin.id}
            admin={activeAdmin}
            catalog={catalogQuery.data}
          />
        ) : (
          <Card className='text-muted-foreground grid min-h-64 place-items-center p-8 text-center'>
            {t('Select an administrator to manage permissions')}
          </Card>
        )}
      </div>
    </SettingsSection>
  )
}
