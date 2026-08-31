import { ExternalLink } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'

import type {
  OfficialPriceOption,
  OfficialPricingDraft,
} from './official-pricing-config'

export function OfficialPricingEditor(props: {
  draft: OfficialPricingDraft
  options: OfficialPriceOption[]
  error: string | null
  onChange: (draft: OfficialPricingDraft) => void
}) {
  const { t } = useTranslation()
  const knownKeys = new Set(props.options.map((option) => option.key))
  const options = [
    ...props.options,
    ...Object.keys(props.draft.items)
      .filter((key) => !knownKeys.has(key))
      .map((key) => ({ key, labelParts: [key] })),
  ]

  return (
    <div className='space-y-5 border-t pt-5'>
      <div>
        <h4 className='text-sm font-medium'>{t('Official pricing display')}</h4>
        <p className='text-muted-foreground mt-1 text-xs leading-5'>
          {t(
            'Official prices are display-only and never participate in billing.'
          )}
        </p>
      </div>

      {props.error && (
        <Alert variant='destructive'>
          <AlertDescription>{t(props.error)}</AlertDescription>
        </Alert>
      )}

      {options.length === 0 ? (
        <p className='text-muted-foreground text-sm'>
          {t('This pricing mode has no directly comparable price items.')}
        </p>
      ) : (
        <div className='space-y-3'>
          {options.map((option) => (
            <label
              key={option.key}
              className='grid gap-2 sm:grid-cols-[minmax(0,1fr)_180px] sm:items-center'
            >
              <span className='min-w-0'>
                <span className='block text-sm'>
                  {option.labelParts.map((part) => t(part)).join(' · ')}
                </span>
                <span className='text-muted-foreground block truncate font-mono text-[11px]'>
                  {option.key}
                </span>
              </span>
              <Input
                inputMode='decimal'
                placeholder={t('Official price')}
                value={props.draft.items[option.key] || ''}
                onChange={(event) =>
                  props.onChange({
                    ...props.draft,
                    items: {
                      ...props.draft.items,
                      [option.key]: event.target.value,
                    },
                  })
                }
              />
            </label>
          ))}
        </div>
      )}

      <div className='grid gap-4 sm:grid-cols-2'>
        <label className='space-y-2 text-sm'>
          <span>{t('Official currency')}</span>
          <Select
            value={props.draft.currency}
            onValueChange={(currency) => {
              if (currency) props.onChange({ ...props.draft, currency })
            }}
          >
            <SelectTrigger className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='USD'>USD</SelectItem>
              <SelectItem value='CNY'>CNY</SelectItem>
            </SelectContent>
          </Select>
        </label>
        <label className='space-y-2 text-sm'>
          <span>{t('Price verified date')}</span>
          <Input
            type='date'
            value={props.draft.verifiedAt}
            onChange={(event) =>
              props.onChange({
                ...props.draft,
                verifiedAt: event.target.value,
              })
            }
          />
        </label>
      </div>

      <label className='space-y-2 text-sm'>
        <span className='flex items-center gap-1.5'>
          {t('Official price source')}
          <ExternalLink className='h-3.5 w-3.5' />
        </span>
        <Input
          type='url'
          placeholder='https://provider.example/pricing'
          value={props.draft.sourceUrl}
          onChange={(event) =>
            props.onChange({ ...props.draft, sourceUrl: event.target.value })
          }
        />
      </label>

      <label className='space-y-2 text-sm'>
        <span>{t('Pricing notes')}</span>
        <Textarea
          rows={5}
          placeholder={t('Markdown notes shown below the price comparison.')}
          value={props.draft.noteMarkdown}
          onChange={(event) =>
            props.onChange({
              ...props.draft,
              noteMarkdown: event.target.value,
            })
          }
        />
      </label>
    </div>
  )
}
