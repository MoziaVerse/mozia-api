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
import { formatTimestampForInput, parseTimestampFromInput } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'

import {
  getSupplierRouting,
  getSupplierStats,
  getSupplierAttempts,
  type SupplierAttempt,
  type SupplierHistoryFilter,
} from './api'
import { SupplierRealtimeMonitor } from './components/realtime-monitor'
import { SupplierReconciliation } from './components/reconciliation'

export function SupplierMonitor() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const canRead = hasPermission(user, 'channel', 'read')
  const canReadCost = canRead && hasPermission(user, 'model_pricing', 'read')
  const canWriteCost = hasPermission(user, 'model_pricing', 'write')
  const [view, setView] = useState('realtime')
  const [historyTab, setHistoryTab] = useState('traffic')
  const [page, setPage] = useState(1)
  const [filters, setFilters] = useState<SupplierHistoryFilter>(() => ({
    start_timestamp: Math.floor(Date.now() / 60000) * 60 - 86400,
    end_timestamp: (Math.floor(Date.now() / 60000) + 1) * 60,
  }))
  const [rangeError, setRangeError] = useState(false)
  const [supplier, setSupplier] = useState('all')
  const [model, setModel] = useState('')
  const [reconcile, setReconcile] = useState<SupplierAttempt | null>(null)
  const config = useQuery({
    queryKey: ['supplier-routing'],
    queryFn: getSupplierRouting,
    enabled: canRead,
  })
  const stats = useQuery({
    queryKey: ['supplier-routing-stats', filters],
    queryFn: () => getSupplierStats(filters),
    enabled: canRead && view === 'history',
  })
  const attempts = useQuery({
    queryKey: ['supplier-routing-attempts', filters, page],
    queryFn: () => getSupplierAttempts({ ...filters, p: page, page_size: 20 }),
    enabled: canReadCost && view === 'history' && historyTab === 'calls',
  })
  const suppliers = new Map(
    config.data?.config.suppliers?.map((s) => [s.id, s.name])
  )
  const pools = new Map(config.data?.config.pools?.map((p) => [p.id, p.name]))
  const rows = stats.data?.rows ?? []
  const traffic = rows.filter((row) => row.kind !== 'shadow')
  const observations = rows.filter((row) => row.kind === 'shadow')
  const calls = attempts.data?.items ?? []
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
    overloaded: t('Overloaded'),
    calculated: t('Calculated'),
    unavailable: t('Unavailable'),
  }
  if (!canRead) return null
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Supplier monitoring')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button render={<Link to='/suppliers' />}>
          {t('Configure routing')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        {config.error && (
          <Alert variant='destructive'>
            <AlertDescription>{config.error.message}</AlertDescription>
          </Alert>
        )}
        <Tabs value={view} onValueChange={(value) => setView(String(value))}>
          <TabsList>
            <TabsTrigger value='realtime'>
              {t('Real-time quality ranking')}
            </TabsTrigger>
            <TabsTrigger value='history'>
              {t('Historical monitoring')}
            </TabsTrigger>
          </TabsList>
          <TabsContent value='realtime'>
            <SupplierRealtimeMonitor suppliers={suppliers} pools={pools} />
          </TabsContent>
          <TabsContent value='history'>
            <div className='space-y-6'>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Query persisted supplier calls by request time. History remains available when real-time metrics expire.'
                )}
              </p>
              <form
                className='flex flex-wrap items-end gap-4'
                onSubmit={(event) => {
                  event.preventDefault()
                  const data = new FormData(event.currentTarget)
                  const start = parseTimestampFromInput(
                    String(data.get('start'))
                  )
                  const end = parseTimestampFromInput(String(data.get('end')))
                  if (
                    !Number.isFinite(start) ||
                    !Number.isFinite(end) ||
                    start < 0 ||
                    start >= end
                  ) {
                    setRangeError(true)
                    return
                  }
                  setRangeError(false)
                  setPage(1)
                  setReconcile(null)
                  setFilters({
                    start_timestamp: start,
                    end_timestamp: end,
                    supplier_id:
                      supplier === 'all' ? undefined : Number(supplier),
                    model: model.trim() || undefined,
                  })
                }}
              >
                <div className='space-y-2'>
                  <Label htmlFor='history-start'>{t('Start time')}</Label>
                  <Input
                    id='history-start'
                    name='start'
                    type='datetime-local'
                    required
                    defaultValue={formatTimestampForInput(
                      filters.start_timestamp
                    )}
                  />
                </div>
                <div className='space-y-2'>
                  <Label htmlFor='history-end'>{t('End time')}</Label>
                  <Input
                    id='history-end'
                    name='end'
                    type='datetime-local'
                    required
                    defaultValue={formatTimestampForInput(
                      filters.end_timestamp
                    )}
                  />
                </div>
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
                    placeholder={t('Exact model name (optional)')}
                  />
                </div>
                <Button type='submit'>{t('Search')}</Button>
                <Button
                  type='button'
                  variant='outline'
                  disabled={stats.isFetching || attempts.isFetching}
                  onClick={() => {
                    void stats.refetch()
                    if (canReadCost && historyTab === 'calls') {
                      void attempts.refetch()
                    }
                  }}
                >
                  {t('Refresh')}
                </Button>
              </form>
              {rangeError && (
                <Alert variant='destructive'>
                  <AlertDescription>
                    {t('Start time must precede end time.')}
                  </AlertDescription>
                </Alert>
              )}
              {stats.error && (
                <Alert variant='destructive'>
                  <AlertDescription>{stats.error.message}</AlertDescription>
                </Alert>
              )}
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
                  'Summary and costs use the selected range and filters. Requests are deduplicated across matching first attempts and retries; probes and recommendations are excluded.'
                )}
              </p>
              {canReadCost &&
                stats.data?.costs?.map((cost) => (
                  <Card key={cost.currency || 'unknown'}>
                    <CardHeader>
                      <CardTitle>
                        {t('Procurement in selected period')} ·{' '}
                        {cost.currency || t('Unknown')}
                      </CardTitle>
                    </CardHeader>
                    <CardContent className='flex flex-wrap gap-x-8 gap-y-3 text-sm'>
                      <span>
                        {t('Procurement cost')}: {cost.total}
                      </span>
                      <span>
                        {t('Retry cost')}: {cost.retry_cost}
                      </span>
                      <span>
                        {t('Failed call cost')}: {cost.failed_cost}
                      </span>
                      <span>
                        {t('Cost per successful request')}:{' '}
                        {cost.successful_requests > 0
                          ? (
                              Number(cost.total) / cost.successful_requests
                            ).toFixed(8)
                          : '—'}
                      </span>
                      <span>
                        {t('Pending reconciliation')}: {cost.pending}
                      </span>
                      {cost.pending > 0 && (
                        <p className='text-muted-foreground w-full'>
                          {t(
                            'Costs are provisional until pending calls are reconciled.'
                          )}
                        </p>
                      )}
                    </CardContent>
                  </Card>
                ))}
              <Tabs
                value={historyTab}
                onValueChange={(value) => setHistoryTab(String(value))}
              >
                <TabsList className='h-auto flex-wrap'>
                  <TabsTrigger value='traffic'>
                    {t('Historical supplier traffic')}
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
                      'First-dispatch share is calculated within the selected suppliers, model, group and time range.'
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
                              t('Latency (ms)'),
                              t('First-dispatch share'),
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
                              <TableCell>
                                {labels[row.kind] || row.kind}
                              </TableCell>
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
                                {row.avg_latency_ms > 0
                                  ? Math.round(row.avg_latency_ms)
                                  : '—'}
                              </TableCell>
                              <TableCell>
                                {row.kind === 'first'
                                  ? `${row.first_share_percent.toFixed(1)}%`
                                  : '—'}
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
                            <EmptyTitle>
                              {t('No supplier traffic yet')}
                            </EmptyTitle>
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
                    {!observations.length &&
                      !stats.isPending &&
                      !stats.error && (
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
                        'Persisted calls in the selected period, including retries and probes. Recommendations are shown separately.'
                      )}
                    </p>
                    {attempts.error && (
                      <Alert variant='destructive'>
                        <AlertDescription>
                          {attempts.error.message}
                        </AlertDescription>
                      </Alert>
                    )}
                    <div className='flex items-center gap-3'>
                      <Button
                        variant='outline'
                        disabled={page <= 1 || attempts.isFetching}
                        onClick={() => setPage(page - 1)}
                      >
                        {t('Previous page')}
                      </Button>
                      <span className='text-sm'>
                        {t('Page {{page}} · {{total}} records', {
                          page,
                          total: attempts.data?.total ?? 0,
                        })}
                      </span>
                      <Button
                        variant='outline'
                        disabled={
                          attempts.isFetching ||
                          page * 20 >= (attempts.data?.total ?? 0)
                        }
                        onClick={() => setPage(page + 1)}
                      >
                        {t('Next page')}
                      </Button>
                    </div>
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
                                t('Estimated procurement cost'),
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
                                <TableCell>
                                  {attempt.reason === 'adaptive'
                                    ? t('Experience qualified, cost first')
                                    : attempt.reason}
                                  {attempt.health_state && (
                                    <div className='text-muted-foreground text-xs'>
                                      {labels[attempt.health_state] ||
                                        attempt.health_state}{' '}
                                      · {t('Routing weight')}:{' '}
                                      {attempt.routing_weight}
                                    </div>
                                  )}
                                </TableCell>
                                <TableCell>
                                  {labels[attempt.status] || attempt.status}
                                </TableCell>
                                <TableCell>{attempt.input_tokens}</TableCell>
                                <TableCell>{attempt.output_tokens}</TableCell>
                                <TableCell>
                                  {attempt.estimated_cost || '—'}{' '}
                                  {attempt.currency}
                                </TableCell>
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
          </TabsContent>
        </Tabs>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
