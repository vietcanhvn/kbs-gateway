import { useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'

import { getTestRunStatus, startTestRun } from '../api'
import type { MediaKind, TestRunStart } from '../types'

const FINISHED = new Set(['SUCCESS', 'FAILED'])

export function TestRunPanel(props: {
  workflowId: number
  mediaKind: MediaKind
}) {
  const { t } = useTranslation()
  const [prompt, setPrompt] = useState('')
  const [images, setImages] = useState('')
  const [duration, setDuration] = useState('')
  const [ratio, setRatio] = useState('')
  const [run, setRun] = useState<TestRunStart | null>(null)
  const [starting, setStarting] = useState(false)

  const statusQuery = useQuery({
    queryKey: ['media-workflows', 'test', run?.task_id],
    enabled: run !== null,
    queryFn: () =>
      run
        ? getTestRunStatus(props.workflowId, run.channel_id, run.task_id)
        : Promise.reject(new Error('no test run')),
    refetchInterval: (query) =>
      FINISHED.has(query.state.data?.data?.status ?? '') ? false : 5000,
  })
  const status = statusQuery.data?.data

  const start = async () => {
    setStarting(true)
    try {
      const result = await startTestRun(props.workflowId, {
        prompt,
        images: images
          .split('\n')
          .map((line) => line.trim())
          .filter(Boolean),
        duration: duration ? Number(duration) : undefined,
        ratio: ratio || undefined,
      })
      if (!result.success || !result.data) {
        return
      }
      setRun(result.data)
    } finally {
      setStarting(false)
    }
  }

  const resultUrl = status?.result?.fileUrl
  return (
    <div className='flex flex-col gap-3'>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Runs one real task on RunningHub with the channel key (RunningHub charges the account; no KBS billing).'
        )}
      </p>
      <div className='flex flex-col gap-1'>
        <Label>{t('Prompt')}</Label>
        <Textarea
          rows={3}
          value={prompt}
          onChange={(event) => setPrompt(event.target.value)}
        />
      </div>
      <div className='flex flex-col gap-1'>
        <Label>{t('Image URLs (one per line)')}</Label>
        <Textarea
          rows={2}
          className='font-mono text-xs'
          value={images}
          onChange={(event) => setImages(event.target.value)}
        />
      </div>
      <div className='grid grid-cols-2 gap-2'>
        <Input
          type='number'
          placeholder={t('Seconds')}
          value={duration}
          onChange={(event) => setDuration(event.target.value)}
        />
        <Input
          placeholder={t('Ratio, e.g. 9:16')}
          value={ratio}
          onChange={(event) => setRatio(event.target.value)}
        />
      </div>
      <Button
        size='sm'
        className='self-start'
        disabled={starting}
        onClick={start}
      >
        {starting && <Loader2 className='h-4 w-4 animate-spin' />}
        {t('Test run')}
      </Button>

      {run && (
        <div className='flex flex-col gap-2 rounded-lg border p-3 text-sm'>
          <div>
            {t('Task')} <span className='font-mono'>{run.task_id}</span>:{' '}
            <b>{status?.status ?? run.task_status}</b>
            {!FINISHED.has(status?.status ?? '') && (
              <Loader2 className='ml-2 inline h-4 w-4 animate-spin' />
            )}
          </div>
          {statusQuery.data && !statusQuery.data.success && (
            <Alert variant='destructive'>
              <AlertDescription>{statusQuery.data.message}</AlertDescription>
            </Alert>
          )}
          {status?.error && (
            <Alert variant='destructive'>
              <AlertDescription>{status.error}</AlertDescription>
            </Alert>
          )}
          {resultUrl && props.mediaKind === 'video' && (
            <video
              src={resultUrl}
              controls
              className='max-h-80 w-full rounded'
            />
          )}
          {resultUrl && props.mediaKind === 'image' && (
            <img
              src={resultUrl}
              alt={t('Result')}
              className='max-h-80 rounded object-contain'
            />
          )}
          {resultUrl && props.mediaKind === 'audio' && (
            <audio src={resultUrl} controls />
          )}
          {resultUrl && (
            <a
              href={resultUrl}
              target='_blank'
              rel='noreferrer'
              className='text-xs underline'
            >
              {t('Open result')} (
              {status?.result?.taskCostTime
                ? `${status.result.taskCostTime}s, `
                : ''}
              {status?.result?.consumeCoins
                ? t('{{coins}} coins', { coins: status.result.consumeCoins })
                : ''}
              )
            </a>
          )}
        </div>
      )}
    </div>
  )
}
