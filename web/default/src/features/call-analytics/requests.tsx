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
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@/components/ui/table'
import dayjs from '@/lib/dayjs'
import { formatQuota } from '@/lib/format'

import type { AnalyticsRequestsPage } from './api'
import { requestLogTimeRange, requestPageNavigation } from './filters'

export function AnalyticsRequests(props: {
  data: AnalyticsRequestsPage
  total: number | null
  startTimestamp: number
  endTimestamp: number
  loading: boolean
  onPageChange: (page: number) => void
}) {
  const { t } = useTranslation()
  const rows = props.data
  const outcomes = {
    success: t('Success'),
    error: t('Failed'),
    cancelled: t('Cancelled'),
    unknown: t('Unknown'),
  }
  const { totalPages, hasNext } = requestPageNavigation(rows, props.total)
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          {t('Filtered requests')}{' '}
          <span className='text-muted-foreground font-normal'>
            (
            {props.total == null
              ? t('Total unavailable')
              : props.total.toLocaleString()}
            )
          </span>
        </CardTitle>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Time')}</TableHead>
              <TableHead>{t('User')}</TableHead>
              <TableHead>{t('Requested model')}</TableHead>
              <TableHead>{t('Result')}</TableHead>
              <TableHead>{t('Channel ID')}</TableHead>
              <TableHead>{t('Consumption')}</TableHead>
              <TableHead>{t('First response latency')}</TableHead>
              <TableHead>{t('Details')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.items.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={8}
                  className='text-muted-foreground py-12 text-center'
                >
                  {t('No data')}
                </TableCell>
              </TableRow>
            )}
            {rows.items.map((row, index) => {
              // Legacy rows have no ID. They are immutable within this page snapshot.
              const rowKey = row.request_id
                ? `${row.user_id}:${row.request_id}`
                : `${rows.p}:${row.user_id}:${row.completed_at}:${index}`
              return (
                <TableRow key={rowKey}>
                  <TableCell className='align-top'>
                    {dayjs.unix(row.completed_at).format('MM-DD HH:mm:ss')}
                  </TableCell>
                  <TableCell className='align-top'>
                    <p>{row.username || '—'}</p>
                    <span className='text-muted-foreground text-xs'>
                      #{row.user_id}
                    </span>
                  </TableCell>
                  <TableCell className='align-top'>
                    <p>{row.model_name || '—'}</p>
                    {row.effective_model &&
                      row.effective_model !== row.model_name && (
                        <p className='text-muted-foreground text-xs'>
                          → {row.effective_model}
                        </p>
                      )}
                  </TableCell>
                  <TableCell className='align-top'>
                    <Badge
                      variant={
                        row.outcome === 'error' ? 'destructive' : 'secondary'
                      }
                    >
                      {outcomes[row.outcome]}
                    </Badge>
                    <p className='text-muted-foreground mt-1 text-xs'>
                      {row.status_code || '—'}
                      {!row.final_recorded && ` · ${t('Inferred')}`}
                    </p>
                  </TableCell>
                  <TableCell className='align-top'>
                    {row.channel_id || '—'}
                  </TableCell>
                  <TableCell className='align-top'>
                    {formatQuota(row.quota)}
                  </TableCell>
                  <TableCell className='align-top'>
                    {row.frt_ms == null
                      ? '—'
                      : `${row.frt_ms.toLocaleString()} ms`}
                  </TableCell>
                  <TableCell className='min-w-64 align-top whitespace-normal'>
                    <details>
                      <summary className='cursor-pointer'>
                        {row.attempts === 0
                          ? t('Upstream not called')
                          : `${t('Attempts')}: ${row.attempts}`}
                      </summary>
                      <div className='mt-2 space-y-2 text-xs'>
                        {row.request_id && (
                          <Link
                            to='/usage-logs/$section'
                            params={{ section: 'common' }}
                            search={{
                              requestId: row.request_id,
                              ...requestLogTimeRange(
                                props.startTimestamp,
                                props.endTimestamp,
                                row.attempt_logs
                              ),
                            }}
                            className='text-primary break-all underline'
                          >
                            {row.request_id}
                          </Link>
                        )}
                        <p>
                          {t('Input Tokens')}:{' '}
                          {row.input_tokens.toLocaleString()} ·{' '}
                          {t('Output Tokens')}:{' '}
                          {row.output_tokens.toLocaleString()}
                        </p>
                        <p>
                          {t('Cache Read Tokens')}:{' '}
                          {row.cache_usage_reported
                            ? row.cache_read_tokens.toLocaleString()
                            : '—'}{' '}
                          · {t('Cache Write Tokens')}:{' '}
                          {row.cache_usage_reported
                            ? row.cache_write_tokens.toLocaleString()
                            : '—'}
                        </p>
                        <p>
                          {t('Duration')}:{' '}
                          {row.duration_ms == null
                            ? '—'
                            : `${row.duration_ms.toLocaleString()} ms`}
                        </p>
                        {row.error_code && (
                          <p className='text-destructive break-all'>
                            {row.error_code}
                          </p>
                        )}
                        {row.routing_rule_id && (
                          <p className='break-all'>
                            {t('Routing rule')}: {row.routing_rule_id}
                          </p>
                        )}
                        <ol className='list-inside list-decimal space-y-1'>
                          {row.attempt_logs.map((attempt, attemptIndex) => (
                            // Attempts are immutable within a request snapshot and have no portable database ID.
                            // eslint-disable-next-line react/no-array-index-key
                            <li key={attemptIndex}>
                              {dayjs
                                .unix(attempt.created_at)
                                .format('HH:mm:ss')}{' '}
                              · #{attempt.channel_id || '—'} ·{' '}
                              {attempt.status_code || '—'}{' '}
                              {attempt.error_code ||
                                (attempt.type === 'consume'
                                  ? t('Usage recorded')
                                  : t('Failed'))}
                            </li>
                          ))}
                        </ol>
                      </div>
                    </details>
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
        <div className='mt-4 flex items-center justify-end gap-3'>
          <span className='text-muted-foreground text-sm'>
            {totalPages == null
              ? `${t('Page')} ${rows.p}`
              : t('Page {{page}} of {{total}}', {
                  page: rows.p,
                  total: totalPages,
                })}
          </span>
          <Button
            variant='outline'
            disabled={props.loading || rows.p <= 1}
            onClick={() => props.onPageChange(rows.p - 1)}
          >
            {t('Previous')}
          </Button>
          <Button
            variant='outline'
            disabled={props.loading || !hasNext}
            onClick={() => props.onPageChange(rows.p + 1)}
          >
            {t('Next')}
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
