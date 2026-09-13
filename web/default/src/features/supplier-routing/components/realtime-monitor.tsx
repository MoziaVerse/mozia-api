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
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyHeader,
  EmptyTitle,
  EmptyDescription,
} from '@/components/ui/empty'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@/components/ui/table'

import { getSupplierRealtime } from '../api'

export function SupplierRealtimeMonitor(props: {
  suppliers: Map<number, string>
  pools: Map<number, string>
}) {
  const { t } = useTranslation()
  const [supplier, setSupplier] = useState('all')
  const [model, setModel] = useState('all')
  const realtime = useQuery({
    queryKey: ['supplier-routing-realtime'],
    queryFn: getSupplierRealtime,
    refetchInterval: 10000,
  })
  const allRows = realtime.data?.rows ?? []
  const rows = allRows.filter(
    (row) =>
      (supplier === 'all' || String(row.supplier_id) === supplier) &&
      (model === 'all' || row.model === model)
  )
  const labels: Record<string, string> = {
    trial: t('Trial'),
    normal: t('Healthy'),
    degraded: t('Degraded'),
    paused: t('Paused'),
    overloaded: t('Overloaded'),
  }
  return (
    <div className='space-y-4'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Real calls only; refreshes every 10 seconds. Metrics cover completions in the current minute and previous four minutes. Probes, recommendations, caller errors and cancellations are excluded.'
        )}
      </p>
      <details className='rounded-lg border p-3 text-sm'>
        <summary className='cursor-pointer font-medium'>
          {t('How is quality ranked?')}
        </summary>
        <p className='text-muted-foreground mt-2'>
          {t(
            'Rank resource pools only within the same model version, group, stream type and routing rule. Compare success rate first, then lower overload rate, higher throughput and lower TTFT. Insufficient samples are unranked; this display does not change routing weights.'
          )}
        </p>
        <p className='text-muted-foreground mt-2'>
          {t(
            'Success rate includes provider failures and HTTP 429 in its denominator. Speed metrics average successful measured calls. New and recovering pools may still be in trial even when their recent ranking is high.'
          )}
        </p>
      </details>
      <div className='flex flex-wrap items-end gap-4'>
        <div className='space-y-2'>
          <Label htmlFor='realtime-model'>{t('Model')}</Label>
          <NativeSelect
            id='realtime-model'
            value={model}
            onChange={(event) => setModel(event.target.value)}
          >
            <NativeSelectOption value='all'>
              {t('All models')}
            </NativeSelectOption>
            {[...new Set(allRows.map((row) => row.model))]
              .sort()
              .map((name) => (
                <NativeSelectOption key={name} value={name}>
                  {name}
                </NativeSelectOption>
              ))}
          </NativeSelect>
        </div>
        <div className='space-y-2'>
          <Label htmlFor='realtime-supplier'>{t('Supplier')}</Label>
          <NativeSelect
            id='realtime-supplier'
            value={supplier}
            onChange={(event) => setSupplier(event.target.value)}
          >
            <NativeSelectOption value='all'>
              {t('All suppliers')}
            </NativeSelectOption>
            {[...props.suppliers].map(([id, name]) => (
              <NativeSelectOption key={id} value={id}>
                {name} · #{id}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </div>
        <Button
          variant='outline'
          disabled={realtime.isFetching}
          onClick={() => void realtime.refetch()}
        >
          {t('Refresh')}
        </Button>
        {realtime.data?.window_start && realtime.data.window_end && (
          <p className='text-muted-foreground text-xs'>
            {t('Observation window')}:{' '}
            {new Date(realtime.data.window_start * 1000).toLocaleTimeString()} –{' '}
            {new Date(realtime.data.window_end * 1000).toLocaleTimeString()}
          </p>
        )}
      </div>
      {realtime.error && (
        <Alert variant='destructive'>
          <AlertDescription>{realtime.error.message}</AlertDescription>
        </Alert>
      )}
      {realtime.data?.available === false && (
        <Alert>
          <AlertDescription>
            {t(
              'Real-time metrics are unavailable. You can still query historical monitoring.'
            )}
          </AlertDescription>
        </Alert>
      )}
      {realtime.isPending && <p>{t('Loading...')}</p>}
      {realtime.data?.available && (
        <div className='overflow-x-auto rounded-lg border'>
          <Table>
            <TableHeader>
              <TableRow>
                {[
                  t('Rank'),
                  t('Supplier'),
                  t('Pool'),
                  t('Model'),
                  t('Group'),
                  t('Routing rule'),
                  t('Stream type'),
                  t('Samples'),
                  t('Success rate'),
                  t('Overload rate'),
                  t('Throughput (tokens/s)'),
                  t('TTFT (ms)'),
                  t('Health'),
                ].map((label) => (
                  <TableHead key={label}>{label}</TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row) => (
                <TableRow key={row.scope}>
                  <TableCell>
                    {row.rank > 0 ? (
                      `#${row.rank}`
                    ) : (
                      <Badge variant='secondary'>
                        {t('Insufficient samples')}
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell>
                    {props.suppliers.get(row.supplier_id) ||
                      `#${row.supplier_id}`}
                  </TableCell>
                  <TableCell>
                    {props.pools.get(row.pool_id) || `#${row.pool_id}`}
                  </TableCell>
                  <TableCell>
                    {row.model}
                    <div className='text-muted-foreground text-xs'>
                      {row.model_version || t('Unspecified version')}
                    </div>
                  </TableCell>
                  <TableCell>{row.group_name || '—'}</TableCell>
                  <TableCell>{row.rule_id || '—'}</TableCell>
                  <TableCell>
                    {row.is_stream ? t('Streaming') : t('Non-streaming')}
                  </TableCell>
                  <TableCell>
                    {row.samples}
                    <div className='text-muted-foreground text-xs'>
                      {t('Minimum samples')}: {row.min_samples}
                    </div>
                  </TableCell>
                  <TableCell>
                    {row.samples ? `${row.success_rate.toFixed(1)}%` : '—'}
                  </TableCell>
                  <TableCell>
                    {row.samples ? `${row.overload_rate.toFixed(1)}%` : '—'}
                  </TableCell>
                  <TableCell>
                    {row.throughput_samples ? row.throughput.toFixed(1) : '—'}
                  </TableCell>
                  <TableCell>
                    {row.ttft_samples ? Math.round(row.ttft_ms) : '—'}
                  </TableCell>
                  <TableCell>{labels[row.state] || row.state}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {!rows.length && (
            <Empty>
              <EmptyHeader>
                <EmptyTitle>{t('No real-time quality samples')}</EmptyTitle>
                <EmptyDescription>
                  {t(
                    'Matching supplier calls will appear here. Expired samples remain accessible in historical monitoring.'
                  )}
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          )}
        </div>
      )}
    </div>
  )
}
