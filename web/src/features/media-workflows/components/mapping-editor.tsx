import { Trash2 } from 'lucide-react'
import { useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'

import { roleLabelKey } from '../lib'
import type {
  AnalysisNode,
  InputBinding,
  InputMapping,
  InputRole,
} from '../types'

const ROLES: InputRole[] = [
  'prompt',
  'negative_prompt',
  'image',
  'video',
  'audio',
  'last_frame',
  'duration',
  'aspect_ratio',
  'megapixels',
  'long_edge',
  'width',
  'height',
  'seed',
  'fixed',
  'timeline',
]

const MEDIA_ROLES = new Set<InputRole>(['image', 'video', 'audio'])

function enumToText(value?: Record<string, string>): string {
  return Object.entries(value ?? {})
    .map(([key, label]) => `${key} = ${label}`)
    .join('\n')
}

function textToEnum(text: string): Record<string, string> | undefined {
  const result: Record<string, string> = {}
  for (const line of text.split('\n')) {
    const separator = line.indexOf('=')
    if (separator <= 0) continue
    const key = line.slice(0, separator).trim()
    const label = line.slice(separator + 1).trim()
    if (key && label) result[key] = label
  }
  return Object.keys(result).length > 0 ? result : undefined
}

function optionalNumber(value: string): number | undefined {
  if (value.trim() === '') return undefined
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : undefined
}

export function MappingEditor(props: {
  mapping: InputMapping
  nodes: AnalysisNode[]
  onChange: (mapping: InputMapping) => void
}) {
  const { t } = useTranslation()
  const activeNodes = props.nodes.filter((node) => node.state === 'active')
  // Stable React keys for rows that have no ID of their own.
  const rowKeys = useRef<string[]>([])
  const nextKey = useRef(0)
  while (rowKeys.current.length < props.mapping.inputs.length) {
    nextKey.current += 1
    rowKeys.current.push(`row-${nextKey.current}`)
  }
  rowKeys.current.length = props.mapping.inputs.length

  const updateRow = (index: number, patch: Partial<InputBinding>) => {
    const inputs = props.mapping.inputs.map((row, i) =>
      i === index ? { ...row, ...patch } : row
    )
    props.onChange({ ...props.mapping, inputs })
  }
  const removeRow = (index: number) => {
    rowKeys.current.splice(index, 1)
    props.onChange({
      ...props.mapping,
      inputs: props.mapping.inputs.filter((_, i) => i !== index),
    })
  }
  const addRow = () => {
    props.onChange({
      ...props.mapping,
      inputs: [
        ...props.mapping.inputs,
        { role: 'image', node_id: '', field: 'image' },
      ],
    })
  }

  return (
    <div className='flex flex-col gap-3'>
      <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
        <div className='flex flex-col gap-1'>
          <Label>{t('Billing')}</Label>
          <NativeSelect
            className='w-full'
            value={props.mapping.billing ?? 'per_call'}
            onChange={(event) =>
              props.onChange({
                ...props.mapping,
                billing: event.target.value as InputMapping['billing'],
              })
            }
          >
            <NativeSelectOption value='per_call'>
              {t('Model price per request')}
            </NativeSelectOption>
            <NativeSelectOption value='per_second'>
              {t('Model price × seconds')}
            </NativeSelectOption>
          </NativeSelect>
        </div>
        <div className='flex flex-col gap-1'>
          <Label>{t('RunningHub GPU')}</Label>
          <NativeSelect
            className='w-full'
            value={props.mapping.instance_type ?? ''}
            onChange={(event) =>
              props.onChange({
                ...props.mapping,
                instance_type: event.target.value || undefined,
              })
            }
          >
            <NativeSelectOption value=''>
              {t('Default (24 GB)')}
            </NativeSelectOption>
            <NativeSelectOption value='plus'>
              {t('Plus (48 GB)')}
            </NativeSelectOption>
          </NativeSelect>
        </div>
      </div>

      {props.mapping.inputs.map((row, index) => (
        <div
          key={rowKeys.current[index]}
          className='flex flex-col gap-2 rounded-lg border p-3'
        >
          <div className='grid grid-cols-2 gap-2 sm:grid-cols-[1fr_4rem_1.5fr_1fr_auto]'>
            <NativeSelect
              className='w-full'
              aria-label={t('Role')}
              value={row.role}
              onChange={(event) =>
                updateRow(index, { role: event.target.value as InputRole })
              }
            >
              {ROLES.map((role) => (
                <NativeSelectOption key={role} value={role}>
                  {t(roleLabelKey(role))}
                </NativeSelectOption>
              ))}
            </NativeSelect>
            <Input
              type='number'
              min={1}
              aria-label={t('Number')}
              disabled={!MEDIA_ROLES.has(row.role)}
              value={MEDIA_ROLES.has(row.role) ? (row.index ?? 0) + 1 : ''}
              onChange={(event) =>
                updateRow(index, {
                  index: Math.max(0, Number(event.target.value) - 1),
                })
              }
            />
            <NativeSelect
              className='w-full'
              aria-label={t('Node')}
              value={row.node_id}
              onChange={(event) =>
                updateRow(index, { node_id: event.target.value })
              }
            >
              <NativeSelectOption value=''>
                {t('Select node')}
              </NativeSelectOption>
              {activeNodes.map((node) => (
                <NativeSelectOption key={node.id} value={node.id}>
                  #{node.id} {node.class_type}{' '}
                  {node.title ? `· ${node.title}` : ''}
                </NativeSelectOption>
              ))}
            </NativeSelect>
            <Input
              aria-label={t('Field')}
              placeholder={t('Field')}
              value={row.field}
              onChange={(event) =>
                updateRow(index, { field: event.target.value })
              }
            />
            <Button
              variant='ghost'
              size='icon'
              aria-label={t('Remove')}
              onClick={() => removeRow(index)}
            >
              <Trash2 className='h-4 w-4' />
            </Button>
          </div>

          {row.role === 'duration' && (
            <div className='grid grid-cols-3 gap-2'>
              <Input
                type='number'
                placeholder={t('Min seconds')}
                value={row.min ?? ''}
                onChange={(event) =>
                  updateRow(index, { min: optionalNumber(event.target.value) })
                }
              />
              <Input
                type='number'
                placeholder={t('Max seconds')}
                value={row.max ?? ''}
                onChange={(event) =>
                  updateRow(index, { max: optionalNumber(event.target.value) })
                }
              />
              <NativeSelect
                className='w-full'
                value={row.value_type ?? 'int'}
                onChange={(event) =>
                  updateRow(index, {
                    value_type: event.target
                      .value as InputBinding['value_type'],
                  })
                }
              >
                <NativeSelectOption value='int'>
                  {t('Whole number')}
                </NativeSelectOption>
                <NativeSelectOption value='float'>
                  {t('Decimal number')}
                </NativeSelectOption>
              </NativeSelect>
            </div>
          )}
          {row.role === 'aspect_ratio' && (
            <div className='flex flex-col gap-1'>
              <Label className='text-muted-foreground text-xs'>
                {t('Ratio from KSB = value the node accepts (one per line)')}
              </Label>
              <Textarea
                rows={4}
                className='font-mono text-xs'
                defaultValue={enumToText(row.enum)}
                onBlur={(event) =>
                  updateRow(index, { enum: textToEnum(event.target.value) })
                }
              />
            </div>
          )}
          {(row.role === 'megapixels' || row.role === 'long_edge') && (
            <div className='flex flex-col gap-1'>
              <Label className='text-muted-foreground text-xs'>
                {t('Value at 720p (the workflow\'s own setting)')}
              </Label>
              <Input
                inputMode='decimal'
                value={row.value === undefined ? '' : String(row.value)}
                onChange={(event) =>
                  updateRow(index, { value: event.target.value })
                }
              />
            </div>
          )}
          {row.role === 'fixed' && (
            <Input
              placeholder={t('Value')}
              value={row.value === undefined ? '' : String(row.value)}
              onChange={(event) =>
                updateRow(index, { value: event.target.value })
              }
            />
          )}
          {MEDIA_ROLES.has(row.role) && (
            <label className='flex items-center gap-2 text-xs'>
              <input
                type='checkbox'
                checked={row.required ?? false}
                onChange={(event) =>
                  updateRow(index, { required: event.target.checked })
                }
              />
              {t('Required')}
            </label>
          )}
        </div>
      ))}
      <Button
        variant='outline'
        size='sm'
        className='self-start'
        onClick={addRow}
      >
        {t('Add input')}
      </Button>
    </div>
  )
}
