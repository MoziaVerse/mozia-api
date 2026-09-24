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
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import dayjs from '@/lib/dayjs'
import { formatTimestampForInput } from '@/lib/format'

import { getCallAnalytics, getCallAnalyticsRequests } from './api'
import {
  analyticsFilterSchema,
  analyticsQueryFilters,
  type AnalyticsFilterForm,
} from './filters'
import { AnalyticsReport } from './report'
import { AnalyticsRequests } from './requests'

export function CallAnalyticsPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [initial] = useState(() => {
    const end = Math.floor(Date.now() / 60000) * 60
    return {
      start: formatTimestampForInput(end - 86400),
      end: formatTimestampForInput(end),
      user: '',
      model: '',
      channel: '',
      outcome: '',
    } satisfies AnalyticsFilterForm
  })
  const form = useForm<AnalyticsFilterForm>({
    resolver: zodResolver(analyticsFilterSchema),
    defaultValues: initial,
  })
  const [filters, setFilters] = useState(() => analyticsQueryFilters(initial))
  const [page, setPage] = useState(1)
  const summaryQuery = useQuery({
    queryKey: ['call-analytics-summary', filters],
    queryFn: ({ signal }) => getCallAnalytics(filters, signal),
    refetchOnWindowFocus: false,
    staleTime: 60000,
    retry: false,
  })
  const requestsQuery = useQuery({
    queryKey: ['call-analytics-requests', filters, page],
    queryFn: ({ signal }) => getCallAnalyticsRequests(filters, page, signal),
    refetchOnWindowFocus: false,
    staleTime: 60000,
    retry: false,
  })
  const isFetching = summaryQuery.isFetching || requestsQuery.isFetching
  const submit = (value: AnalyticsFilterForm) => {
    const next = analyticsQueryFilters(value)
    if (JSON.stringify(filters) === JSON.stringify(next)) {
      void summaryQuery.refetch()
      void queryClient.invalidateQueries({
        queryKey: ['call-analytics-requests', filters],
        refetchType: 'none',
      })
      setPage(1)
      if (page === 1) void requestsQuery.refetch()
      return
    }
    setPage(1)
    setFilters(next)
  }
  const warnings: Record<string, string> = {
    logging_disabled: t(
      'Consumption logging is disabled. Usage and success metrics may be incomplete.'
    ),
    unknown_request_models: t(
      'Some historical requests have no reliable requested model and are excluded when a model is selected.'
    ),
    historical_outcomes_incomplete: t(
      'Historical requests may lack a final outcome. Rates are based on recorded requests and may omit early rejections.'
    ),
    cache_usage_incomplete: t(
      'Some upstreams do not report cache usage. A missing report is not proof of a cache miss.'
    ),
    completion_time_basis: t(
      'Time filters and minute buckets use request completion time.'
    ),
    legacy_missing_request_ids: t(
      'Some historical logs have no request ID and cannot be reliably grouped into requests.'
    ),
  }
  const outcomeOptions = [
    ['', t('All')],
    ['success', t('Success')],
    ['error', t('Failed')],
    ['cancelled', t('Cancelled')],
    ['unknown', t('Unknown')],
  ]

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Call Analytics')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='space-y-5 pb-6'>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Analyze recorded text requests by customer, model and time range. Metrics cover all filtered requests, not just this page.'
            )}
          </p>
          <Card>
            <CardContent>
              <form onSubmit={form.handleSubmit(submit)} className='space-y-4'>
                <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-4'>
                  <div className='space-y-2'>
                    <Label htmlFor='analytics-start'>{t('Start Time')}</Label>
                    <Input
                      id='analytics-start'
                      type='datetime-local'
                      required
                      {...form.register('start')}
                    />
                  </div>
                  <div className='space-y-2'>
                    <Label htmlFor='analytics-end'>{t('End Time')}</Label>
                    <Input
                      id='analytics-end'
                      type='datetime-local'
                      required
                      {...form.register('end')}
                    />
                  </div>
                  <div className='space-y-2'>
                    <Label htmlFor='analytics-user'>
                      {t('User ID or username')}
                    </Label>
                    <Input
                      id='analytics-user'
                      placeholder={t(
                        'Exact username or ID; leave blank for all'
                      )}
                      autoCapitalize='none'
                      spellCheck={false}
                      aria-invalid={Boolean(form.formState.errors.user)}
                      aria-describedby={
                        form.formState.errors.user
                          ? 'analytics-user-error'
                          : undefined
                      }
                      {...form.register('user')}
                    />
                  </div>
                  <div className='space-y-2'>
                    <Label htmlFor='analytics-model'>{t('Model Name')}</Label>
                    <Input
                      id='analytics-model'
                      placeholder={t('Exact model name; leave blank for all')}
                      {...form.register('model')}
                    />
                  </div>
                  <div className='space-y-2'>
                    <Label htmlFor='analytics-channel'>{t('Channel ID')}</Label>
                    <Input
                      id='analytics-channel'
                      inputMode='numeric'
                      placeholder={t('All')}
                      {...form.register('channel')}
                    />
                  </div>
                  <div className='space-y-2'>
                    <Label htmlFor='analytics-outcome'>{t('Result')}</Label>
                    <NativeSelect
                      id='analytics-outcome'
                      className='w-full'
                      {...form.register('outcome')}
                    >
                      {outcomeOptions.map(([value, label]) => (
                        <NativeSelectOption value={value} key={value}>
                          {label}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  </div>
                </div>
                <div className='flex flex-wrap items-center justify-between gap-3'>
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Maximum range: 31 days. Model names match the customer request exactly. End time is exclusive.'
                    )}
                  </p>
                  <Button type='submit' disabled={isFetching}>
                    {isFetching ? t('Loading...') : t('Analyze')}
                  </Button>
                </div>
                {Object.entries(form.formState.errors).map(([key, error]) => (
                  <p
                    key={key}
                    id={`analytics-${key}-error`}
                    role='alert'
                    className='text-destructive text-sm'
                  >
                    {t(error.message || 'Invalid input')}
                  </p>
                ))}
              </form>
            </CardContent>
          </Card>
          {summaryQuery.error && (
            <Alert variant='destructive'>
              <AlertDescription>
                {t('Failed to load call analytics')}:{' '}
                {summaryQuery.error.message}
              </AlertDescription>
            </Alert>
          )}
          {requestsQuery.error && (
            <Alert variant='destructive'>
              <AlertDescription>
                {t('Filtered requests')}: {requestsQuery.error.message}
              </AlertDescription>
            </Alert>
          )}
          <div aria-busy={summaryQuery.isFetching} className='space-y-5'>
            {summaryQuery.isFetching && (
              <p role='status' className='text-muted-foreground text-sm'>
                {t('Loading summary...')}
              </p>
            )}
            {summaryQuery.data && !summaryQuery.error && (
              <>
                <p className='text-muted-foreground text-sm'>
                  {t('Results for')}:{' '}
                  {dayjs
                    .unix(summaryQuery.data.start_timestamp)
                    .format('YYYY-MM-DD HH:mm')}{' '}
                  —{' '}
                  {dayjs
                    .unix(summaryQuery.data.end_timestamp)
                    .format('YYYY-MM-DD HH:mm')}
                </p>
                {summaryQuery.data.warnings.includes('logging_disabled') && (
                  <Alert variant='destructive'>
                    <AlertDescription>
                      {warnings.logging_disabled}
                    </AlertDescription>
                  </Alert>
                )}
                <details className='bg-card rounded-xl border px-4 py-3'>
                  <summary className='cursor-pointer text-sm font-medium'>
                    {t('Data coverage and notes')}
                    {' · '}
                    <span className='text-muted-foreground font-normal'>
                      {t('Final outcomes recorded')}:{' '}
                      {summaryQuery.data.coverage.final_recorded_requests.toLocaleString()}{' '}
                      · {t('Inferred')}:{' '}
                      {summaryQuery.data.coverage.inferred_requests.toLocaleString()}{' '}
                      {t('Unknown')}:{' '}
                      {summaryQuery.data.summary.unknown.toLocaleString()}
                    </span>
                  </summary>
                  <div className='text-muted-foreground mt-3 space-y-1 text-xs'>
                    {summaryQuery.data.warnings
                      .filter((warning) => warning !== 'logging_disabled')
                      .map((warning) => (
                        <p key={warning}>{warnings[warning] ?? warning}</p>
                      ))}
                  </div>
                </details>
                <AnalyticsReport data={summaryQuery.data} />
              </>
            )}
          </div>
          <div aria-busy={requestsQuery.isFetching} className='space-y-3'>
            {requestsQuery.isFetching && (
              <p role='status' className='text-muted-foreground text-sm'>
                {t('Loading requests...')}
              </p>
            )}
            {requestsQuery.data && (
              <AnalyticsRequests
                data={requestsQuery.data}
                total={
                  summaryQuery.isSuccess
                    ? summaryQuery.data.summary.requests
                    : null
                }
                startTimestamp={filters.start_timestamp}
                endTimestamp={filters.end_timestamp}
                loading={requestsQuery.isFetching}
                onPageChange={setPage}
              />
            )}
          </div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
