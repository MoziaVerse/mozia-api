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

import { Button } from '@/components/ui/button'
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
  const [rateMode, setRateMode] = useState<'interval' | 'recent'>('interval')
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
    value == null ? '—' : `${number(value * 100)}%`
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
      `${t('Error Rate')}: ${percent(summary.error_rate)} · ${t('Cancelled')}: ${number(summary.cancelled)} · ${t('Unknown')}: ${number(summary.unknown)}`,
    ],
    [
      t('Consumption'),
      formatQuota(summary.quota),
      t('Recorded usage charges; refunds are not deducted.'),
    ],
    [
      t('Cache Hit Rate'),
      percent(summary.cache_hit_rate),
      t('Token-weighted; only reported cache usage is included.'),
    ],
  ]
  const usageDetails = [
    [t('Input Tokens'), number(summary.input_tokens)],
    [t('Output Tokens'), number(summary.output_tokens)],
    [t('Cache Read Tokens'), number(summary.cache_read_tokens)],
    [t('Cache Write Tokens'), number(summary.cache_write_tokens)],
    [t('Average Duration'), milliseconds(summary.avg_duration_ms)],
    [t('Duration P95'), milliseconds(summary.p95_duration_ms)],
    [t('Retried requests'), number(summary.retried_requests)],
    [t('Recovered requests'), number(summary.recovered_requests)],
  ]
  const rates = [
    {
      label: 'RPM',
      value: rateMode === 'interval' ? summary.avg_rpm : summary.recent_rpm,
      peak: summary.peak_rpm,
      peakLabel: t('Peak RPM'),
    },
    {
      label: 'TPM',
      value: rateMode === 'interval' ? summary.avg_tpm : summary.recent_tpm,
      peak: summary.peak_tpm,
      peakLabel: t('Peak TPM'),
    },
  ]
  const distributionRows = [
    {
      key: 'first_response_ms' as const,
      label: t('TTFT (approx.)'),
      format: milliseconds,
      unit: t('Requests'),
      description: t(
        'First non-empty stream event, not an exact token timestamp.'
      ),
    },
    {
      key: 'output_tps' as const,
      label: t('Estimated output TPS'),
      format: number,
      unit: t('Requests'),
      description: t(
        'Successful streaming output tokens per second, measured from first response to stream end.'
      ),
    },
    {
      key: 'rpm' as const,
      label: t('Active-minute RPM'),
      format: number,
      unit: t('Active minutes'),
      description: t(
        'Requests per completion-time minute; idle minutes are excluded.'
      ),
    },
    {
      key: 'tpm' as const,
      label: t('Active-minute TPM'),
      format: number,
      unit: t('Active minutes'),
      description: t(
        'Input and output tokens per completion-time minute; idle minutes are excluded.'
      ),
    },
    {
      key: 'cache_share' as const,
      label: t('Cached input share per request'),
      format: percent,
      unit: t('Requests'),
      description: t(
        'Cache-read tokens divided by input tokens for each reported request; requests have equal weight.'
      ),
    },
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
      <Card className='gap-4'>
        <CardHeader className='flex flex-wrap items-center justify-between gap-3'>
          <CardTitle>{t('Request and token rates')}</CardTitle>
          <div
            role='group'
            aria-label={t('Rate calculation window')}
            className='bg-muted flex rounded-lg p-0.5'
          >
            <Button
              type='button'
              size='sm'
              variant={rateMode === 'interval' ? 'secondary' : 'ghost'}
              aria-pressed={rateMode === 'interval'}
              onClick={() => setRateMode('interval')}
            >
              {t('Interval average')}
            </Button>
            <Button
              type='button'
              size='sm'
              variant={rateMode === 'recent' ? 'secondary' : 'ghost'}
              aria-pressed={rateMode === 'recent'}
              onClick={() => setRateMode('recent')}
            >
              {t('Final 60 seconds')}
            </Button>
          </div>
        </CardHeader>
        <CardContent className='space-y-3'>
          <dl className='grid grid-cols-2 gap-6'>
            {rates.map((rate) => (
              <div key={rate.label} className='space-y-1'>
                <dt className='text-muted-foreground text-sm'>{rate.label}</dt>
                <dd className='text-2xl font-semibold tabular-nums'>
                  {number(rate.value)}
                </dd>
                {rateMode === 'interval' && (
                  <dd className='text-muted-foreground text-xs'>
                    {rate.peakLabel}: {number(rate.peak)}
                  </dd>
                )}
              </div>
            ))}
          </dl>
          <p className='text-muted-foreground text-xs'>
            {rateMode === 'interval'
              ? t(
                  'Totals divided by all minutes in the selected interval, including idle minutes.'
                )
              : t(
                  'Counts in the 60 seconds before the selected end time. Intervals shorter than 60 seconds display —.'
                )}
          </p>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>{t('Performance distributions')}</CardTitle>
          <p className='text-muted-foreground text-xs'>
            {t(
              'P50 is the median. P95 and P99 show the upper tail. Missing samples display —.'
            )}
          </p>
        </CardHeader>
        <CardContent>
          <div className='space-y-3 md:hidden'>
            {distributionRows.map((row) => {
              const distribution = props.data.distributions[row.key]
              return (
                <div key={row.key} className='rounded-lg border p-3'>
                  <p className='font-medium'>{row.label}</p>
                  <p className='text-muted-foreground mt-1 text-xs'>
                    {row.description}
                  </p>
                  <dl className='mt-3 grid grid-cols-3 gap-2'>
                    {(['p50', 'p95', 'p99'] as const).map((quantile) => (
                      <div key={quantile}>
                        <dt className='text-muted-foreground text-xs'>
                          {quantile.toUpperCase()}
                        </dt>
                        <dd className='font-medium tabular-nums'>
                          {row.format(distribution[quantile])}
                        </dd>
                      </div>
                    ))}
                  </dl>
                  <p className='text-muted-foreground mt-2 text-xs'>
                    {t('Samples')}: {number(distribution.samples)} {row.unit}
                  </p>
                </div>
              )
            })}
          </div>
          <div className='hidden md:block'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Metric')}</TableHead>
                  <TableHead className='text-right'>P50</TableHead>
                  <TableHead className='text-right'>P95</TableHead>
                  <TableHead className='text-right'>P99</TableHead>
                  <TableHead className='text-right'>{t('Samples')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {distributionRows.map((row) => {
                  const distribution = props.data.distributions[row.key]
                  return (
                    <TableRow key={row.key}>
                      <TableCell className='max-w-72 min-w-40 whitespace-normal'>
                        <span className='font-medium'>{row.label}</span>
                        <p className='text-muted-foreground mt-1 text-xs'>
                          {row.description}
                        </p>
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {row.format(distribution.p50)}
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {row.format(distribution.p95)}
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {row.format(distribution.p99)}
                      </TableCell>
                      <TableCell className='text-right tabular-nums'>
                        {number(distribution.samples)}{' '}
                        <span className='text-muted-foreground text-xs'>
                          {row.unit}
                        </span>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>
      <details className='bg-card rounded-xl border p-4'>
        <summary className='cursor-pointer text-sm font-medium'>
          {t('Usage details')}
        </summary>
        <dl className='mt-4 grid grid-cols-2 gap-4 lg:grid-cols-4'>
          {usageDetails.map(([label, value]) => (
            <div key={label}>
              <dt className='text-muted-foreground text-xs'>{label}</dt>
              <dd className='mt-1 text-sm font-medium tabular-nums'>{value}</dd>
            </div>
          ))}
        </dl>
      </details>
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
