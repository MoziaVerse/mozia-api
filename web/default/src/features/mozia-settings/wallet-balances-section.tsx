import { RefreshCw, Search, Wallet } from 'lucide-react'
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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Spinner } from '@/components/ui/spinner'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { SettingsCard } from '@/features/system-settings/components/settings-card'
import { SettingsSection } from '@/features/system-settings/components/settings-section'
import { api } from '@/lib/api'
import { getCurrencyDisplay, getCurrencyLabel } from '@/lib/currency'
import { formatQuota, parseQuotaFromDollars } from '@/lib/format'

type QuotaSource = 'gift' | 'paid' | 'legacy'
type AdjustMode = 'add' | 'subtract' | 'set'

type MoziaWalletView = {
  user_id: number
  total: number
  sources: Record<string, number>
}

type ApiEnvelope<T> = {
  success: boolean
  message?: string
  data: T
}

const quotaSources: Array<{
  value: QuotaSource
  labelKey: string
  descriptionKey: string
}> = [
  {
    value: 'gift',
    labelKey: 'Gift quota',
    descriptionKey: 'Registration, invite, and trial credits.',
  },
  {
    value: 'paid',
    labelKey: 'Paid quota',
    descriptionKey: 'Paid top-ups, redemption codes, and SSO top-ups.',
  },
  {
    value: 'legacy',
    labelKey: 'Legacy quota',
    descriptionKey: 'Legacy balance synchronized from users.quota.',
  },
]

const adjustModes: Array<{ value: AdjustMode; labelKey: string }> = [
  { value: 'add', labelKey: 'Add' },
  { value: 'subtract', labelKey: 'Subtract' },
  { value: 'set', labelKey: 'Set balance' },
]

function getWalletSourceBalance(wallet: MoziaWalletView, source: QuotaSource) {
  return wallet.sources?.[source] ?? 0
}

async function fetchWallet(userId: number) {
  const res = await api.get<ApiEnvelope<MoziaWalletView>>(
    `/api/mozia/wallet/users/${userId}`,
    { disableDuplicate: true }
  )
  return res.data
}

async function adjustWallet(
  userId: number,
  payload:
    | { source: QuotaSource; delta: number; reason?: string }
    | { source: QuotaSource; target_balance: number; reason?: string }
) {
  const res = await api.post<ApiEnvelope<MoziaWalletView>>(
    `/api/mozia/wallet/users/${userId}/adjust`,
    payload
  )
  return res.data
}

export function MoziaWalletBalancesSection() {
  const { t } = useTranslation()
  const [userIdInput, setUserIdInput] = useState('')
  const [wallet, setWallet] = useState<MoziaWalletView | null>(null)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [source, setSource] = useState<QuotaSource>('gift')
  const [mode, setMode] = useState<AdjustMode>('add')
  const [amount, setAmount] = useState('')
  const [reason, setReason] = useState('')

  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const tokensOnly = currencyMeta.kind === 'tokens'
  const userId = Number(userIdInput)
  const amountValue = Number(amount)
  const parsedQuota = parseQuotaFromDollars(amountValue)
  const currentSourceBalance = wallet
    ? getWalletSourceBalance(wallet, source)
    : 0

  const preview = useMemo(() => {
    if (!wallet || !amount) return t('Select a user and amount to preview.')
    if (!Number.isFinite(amountValue)) return t('Enter a valid amount.')
    if (mode === 'set') {
      return `${t('Current source balance')}: ${formatQuota(currentSourceBalance)} -> ${formatQuota(parsedQuota)}`
    }
    const delta = mode === 'subtract' ? -Math.abs(parsedQuota) : parsedQuota
    return `${t('Current source balance')}: ${formatQuota(currentSourceBalance)} ${delta >= 0 ? '+' : '-'} ${formatQuota(Math.abs(parsedQuota))} = ${formatQuota(currentSourceBalance + delta)}`
  }, [amount, amountValue, currentSourceBalance, mode, parsedQuota, t, wallet])

  const loadWallet = async () => {
    if (!Number.isInteger(userId) || userId <= 0) {
      toast.error(t('Enter a valid user ID'))
      return
    }
    setLoading(true)
    try {
      const res = await fetchWallet(userId)
      if (res.success) {
        setWallet(res.data)
        toast.success(t('Wallet loaded successfully'))
      } else {
        toast.error(res.message || t('Failed to load wallet'))
      }
    } catch (error: unknown) {
      toast.error(
        error instanceof Error ? error.message : t('Failed to load wallet')
      )
    } finally {
      setLoading(false)
    }
  }

  const submitAdjustment = async () => {
    if (!wallet) {
      toast.error(t('Load a user wallet first'))
      return
    }
    if (amount.trim() === '') {
      toast.error(t('Enter an amount'))
      return
    }
    if (!Number.isFinite(amountValue)) {
      toast.error(t('Enter a valid amount'))
      return
    }
    if (mode !== 'set' && parsedQuota <= 0) {
      toast.error(t('Amount must be greater than zero'))
      return
    }
    if (mode === 'set' && parsedQuota < 0) {
      toast.error(t('Target balance cannot be negative'))
      return
    }

    setSaving(true)
    try {
      const trimmedReason = reason.trim()
      const payload =
        mode === 'set'
          ? {
              source,
              target_balance: parsedQuota,
              reason: trimmedReason,
            }
          : {
              source,
              delta: mode === 'subtract' ? -parsedQuota : parsedQuota,
              reason: trimmedReason,
            }
      const res = await adjustWallet(wallet.user_id, payload)
      if (res.success) {
        setWallet(res.data)
        setAmount('')
        setReason('')
        toast.success(t('Balance adjusted successfully'))
      } else {
        toast.error(res.message || t('Failed to adjust wallet balance'))
      }
    } catch (error: unknown) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to adjust wallet balance')
      )
    } finally {
      setSaving(false)
    }
  }

  const amountPlaceholder = tokensOnly
    ? t('Enter amount in tokens')
    : t('Enter amount in {{currency}}', { currency: currencyLabel })

  let walletLoadIcon = <Search data-icon='inline-start' />
  if (loading) {
    walletLoadIcon = <Spinner data-icon='inline-start' />
  } else if (wallet) {
    walletLoadIcon = <RefreshCw data-icon='inline-start' />
  }
  const walletLoadLabel = wallet ? t('Refresh wallet') : t('Load wallet')

  return (
    <SettingsSection title={t('User Wallet Balances')}>
      <SettingsCard
        title={t('Balance configuration notes')}
        description={t(
          'Use this page to inspect and adjust the three wallet balance sources behind the built-in user balance.'
        )}
      >
        <div className='text-muted-foreground grid gap-3 text-sm md:grid-cols-3'>
          {quotaSources.map((item) => (
            <Alert key={item.value}>
              <AlertTitle>{t(item.labelKey)}</AlertTitle>
              <AlertDescription>{t(item.descriptionKey)}</AlertDescription>
            </Alert>
          ))}
        </div>
      </SettingsCard>

      <SettingsCard
        title={t('Load user wallet')}
        description={t(
          'Enter a user ID to inspect and adjust wallet balances.'
        )}
      >
        <FieldGroup className='grid gap-3 md:grid-cols-[minmax(180px,280px)_auto]'>
          <Field>
            <FieldLabel htmlFor='mozia-wallet-user-id'>
              {t('User ID')}
            </FieldLabel>
            <Input
              id='mozia-wallet-user-id'
              type='number'
              min={1}
              value={userIdInput}
              onChange={(event) => setUserIdInput(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter') void loadWallet()
              }}
            />
          </Field>
          <Field className='justify-end md:pt-6'>
            <Button onClick={loadWallet} disabled={loading}>
              {walletLoadIcon}
              {walletLoadLabel}
            </Button>
          </Field>
        </FieldGroup>
      </SettingsCard>

      <div className='grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(340px,0.7fr)]'>
        <SettingsCard
          title={t('Current wallet balances')}
          description={
            wallet
              ? t('User {{id}}', { id: wallet.user_id })
              : t('Search by user ID to load gift, paid, and legacy balances.')
          }
        >
          {wallet ? (
            <div className='space-y-4'>
              <div className='bg-muted/30 flex flex-wrap items-center justify-between gap-2 rounded-lg border p-3'>
                <span className='text-sm font-medium'>
                  {t('Total wallet balance')}
                </span>
                <Badge variant='default'>{formatQuota(wallet.total)}</Badge>
              </div>
              <Table>
                <TableHeader>
                  <TableRow className='bg-muted/40 hover:bg-muted/40'>
                    <TableHead>{t('Balance source')}</TableHead>
                    <TableHead>{t('Purpose')}</TableHead>
                    <TableHead className='text-right'>{t('Balance')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {quotaSources.map((item) => (
                    <TableRow key={item.value}>
                      <TableCell>
                        <Badge
                          variant={
                            item.value === 'paid' ? 'default' : 'secondary'
                          }
                        >
                          {t(item.labelKey)}
                        </Badge>
                      </TableCell>
                      <TableCell className='text-muted-foreground max-w-[360px] whitespace-normal'>
                        {t(item.descriptionKey)}
                      </TableCell>
                      <TableCell className='text-right font-medium'>
                        {formatQuota(
                          getWalletSourceBalance(wallet, item.value)
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          ) : (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant='icon'>
                  <Wallet />
                </EmptyMedia>
                <EmptyTitle>{t('No wallet loaded')}</EmptyTitle>
                <EmptyDescription>
                  {t(
                    'Search by user ID to load gift, paid, and legacy balances.'
                  )}
                </EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button variant='outline' onClick={loadWallet}>
                  <Search data-icon='inline-start' />
                  {t('Load wallet')}
                </Button>
              </EmptyContent>
            </Empty>
          )}
        </SettingsCard>

        <SettingsCard
          title={t('Adjust wallet balance')}
          description={t(
            'Choose a source and operation. Adjustments are audited and keep users.quota synchronized.'
          )}
        >
          <FieldGroup className='gap-4'>
            <Field>
              <FieldLabel htmlFor='mozia-wallet-source'>
                {t('Source')}
              </FieldLabel>
              <NativeSelect
                id='mozia-wallet-source'
                className='w-full'
                value={source}
                disabled={!wallet || saving}
                onChange={(event) =>
                  setSource(event.target.value as QuotaSource)
                }
              >
                {quotaSources.map((item) => (
                  <NativeSelectOption key={item.value} value={item.value}>
                    {t(item.labelKey)}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </Field>

            <Field>
              <FieldLabel>{t('Operation')}</FieldLabel>
              <ToggleGroup
                value={[mode]}
                onValueChange={(value) => {
                  const nextMode = value.find((item) => item !== mode)
                  if (nextMode) setMode(nextMode as AdjustMode)
                }}
                aria-label={t('Operation')}
                variant='outline'
                size='sm'
                spacing={2}
                className='grid w-full grid-cols-3 gap-2'
              >
                {adjustModes.map((item) => (
                  <ToggleGroupItem
                    key={item.value}
                    value={item.value}
                    disabled={!wallet || saving}
                    className='w-full px-2'
                  >
                    <span className='truncate'>{t(item.labelKey)}</span>
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            </Field>

            <Field>
              <FieldLabel htmlFor='mozia-wallet-amount'>
                {t('Amount')}
              </FieldLabel>
              <Input
                id='mozia-wallet-amount'
                type='number'
                min={mode === 'set' ? 0 : 0.000001}
                step='any'
                value={amount}
                placeholder={amountPlaceholder}
                disabled={!wallet || saving}
                onChange={(event) => setAmount(event.target.value)}
              />
              <FieldDescription>{preview}</FieldDescription>
            </Field>

            <Field>
              <FieldLabel htmlFor='mozia-wallet-reason'>
                {t('Reason')}
              </FieldLabel>
              <Textarea
                id='mozia-wallet-reason'
                value={reason}
                placeholder={t('Optional reason for audit log')}
                disabled={!wallet || saving}
                onChange={(event) => setReason(event.target.value)}
              />
              <FieldDescription>
                {t(
                  'This keeps the built-in balance equal to gift + paid + legacy.'
                )}
              </FieldDescription>
            </Field>

            <Field orientation='horizontal' className='justify-end'>
              <FieldContent>
                <Button onClick={submitAdjustment} disabled={!wallet || saving}>
                  {saving && <Spinner data-icon='inline-start' />}
                  {t('Save adjustment')}
                </Button>
              </FieldContent>
            </Field>
          </FieldGroup>
        </SettingsCard>
      </div>
    </SettingsSection>
  )
}
