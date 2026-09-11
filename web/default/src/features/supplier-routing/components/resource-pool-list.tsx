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
import { getRouteApi } from '@tanstack/react-router'
import {
  getCoreRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnDef,
} from '@tanstack/react-table'
import { useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import {
  DataTableView,
  DataTableToolbar,
  DataTablePagination,
} from '@/components/data-table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useTableUrlState } from '@/hooks/use-table-url-state'

import type { SupplierConfigValues } from '../lib/config-schema'

const route = getRouteApi('/_authenticated/resource-pools/')
type PoolRow = SupplierConfigValues['pools'][number] & {
  supplier: string
  supplierName: string
  status: string
  modelNames: string[]
  channels: number
}

export function ResourcePoolList(props: {
  config: SupplierConfigValues
  canPublish: boolean
  busy: boolean
  onEdit: (id: number) => void
  onDelete: (id: number) => void
  onToggle: (id: number) => void
}) {
  const { t } = useTranslation()
  const data = useMemo(
    () =>
      props.config.pools.map((pool) => ({
        ...pool,
        supplier: String(pool.supplier_id),
        supplierName:
          props.config.suppliers.find(
            (supplier) => supplier.id === pool.supplier_id
          )?.name ?? `#${pool.supplier_id}`,
        status: pool.enabled ? 'enabled' : 'disabled',
        modelNames: pool.models.map((model) => model.name),
        channels: new Set(
          props.config.bindings
            .filter((binding) => binding.pool_id === pool.id)
            .map((binding) => binding.channel_id)
        ).size,
      })),
    [props.config.pools, props.config.suppliers, props.config.bindings]
  )
  const url = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: { defaultPageSize: 10 },
    columnFilters: [
      { columnId: 'supplier', searchKey: 'supplier', type: 'array' },
      { columnId: 'status', searchKey: 'status', type: 'array' },
      { columnId: 'modelNames', searchKey: 'model', type: 'array' },
      {
        columnId: 'failure_domain',
        searchKey: 'failure_domain',
        type: 'array',
      },
    ],
  })
  const columns: ColumnDef<PoolRow>[] = [
    {
      accessorKey: 'name',
      header: t('Pool name'),
      cell: ({ row }) => (
        <div>
          <Button
            variant='link'
            className='h-auto p-0'
            onClick={() => props.onEdit(row.original.id)}
          >
            {row.original.name}
          </Button>
          <p className='text-muted-foreground text-xs'>#{row.original.id}</p>
        </div>
      ),
    },
    {
      accessorKey: 'supplier',
      header: t('Supplier'),
      filterFn: (row, id, values: string[]) =>
        values.includes(row.getValue(id)),
      cell: ({ row }) => row.original.supplierName,
    },
    {
      accessorKey: 'failure_domain',
      header: t('Failure domain'),
      filterFn: (row, id, values: string[]) =>
        values.includes(row.getValue(id)),
    },
    {
      accessorKey: 'modelNames',
      header: t('Models'),
      filterFn: 'arrIncludesSome',
      cell: ({ row }) => (
        <div
          className='max-w-64 truncate'
          title={row.original.modelNames.join(', ')}
        >
          {row.original.modelNames.join(', ') || '—'}
        </div>
      ),
    },
    { accessorKey: 'channels', header: t('Channels') },
    {
      id: 'limits',
      header: t('Shared capacity limits'),
      cell: ({ row }) => (
        <div className='text-sm whitespace-nowrap'>
          <p>
            {t('Concurrent requests')}: {row.original.limits.concurrency}
          </p>
          <p>
            {row.original.limits.rpm} RPM · {row.original.limits.tpm} TPM
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
      id: 'actions',
      header: t('Actions'),
      cell: ({ row }) => (
        <div className='flex gap-1'>
          <Button
            variant='ghost'
            size='sm'
            disabled={props.busy}
            onClick={() => props.onEdit(row.original.id)}
          >
            {props.canPublish ? t('Edit') : t('View')}
          </Button>
          {props.canPublish && (
            <>
              <Button
                variant='ghost'
                size='sm'
                disabled={props.busy}
                onClick={() => props.onToggle(row.original.id)}
              >
                {row.original.enabled ? t('Disable') : t('Enable')}
              </Button>
              <Button
                variant='ghost'
                size='sm'
                disabled={props.busy}
                onClick={() => props.onDelete(row.original.id)}
              >
                {t('Delete')}
              </Button>
            </>
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
      `${row.original.id} ${row.original.name}`
        .toLocaleLowerCase()
        .includes(value.toLocaleLowerCase()),
    state: {
      globalFilter: url.globalFilter,
      columnFilters: url.columnFilters,
      pagination: url.pagination,
    },
    onGlobalFilterChange: url.onGlobalFilterChange,
    onColumnFiltersChange: url.onColumnFiltersChange,
    onPaginationChange: url.onPaginationChange,
    autoResetPageIndex: false,
  })
  const pageCount = Math.max(1, table.getPageCount())
  useEffect(() => {
    url.ensurePageInRange(pageCount)
  }, [url, pageCount])
  return (
    <div className='space-y-4'>
      <DataTableToolbar
        table={table}
        hideViewOptions
        searchPlaceholder={t('Search pool name or ID...')}
        filters={[
          {
            columnId: 'supplier',
            title: t('Supplier'),
            options: props.config.suppliers.map((supplier) => ({
              value: String(supplier.id),
              label: `${supplier.name} · #${supplier.id}`,
            })),
          },
          {
            columnId: 'status',
            title: t('Status'),
            options: [
              { value: 'enabled', label: t('Enabled') },
              { value: 'disabled', label: t('Disabled') },
            ],
          },
          {
            columnId: 'modelNames',
            title: t('Models'),
            options: [...new Set(data.flatMap((pool) => pool.modelNames))]
              .sort()
              .map((value) => ({ value, label: value })),
          },
          {
            columnId: 'failure_domain',
            title: t('Failure domain'),
            options: [...new Set(data.map((pool) => pool.failure_domain))]
              .sort()
              .map((value) => ({ value, label: value })),
          },
        ]}
      />
      <DataTableView
        table={table}
        emptyTitle={t('No resource pools found')}
        emptyDescription={t('Add a resource pool or adjust the filters.')}
      />
      <DataTablePagination table={table} />
    </div>
  )
}
