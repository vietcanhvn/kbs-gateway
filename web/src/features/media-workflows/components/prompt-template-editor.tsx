import { useTranslation } from 'react-i18next'

import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'

import type { PromptTemplate } from '../types'

const TAG_KINDS = ['image', 'video', 'audio'] as const

export function PromptTemplateEditor(props: {
  value: PromptTemplate
  onChange: (value: PromptTemplate) => void
}) {
  const { t } = useTranslation()
  const tags = props.value.reference_tags ?? {}

  const setTag = (kind: string, format: string) => {
    const next = { ...tags }
    if (format.trim() === '') {
      delete next[kind]
    } else {
      next[kind] = format
    }
    props.onChange({
      ...props.value,
      reference_tags: Object.keys(next).length > 0 ? next : undefined,
    })
  }

  return (
    <div className='flex flex-col gap-3'>
      <p className='text-muted-foreground text-xs'>
        {t('Leave empty to send the KSB prompt unchanged.')}
      </p>
      <div className='flex flex-col gap-1'>
        <Label>{t('Prefix')}</Label>
        <Textarea
          rows={2}
          value={props.value.prefix ?? ''}
          onChange={(event) =>
            props.onChange({ ...props.value, prefix: event.target.value })
          }
        />
      </div>
      <div className='flex flex-col gap-1'>
        <Label>{t('Template (use {{token}})', { token: '{{prompt}}' })}</Label>
        <Textarea
          rows={3}
          placeholder='{{prompt}}'
          value={props.value.template ?? ''}
          onChange={(event) =>
            props.onChange({ ...props.value, template: event.target.value })
          }
        />
      </div>
      <div className='flex flex-col gap-1'>
        <Label>{t('Suffix')}</Label>
        <Textarea
          rows={2}
          value={props.value.suffix ?? ''}
          onChange={(event) =>
            props.onChange({ ...props.value, suffix: event.target.value })
          }
        />
      </div>
      <div className='flex flex-col gap-1'>
        <Label>{t('Rewrite KSB reference tags ({n} = number)')}</Label>
        <div className='grid grid-cols-1 gap-2 sm:grid-cols-3'>
          {TAG_KINDS.map((kind) => (
            <div key={kind} className='flex flex-col gap-1'>
              <span className='text-muted-foreground font-mono text-xs'>
                @{kind}1 →
              </span>
              <Input
                placeholder={kind === 'image' ? '<Picture {n}>' : `@${kind}{n}`}
                value={tags[kind] ?? ''}
                onChange={(event) => setTag(kind, event.target.value)}
              />
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
