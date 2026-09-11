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
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import {
  Empty,
  EmptyHeader,
  EmptyTitle,
  EmptyDescription,
} from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { hasPermission } from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

import {
  getSupplierRouting,
  getSupplierStats,
  getSupplierAttempts,
  type SupplierAttempt,
} from './api'
import { SupplierReconciliation } from './components/reconciliation'

export function SupplierMonitor() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const canRead = hasPermission(user, 'channel', 'read')
  const canReadCost = canRead && hasPermission(user, 'model_pricing', 'read')
  const canWriteCost = hasPermission(user, 'model_pricing', 'write')
  const [supplier, setSupplier] = useState('all')
  const [model, setModel] = useState('')
  const [reconcile, setReconcile] = useState<SupplierAttempt | null>(null)
  const config = useQuery({
    queryKey: ['supplier-routing'],
    queryFn: getSupplierRouting,
    enabled: canRead,
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
  const suppliers = new Map(
    config.data?.config.suppliers?.map((s) => [s.id, s.name])
  )
  const pools = new Map(config.data?.config.pools?.map((p) => [p.id, p.name]))
  const rows = (stats.data?.rows ?? []).filter(
    (row) =>
      (supplier === 'all' || String(row.supplier_id) === supplier) &&
      row.model.toLowerCase().includes(model.toLowerCase())
  )
  const traffic = rows.filter((row) => row.kind !== 'shadow')
  const observations = rows.filter((row) => row.kind === 'shadow')
  const calls = (attempts.data ?? []).filter(
    (row) =>
      row.kind !== 'shadow' &&
      (supplier === 'all' || String(row.supplier_id) === supplier) &&
      row.model.toLowerCase().includes(model.toLowerCase())
  )
  const labels: Record<string, string> = {
    first: t('First attempt'),
    retry: t('Retry'),
    probe: t('Probe'),
    success: t('Success'),
    failed: t('Failed'),
    pending: t('Pending'),
    unknown: t('Unknown'),
    cancelled: t('Not sent'),
    not_applicable: t('Not applicable'),
    settled: t('Settled'),
    reconciled: t('Reconciled'),
    trial: t('Trial'),
    normal: t('Healthy'),
    degraded: t('Degraded'),
    paused: t('Paused'),
    unavailable: t('Unavailable'),
  }
  if (!canRead) return null
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Supplier monitoring')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          disabled={stats.isFetching || attempts.isFetching}
          onClick={() => {
            void config.refetch()
            void stats.refetch()
            if (canReadCost) void attempts.refetch()
          }}
        >
          {t('Refresh')}
        </Button>
        <Button render={<Link to='/suppliers' />}>
          {t('Configure routing')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='space-y-6'>
          <div>
            <p className='text-muted-foreground text-sm'>
              {t(
                'Supplier requests, health and procurement records. Refreshes every 30 seconds.'
              )}
            </p>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('Current hour starts at')}:{' '}
              {stats.data
                ? new Date(stats.data.hour_start * 1000).toLocaleString()
                : '—'}
            </p>
          </div>
          {[
            { name: 'config', error: config.error },
            { name: 'stats', error: stats.error },
          ]
            .filter((result) => result.error)
            .map((result) => (
              <Alert variant='destructive' key={result.name}>
                <AlertDescription>{result.error?.message}</AlertDescription>
              </Alert>
            ))}
          <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-4'>
            {[
              { label: t('Requests'), value: stats.data?.summary.requests },
              {
                label: t('First-attempt successes'),
                value: stats.data?.summary.first_success,
              },
              {
                label: t('Final successes'),
                value: stats.data?.summary.final_success,
              },
              {
                label: t('Unresolved requests'),
                value: stats.data?.summary.unresolved,
              },
            ].map((card) => (
              <Card key={card.label}>
                <CardHeader>
                  <CardTitle className='text-muted-foreground text-sm font-normal'>
                    {card.label}
                  </CardTitle>
                </CardHeader>
                <CardContent className='text-2xl font-semibold tabular-nums'>
                  {card.value?.toLocaleString() ?? '—'}
                </CardContent>
              </Card>
            ))}
          </div>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Summary covers all suppliers this hour and excludes probes and recommendations. Filters below apply to tables only.'
            )}
          </p>
          <div className='flex flex-wrap items-end gap-4'>
            <div className='space-y-2'>
              <Label htmlFor='monitor-supplier'>{t('Supplier')}</Label>
              <NativeSelect
                id='monitor-supplier'
                value={supplier}
                onChange={(event) => setSupplier(event.target.value)}
              >
                <NativeSelectOption value='all'>
                  {t('All suppliers')}
                </NativeSelectOption>
                {[...suppliers].map(([id, name]) => (
                  <NativeSelectOption key={id} value={id}>
                    {name} · #{id}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </div>
            <div className='space-y-2'>
              <Label htmlFor='monitor-model'>{t('Model')}</Label>
              <Input
                id='monitor-model'
                value={model}
                onChange={(event) => setModel(event.target.value)}
                placeholder={t('Filter by model')}
              />
            </div>
          </div>
          <Tabs defaultValue='traffic'>
            <TabsList className='h-auto flex-wrap'>
              <TabsTrigger value='traffic'>
                {t('Supplier traffic this hour')}
              </TabsTrigger>
              <TabsTrigger value='observations'>
                {t('Routing recommendations')}
              </TabsTrigger>
              {canReadCost && (
                <TabsTrigger value='calls'>
                  {t('Supplier call records')}
                </TabsTrigger>
              )}
            </TabsList>
            <TabsContent value='traffic' className='space-y-3'>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'First-dispatch share is calculated per supplier for the same model and group. It is not a percentage of all platform requests.'
                )}
              </p>
              {stats.isPending ? (
                <p>{t('Loading...')}</p>
              ) : (
                <div className='overflow-x-auto rounded-lg border'>
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
                      {traffic.map((row) => (
                        <TableRow
                          key={`${row.pool_id}:${row.model}:${row.group_name}:${row.kind}:${row.status}:${row.priority_fallback}:${row.outcome_class}`}
                        >
                          <TableCell>
                            {suppliers.get(row.supplier_id) ||
                              `#${row.supplier_id}`}
                          </TableCell>
                          <TableCell>
                            {pools.get(row.pool_id) || `#${row.pool_id}`}
                          </TableCell>
                          <TableCell>{row.model}</TableCell>
                          <TableCell>{row.group_name || '—'}</TableCell>
                          <TableCell>{labels[row.kind] || row.kind}</TableCell>
                          <TableCell>
                            <Badge
                              variant={
                                row.status === 'failed'
                                  ? 'destructive'
                                  : 'secondary'
                              }
                            >
                              {labels[row.status] || row.status}
                            </Badge>
                          </TableCell>
                          <TableCell>{row.requests}</TableCell>
                          <TableCell>
                            {row.avg_ttft_ms > 0
                              ? Math.round(row.avg_ttft_ms)
                              : '—'}
                          </TableCell>
                          <TableCell>
                            {row.kind === 'first'
                              ? `${row.first_share_percent.toFixed(1)}%`
                              : '—'}
                          </TableCell>
                          <TableCell>
                            {labels[row.health_state] ||
                              row.health_state ||
                              '—'}{' '}
                            {row.health_scale > 0 && `${row.health_scale}%`}
                          </TableCell>
                          <TableCell>
                            {row.priority_fallback ? t('Yes') : t('No')}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                  {!traffic.length && !stats.error && (
                    <Empty>
                      <EmptyHeader>
                        <EmptyTitle>{t('No supplier traffic yet')}</EmptyTitle>
                        <EmptyDescription>
                          {t(
                            'Calls appear after requests reach channels bound to a supplier resource pool.'
                          )}
                        </EmptyDescription>
                      </EmptyHeader>
                    </Empty>
                  )}
                </div>
              )}
            </TabsContent>
            <TabsContent value='observations' className='space-y-3'>
              <Alert>
                <AlertDescription>
                  {t(
                    'These are scheduling suggestions, not model calls. They do not consume supplier capacity and are excluded from request success and cost totals.'
                  )}
                </AlertDescription>
              </Alert>
              <div className='overflow-x-auto rounded-lg border'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      {[
                        t('Recommended supplier'),
                        t('Pool'),
                        t('Model'),
                        t('Group'),
                        t('Recommendations'),
                      ].map((label) => (
                        <TableHead key={label}>{label}</TableHead>
                      ))}
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {observations.map((row) => (
                      <TableRow
                        key={`${row.pool_id}:${row.model}:${row.group_name}:${row.status}`}
                      >
                        <TableCell>
                          {suppliers.get(row.supplier_id) ||
                            `#${row.supplier_id}`}
                        </TableCell>
                        <TableCell>
                          {pools.get(row.pool_id) || `#${row.pool_id}`}
                        </TableCell>
                        <TableCell>{row.model}</TableCell>
                        <TableCell>{row.group_name || '—'}</TableCell>
                        <TableCell>{row.requests}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
                {!observations.length && !stats.isPending && !stats.error && (
                  <Empty>
                    <EmptyHeader>
                      <EmptyTitle>
                        {t('No routing recommendations yet')}
                      </EmptyTitle>
                      <EmptyDescription>
                        {t(
                          'Enable observation and send requests matching a routing rule to collect recommendations.'
                        )}
                      </EmptyDescription>
                    </EmptyHeader>
                  </Empty>
                )}
              </div>
            </TabsContent>
            {canReadCost && (
              <TabsContent value='calls' className='space-y-3'>
                <p className='text-muted-foreground text-sm'>
                  {t(
                    'Latest 100 supplier attempts, including retries and probes. Recommendations are shown separately.'
                  )}
                </p>
                {attempts.error && (
                  <Alert variant='destructive'>
                    <AlertDescription>
                      {attempts.error.message}
                    </AlertDescription>
                  </Alert>
                )}
                {reconcile && (
                  <SupplierReconciliation
                    key={reconcile.id}
                    attempt={reconcile}
                    onClose={() => setReconcile(null)}
                  />
                )}
                {attempts.isPending ? (
                  <p>{t('Loading...')}</p>
                ) : (
                  <div className='overflow-x-auto rounded-lg border'>
                    <Table>
                      <TableHeader>
                        <TableRow>
                          {[
                            t('Time'),
                            t('Request'),
                            t('Supplier'),
                            t('Model'),
                            t('Channel'),
                            t('Attempt type'),
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
                        {calls.map((attempt) => (
                          <TableRow key={attempt.id}>
                            <TableCell className='whitespace-nowrap'>
                              {new Date(
                                attempt.created_at * 1000
                              ).toLocaleString()}
                            </TableCell>
                            <TableCell title={attempt.request_id}>
                              {attempt.request_id.slice(-10)} /{' '}
                              {attempt.attempt}
                            </TableCell>
                            <TableCell>
                              {suppliers.get(attempt.supplier_id) ||
                                `#${attempt.supplier_id}`}
                            </TableCell>
                            <TableCell>{attempt.model}</TableCell>
                            <TableCell>{attempt.channel_id}</TableCell>
                            <TableCell>
                              {labels[attempt.kind] || attempt.kind}
                            </TableCell>
                            <TableCell>{attempt.reason}</TableCell>
                            <TableCell>
                              {labels[attempt.status] || attempt.status}
                            </TableCell>
                            <TableCell>{attempt.input_tokens}</TableCell>
                            <TableCell>{attempt.output_tokens}</TableCell>
                            <TableCell>
                              {attempt.cost || t('Pending reconciliation')}{' '}
                              {attempt.currency}
                            </TableCell>
                            <TableCell>
                              {canWriteCost &&
                              attempt.cost_status === 'pending' ? (
                                <Button
                                  type='button'
                                  variant='outline'
                                  size='sm'
                                  onClick={() => setReconcile(attempt)}
                                >
                                  {t('Reconcile')}
                                </Button>
                              ) : (
                                labels[attempt.cost_status] ||
                                attempt.cost_status
                              )}
                            </TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                    {!calls.length && !attempts.error && (
                      <Empty>
                        <EmptyHeader>
                          <EmptyTitle>
                            {t('No supplier call records yet')}
                          </EmptyTitle>
                        </EmptyHeader>
                      </Empty>
                    )}
                  </div>
                )}
              </TabsContent>
            )}
          </Tabs>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
