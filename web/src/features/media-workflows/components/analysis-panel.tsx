import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'

import { roleLabelKey } from '../lib'
import type { WorkflowAnalysis } from '../types'

export function AnalysisPanel(props: { analysis: WorkflowAnalysis }) {
  const { t } = useTranslation()
  // Older saved analyses can carry null lists (Go nil slices).
  const analysis = {
    ...props.analysis,
    nodes: props.analysis.nodes ?? [],
    models: props.analysis.models ?? [],
    packages: props.analysis.packages ?? [],
    inputs: props.analysis.inputs ?? [],
    outputs: props.analysis.outputs ?? [],
  }
  const activeModels = analysis.models.filter((item) => item.active)
  const disabledModels = analysis.models.filter((item) => !item.active)
  // filter() returns a new array, so sorting it does not touch the prop.
  // oxlint-disable-next-line unicorn/no-array-sort
  const disabledInputs = analysis.inputs
    .filter((item) => !item.active)
    .sort((a, b) => a.role.localeCompare(b.role))

  return (
    <div className='flex flex-col gap-4 text-sm'>
      <div className='flex flex-wrap gap-2'>
        <Badge variant='secondary'>
          {t('{{count}} active nodes', { count: analysis.active_nodes })}
        </Badge>
        <Badge variant='outline'>
          {t('{{count}} disabled nodes', { count: analysis.disabled_nodes })}
        </Badge>
        {analysis.media_kind && (
          <Badge variant='default'>
            {t('Output')}: {t(roleLabelKey(analysis.media_kind))}
          </Badge>
        )}
      </div>

      {(analysis.warnings ?? []).map((warning) => (
        <Alert key={warning}>
          <AlertDescription>{t(warning)}</AlertDescription>
        </Alert>
      ))}

      <Alert variant={activeModels.length > 0 ? 'destructive' : 'default'}>
        <AlertTitle>
          {t('Model files this workflow needs ({{count}})', {
            count: activeModels.length,
          })}
        </AlertTitle>
        <AlertDescription>
          <p className='mb-1'>
            {t(
              'Each file must exist in the RunningHub account that owns the API key (RunningHub public library, shared by the author, or uploaded by you).'
            )}
          </p>
          <ul className='list-disc pl-5 font-mono text-xs'>
            {activeModels.map((item) => (
              <li key={`${item.node_id}-${item.name}`}>
                {item.name}{' '}
                <span className='text-muted-foreground'>
                  ({item.class_type} #{item.node_id})
                </span>
              </li>
            ))}
          </ul>
          {disabledModels.length > 0 && (
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('Only used by disabled nodes')}:{' '}
              {disabledModels.map((item) => item.name).join(', ')}
            </p>
          )}
        </AlertDescription>
      </Alert>

      {analysis.packages.length > 0 && (
        <div>
          <h4 className='mb-1 font-medium'>{t('Custom node packages')}</h4>
          <ul className='list-disc pl-5 text-xs'>
            {analysis.packages.map((pkg) => (
              <li key={pkg.name}>
                <span className='font-mono'>{pkg.name}</span>
                {!pkg.active && (
                  <span className='text-muted-foreground'>
                    {' '}
                    ({t('disabled nodes only')})
                  </span>
                )}
                <span className='text-muted-foreground'>
                  {' '}
                  — {pkg.node_types.join(', ')}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {disabledInputs.length > 0 && (
        <div>
          <h4 className='mb-1 font-medium'>
            {t('Available but disabled inputs')}
          </h4>
          <p className='text-muted-foreground mb-1 text-xs'>
            {t(
              'These loaders are bypassed in the workflow, so the API cannot use them. To use them: enable the nodes in the RunningHub editor, run once, save, then export the API JSON again.'
            )}
          </p>
          <div className='flex flex-wrap gap-1'>
            {disabledInputs.map((item) => (
              <Badge key={item.node_id} variant='outline'>
                {t(roleLabelKey(item.role))} #{item.node_id}
              </Badge>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
