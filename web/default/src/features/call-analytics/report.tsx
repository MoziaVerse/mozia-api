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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { CartesianGrid, Line, LineChart, XAxis, YAxis } from 'recharts'

import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
} from '@/components/ui/chart'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import dayjs from '@/lib/dayjs'
import { formatQuota } from '@/lib/format'

import type { CallAnalytics } from './api'

export function AnalyticsReport(props: { data: CallAnalytics }) {
  const { t } = useTranslation()
  const [metric, setMetric] = useState('requests')
  const summary = props.data.summary
  const number = (value: number | null) =>
    value == null
      ? '—'
      : value.toLocaleString(
          undefined,
          value > 0 && value < 0.01
            ? { maximumSignificantDigits: 3 }
            : { maximumFractionDigits: 2 }
        )
  const percent = (value: number | null) =>
    value == null ? '—' : `${(value * 100).toFixed(2)}%`
  const milliseconds = (value: number | null) =>
    value == null ? '—' : `${number(value)} ms`
  const cards = [
    [
      t('Total Requests'),
      number(summary.requests),
      `${t('Success')}: ${number(summary.success)} · ${t('Failed')}: ${number(summary.errors)}`,
    ],
    [
      t('Success Rate'),
      percent(summary.success_rate),
      `${t('Error Rate')}: ${percent(summary.error_rate)}`,
    ],
    [
      t('Consumption'),
      formatQuota(summary.quota),
      t('Recorded usage charges; refunds are not deducted.'),
    ],
    [
      t('Input Tokens'),
      number(summary.input_tokens),
      `${t('Output Tokens')}: ${number(summary.output_tokens)}`,
    ],
    [
      t('Average RPM'),
      number(summary.avg_rpm),
      `${t('Peak RPM')}: ${number(summary.peak_rpm)}`,
    ],
    [
      t('Average TPM'),
      number(summary.avg_tpm),
      `${t('Peak TPM')}: ${number(summary.peak_tpm)}`,
    ],
    [
      t('First response latency'),
      milliseconds(summary.avg_frt_ms),
      `P95: ${milliseconds(summary.p95_frt_ms)} · ${t('Samples')}: ${number(props.data.coverage.frt_samples)}`,
    ],
    [
      t('Average Duration'),
      milliseconds(summary.avg_duration_ms),
      `P95: ${milliseconds(summary.p95_duration_ms)}`,
    ],
    [
      t('Cache Hit Rate'),
      percent(summary.cache_hit_rate),
      `${t('Reported samples')}: ${number(props.data.coverage.cache_samples)}`,
    ],
    [
      t('Cache Read Tokens'),
      number(summary.cache_read_tokens),
      `${t('Cache Write Tokens')}: ${number(summary.cache_write_tokens)}`,
    ],
    [
      t('Retried requests'),
      number(summary.retried_requests),
      `${t('Recovered requests')}: ${number(summary.recovered_requests)}`,
    ],
    [
      t('Cancelled'),
      number(summary.cancelled),
      `${t('Unknown')}: ${number(summary.unknown)}`,
    ],
  ]
  const metrics = [
    { key: 'requests', label: t('Requests') },
    { key: 'errors', label: t('Failed') },
    { key: 'tokens', label: t('Total Tokens') },
    { key: 'quota', label: t('Consumption') },
  ]
  const selectedMetric =
    metrics.find((item) => item.key === metric) ?? metrics[0]

  return (
    <>
      <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
        {cards.map(([label, value, detail]) => (
          <Card key={label} className='gap-2 py-4'>
            <CardHeader className='px-4'>
              <CardTitle className='text-muted-foreground text-sm font-medium'>
                {label}
              </CardTitle>
            </CardHeader>
            <CardContent className='space-y-1 px-4'>
              <p className='text-2xl font-semibold tabular-nums'>{value}</p>
              <p className='text-muted-foreground text-xs'>{detail}</p>
            </CardContent>
          </Card>
        ))}
      </div>
      <Card>
        <CardHeader className='flex flex-row items-center justify-between gap-2'>
          <div>
            <CardTitle>{t('Trend')}</CardTitle>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('Each point covers {{minutes}} minutes.', {
                minutes: props.data.trend_interval_seconds / 60,
              })}
            </p>
          </div>
          <NativeSelect
            aria-label={t('Metric')}
            value={metric}
            onChange={(event) => setMetric(event.target.value)}
          >
            {metrics.map((item) => (
              <NativeSelectOption key={item.key} value={item.key}>
                {item.label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </CardHeader>
        <CardContent>
          {props.data.trend.length === 0 ? (
            <p className='text-muted-foreground py-12 text-center'>
              {t('No data')}
            </p>
          ) : (
            <ChartContainer
              className='h-64 w-full'
              config={{
                [metric]: {
                  label: selectedMetric.label,
                  color: 'var(--primary)',
                },
              }}
            >
              <LineChart
                data={props.data.trend}
                accessibilityLayer
                margin={{ left: 12, right: 16, bottom: 8 }}
              >
                <CartesianGrid vertical={false} />
                <XAxis
                  dataKey='timestamp'
                  tickFormatter={(value: number) =>
                    dayjs.unix(value).format('MM-DD HH:mm')
                  }
                  minTickGap={60}
                />
                <YAxis
                  width={80}
                  tickFormatter={(value: number) =>
                    metric === 'quota'
                      ? formatQuota(value)
                      : value.toLocaleString()
                  }
                />
                <ChartTooltip
                  content={
                    <ChartTooltipContent
                      labelFormatter={(value) =>
                        dayjs.unix(Number(value)).format('YYYY-MM-DD HH:mm')
                      }
                      formatter={(value) =>
                        metric === 'quota'
                          ? formatQuota(Number(value))
                          : Number(value).toLocaleString()
                      }
                    />
                  }
                />
                <Line
                  dataKey={metric}
                  name={selectedMetric.label}
                  stroke='var(--primary)'
                  strokeWidth={2}
                  dot={false}
                  isAnimationActive={false}
                />
              </LineChart>
            </ChartContainer>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardContent>
          <Tabs defaultValue='users'>
            <TabsList>
              <TabsTrigger value='users'>{t('Users')}</TabsTrigger>
              <TabsTrigger value='channels'>{t('Channels')}</TabsTrigger>
              <TabsTrigger value='errors'>{t('Errors')}</TabsTrigger>
            </TabsList>
            <TabsContent value='users'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('User')}</TableHead>
                    <TableHead>{t('Requests')}</TableHead>
                    <TableHead>{t('Success Rate')}</TableHead>
                    <TableHead>{t('Consumption')}</TableHead>
                    <TableHead>{t('Cache Hit Rate')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {props.data.users.map((row) => (
                    <TableRow key={row.user_id}>
                      <TableCell>
                        {row.username || `#${row.user_id}`}{' '}
                        <span className='text-muted-foreground'>
                          #{row.user_id}
                        </span>
                      </TableCell>
                      <TableCell>{number(row.requests)}</TableCell>
                      <TableCell>{percent(row.success_rate)}</TableCell>
                      <TableCell>{formatQuota(row.quota)}</TableCell>
                      <TableCell>{percent(row.cache_hit_rate)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TabsContent>
            <TabsContent value='channels'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('Channel ID')}</TableHead>
                    <TableHead>{t('Requests')}</TableHead>
                    <TableHead>{t('Success Rate')}</TableHead>
                    <TableHead>{t('Consumption')}</TableHead>
                    <TableHead>{t('Cache Hit Rate')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {props.data.channels.map((row) => (
                    <TableRow key={row.channel_id}>
                      <TableCell>{row.channel_id || '—'}</TableCell>
                      <TableCell>{number(row.requests)}</TableCell>
                      <TableCell>{percent(row.success_rate)}</TableCell>
                      <TableCell>{formatQuota(row.quota)}</TableCell>
                      <TableCell>{percent(row.cache_hit_rate)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TabsContent>
            <TabsContent value='errors'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('Status Code')}</TableHead>
                    <TableHead>{t('Error Code')}</TableHead>
                    <TableHead>{t('Requests')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {props.data.errors.map((row) => (
                    <TableRow key={`${row.status_code}:${row.error_code}`}>
                      <TableCell>{row.status_code || '—'}</TableCell>
                      <TableCell className='max-w-lg break-all whitespace-normal'>
                        {row.error_code || '—'}
                      </TableCell>
                      <TableCell>{number(row.count)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TabsContent>
          </Tabs>
          <p className='text-muted-foreground mt-3 text-xs'>
            {t(
              'Channel breakdown uses the final recorded channel. Retry attempts appear in request details.'
            )}
          </p>
        </CardContent>
      </Card>
    </>
  )
}
