import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import {
  deleteMediaWorkflow,
  enableMediaWorkflow,
  getRunningHubAccount,
  listMediaWorkflows,
  mediaWorkflowQueryKeys,
} from './api'
import { WorkflowEditorSheet } from './components/workflow-editor-sheet'
import { roleLabelKey } from './lib'
import type { MediaWorkflowSummary } from './types'

function AccountLine() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: mediaWorkflowQueryKeys.account(),
    queryFn: getRunningHubAccount,
    staleTime: 60 * 1000,
  })
  const status = query.data?.data
  if (!status) {
    return null
  }
  if (!status.configured) {
    return (
      <p className='text-muted-foreground text-sm'>
        {t(
          'No RunningHub channel with an API key yet. Add one under Channels (type RunningHub).'
        )}
      </p>
    )
  }
  if (!status.account) {
    return <p className='text-destructive text-sm'>{status.error}</p>
  }
  return (
    <p className='text-muted-foreground text-sm'>
      {t(
        'RunningHub account ({{channel}}): {{coins}} coins, {{money}} {{currency}}, {{tasks}} running',
        {
          channel: status.channel_name,
          coins: status.account.remain_coins || '0',
          money: status.account.remain_money || '0',
          currency: status.account.currency,
          tasks: status.account.current_task_counts || '0',
        }
      )}
    </p>
  )
}

export function MediaWorkflowsSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<number | 'new' | null>(null)
  const [deleting, setDeleting] = useState<MediaWorkflowSummary | null>(null)

  const listQuery = useQuery({
    queryKey: mediaWorkflowQueryKeys.list(),
    queryFn: listMediaWorkflows,
  })
  const items = listQuery.data?.data ?? []

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: mediaWorkflowQueryKeys.all })

  const toggleMutation = useMutation({
    mutationFn: (item: MediaWorkflowSummary) =>
      enableMediaWorkflow(item.id, !item.enabled),
    onSuccess: (result) => {
      if (!result.success) {
        return
      }
      const channels = result.data?.channels_updated ?? []
      if (channels.length > 0) {
        toast.success(
          t('Model added to channels: {{names}}', {
            names: channels.join(', '),
          })
        )
      }
      void invalidate()
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => deleteMediaWorkflow(id),
    onSuccess: (result) => {
      if (!result.success) {
        return
      }
      setDeleting(null)
      void invalidate()
    },
  })

  return (
    <div className='flex h-full min-h-0 flex-col gap-3'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <AccountLine />
        <Button size='sm' onClick={() => setEditing('new')}>
          <Plus className='h-4 w-4' />
          {t('Add workflow')}
        </Button>
      </div>
      <div className='min-h-0 flex-1 overflow-auto rounded-lg border'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Model name')}</TableHead>
              <TableHead>{t('Title')}</TableHead>
              <TableHead>{t('Type')}</TableHead>
              <TableHead>{t('Workflow ID')}</TableHead>
              <TableHead>{t('Enabled')}</TableHead>
              <TableHead className='text-right'>{t('Actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={6}
                  className='text-muted-foreground py-8 text-center'
                >
                  {listQuery.isLoading
                    ? t('Loading...')
                    : t(
                        'No workflows yet. Paste a RunningHub link to add one.'
                      )}
                </TableCell>
              </TableRow>
            )}
            {items.map((item) => (
              <TableRow key={item.id}>
                <TableCell className='font-mono text-xs'>
                  {item.model_name}
                </TableCell>
                <TableCell>{item.title}</TableCell>
                <TableCell>
                  <Badge variant='secondary'>
                    {t(roleLabelKey(item.media_kind))}
                  </Badge>
                </TableCell>
                <TableCell className='font-mono text-xs'>
                  {item.source_url ? (
                    <a
                      href={item.source_url}
                      target='_blank'
                      rel='noreferrer'
                      className='underline'
                    >
                      {item.workflow_id}
                    </a>
                  ) : (
                    item.workflow_id
                  )}
                </TableCell>
                <TableCell>
                  <Switch
                    checked={item.enabled}
                    disabled={toggleMutation.isPending}
                    onCheckedChange={() => toggleMutation.mutate(item)}
                    aria-label={t('Enabled')}
                  />
                </TableCell>
                <TableCell className='space-x-2 text-right'>
                  <Button
                    size='sm'
                    variant='outline'
                    onClick={() => setEditing(item.id)}
                  >
                    {t('Edit')}
                  </Button>
                  <Button
                    size='sm'
                    variant='ghost'
                    onClick={() => setDeleting(item)}
                  >
                    {t('Delete')}
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      {editing !== null && (
        <WorkflowEditorSheet
          workflowId={editing === 'new' ? null : editing}
          onClose={() => {
            setEditing(null)
            void invalidate()
          }}
        />
      )}
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={t('Delete workflow')}
        desc={t('Delete {{name}}? Requests for this model will stop working.', {
          name: deleting?.model_name ?? '',
        })}
        destructive
        isLoading={deleteMutation.isPending}
        handleConfirm={() => deleting && deleteMutation.mutate(deleting.id)}
      />
    </div>
  )
}
