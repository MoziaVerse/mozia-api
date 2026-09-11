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
import {
  getCoreRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnDef,
} from '@tanstack/react-table'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DataTableView,
  DataTableToolbar,
  DataTablePagination,
} from '@/components/data-table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'

import type { SupplierConfigValues } from '../lib/config-schema'

type SupplierRow = SupplierConfigValues['suppliers'][number] & {
  status: string
  models: string[]
  pools: number
  channels: number
  used: boolean
}

export function SupplierList(props: {
  config: SupplierConfigValues
  readOnly: boolean
  onEdit: (id: number) => void
  onDelete: (id: number) => void
}) {
  const { t } = useTranslation()
  const { suppliers, pools, bindings, rules } = props.config
  const data = useMemo(
    () =>
      suppliers.map((supplier) => {
        const ownPools = pools.filter(
          (pool) => pool.supplier_id === supplier.id
        )
        const ownBindings = bindings.filter((binding) =>
          ownPools.some((pool) => pool.id === binding.pool_id)
        )
        return {
          ...supplier,
          status: supplier.enabled ? 'enabled' : 'disabled',
          models: [
            ...new Set(
              ownPools
                .flatMap((pool) => pool.models.map((model) => model.name))
                .filter(Boolean)
            ),
          ],
          pools: ownPools.length,
          channels: new Set(ownBindings.map((binding) => binding.channel_id))
            .size,
          used:
            ownPools.length > 0 ||
            rules.some((rule) =>
              rule.targets.some((target) => target.supplier_id === supplier.id)
            ),
        }
      }),
    [suppliers, pools, bindings, rules]
  )
  const columns: ColumnDef<SupplierRow>[] = [
    {
      accessorKey: 'name',
      header: t('Supplier name'),
      cell: ({ row }) => (
        <div>
          <Button
            variant='link'
            className='h-auto p-0'
            onClick={() => props.onEdit(row.original.id)}
          >
            {row.original.name || t('New supplier')}
          </Button>
          <p className='text-muted-foreground text-xs'>
            #{row.original.id}
            {row.original.contact ? ` · ${row.original.contact}` : ''}
          </p>
        </div>
      ),
    },
    {
      accessorKey: 'status',
      header: t('Status'),
      filterFn: (row, id, values: string[]) =>
        values.includes(row.getValue(id)),
      cell: ({ row }) => (
        <Badge variant={row.original.enabled ? 'default' : 'secondary'}>
          {row.original.enabled ? t('Enabled') : t('Disabled')}
        </Badge>
      ),
    },
    {
      accessorKey: 'region',
      header: t('Region'),
      filterFn: (row, id, values: string[]) =>
        values.includes(row.getValue(id)),
      cell: ({ row }) => row.original.region || '—',
    },
    {
      accessorKey: 'models',
      header: t('Models'),
      filterFn: 'arrIncludesSome',
      cell: ({ row }) => (
        <div
          className='max-w-64 truncate'
          title={row.original.models.join(', ')}
        >
          {row.original.models.join(', ') || '—'}
        </div>
      ),
    },
    { accessorKey: 'pools', header: t('Shared resource pools') },
    { accessorKey: 'channels', header: t('Channels') },
    {
      id: 'actions',
      header: t('Actions'),
      cell: ({ row }) => (
        <div className='flex gap-1'>
          <Button
            variant='ghost'
            size='sm'
            onClick={() => props.onEdit(row.original.id)}
          >
            {props.readOnly ? t('View') : t('Edit')}
          </Button>
          {!props.readOnly && (
            <Button
              variant='ghost'
              size='sm'
              disabled={row.original.used}
              title={
                row.original.used
                  ? t('Remove references before deleting this item.')
                  : undefined
              }
              onClick={() => props.onDelete(row.original.id)}
            >
              {t('Delete')}
            </Button>
          )}
        </div>
      ),
    },
  ]
  const table = useReactTable({
    data,
    columns,
    getRowId: (row) => String(row.id),
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getSortedRowModel: getSortedRowModel(),
    globalFilterFn: (row, _column, value: string) =>
      `${row.original.id} ${row.original.name} ${row.original.contact ?? ''}`
        .toLocaleLowerCase()
        .includes(value.toLocaleLowerCase()),
    initialState: { pagination: { pageSize: 10, pageIndex: 0 } },
  })
  return (
    <div className='space-y-4'>
      <DataTableToolbar
        table={table}
        hideViewOptions
        searchPlaceholder={t('Search name, ID or contact...')}
        filters={[
          {
            columnId: 'status',
            title: t('Status'),
            options: [
              { value: 'enabled', label: t('Enabled') },
              { value: 'disabled', label: t('Disabled') },
            ],
          },
          {
            columnId: 'region',
            title: t('Region'),
            options: [
              ...new Set(
                data
                  .map((row) => row.region)
                  .filter((region): region is string => Boolean(region))
              ),
            ]
              .sort()
              .map((value) => ({ label: value, value })),
          },
          {
            columnId: 'models',
            title: t('Models'),
            options: [...new Set(data.flatMap((row) => row.models))]
              .sort()
              .map((value) => ({ label: value, value })),
          },
        ]}
      />
      <DataTableView
        table={table}
        emptyTitle={t('No suppliers found')}
        emptyDescription={t('Create a supplier or adjust the filters.')}
      />
      <DataTablePagination table={table} />
    </div>
  )
}
