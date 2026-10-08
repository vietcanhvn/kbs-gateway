import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  SideDrawerSection,
  SideDrawerSectionHeader,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Switch } from '@/components/ui/switch'

import {
  analyzeMediaWorkflow,
  enableMediaWorkflow,
  fetchMediaWorkflow,
  getMediaWorkflow,
  mediaWorkflowQueryKeys,
  saveMediaWorkflow,
} from '../api'
import { roleLabelKey } from '../lib'
import type {
  AnalyzeResult,
  InputMapping,
  MediaKind,
  PromptTemplate,
  WorkflowAnalysis,
} from '../types'
import { AnalysisPanel } from './analysis-panel'
import { MappingEditor } from './mapping-editor'
import { PromptTemplateEditor } from './prompt-template-editor'
import { TestRunPanel } from './test-run-panel'

interface BasicFields {
  model_name: string
  title: string
  source_url: string
  workflow_id: string
  media_kind: MediaKind
  enabled: boolean
}

const EMPTY_FIELDS: BasicFields = {
  model_name: '',
  title: '',
  source_url: '',
  workflow_id: '',
  media_kind: 'video',
  enabled: false,
}

function isEditorExport(text: string): boolean {
  try {
    const parsed: unknown = JSON.parse(text)
    return (
      typeof parsed === 'object' &&
      parsed !== null &&
      Array.isArray((parsed as { nodes?: unknown }).nodes)
    )
  } catch {
    return false
  }
}

export function WorkflowEditorSheet(props: {
  workflowId: number | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [savedId, setSavedId] = useState<number | null>(props.workflowId)
  const [link, setLink] = useState('')
  const [apiJson, setApiJson] = useState('')
  const [uiJson, setUiJson] = useState<string | undefined>(undefined)
  const [hasStoredUiJson, setHasStoredUiJson] = useState(false)
  const [fields, setFields] = useState<BasicFields>(EMPTY_FIELDS)
  const [analysis, setAnalysis] = useState<WorkflowAnalysis | null>(null)
  const [suggested, setSuggested] = useState<AnalyzeResult | null>(null)
  const [mapping, setMapping] = useState<InputMapping>({ inputs: [] })
  const [outputNodes, setOutputNodes] = useState<string[]>([])
  const [template, setTemplate] = useState<PromptTemplate>({})
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState<'fetch' | 'analyze' | 'save' | null>(null)

  const existing = useQuery({
    queryKey: mediaWorkflowQueryKeys.detail(props.workflowId ?? 0),
    queryFn: () =>
      props.workflowId === null
        ? Promise.reject(new Error('no workflow'))
        : getMediaWorkflow(props.workflowId),
    enabled: props.workflowId !== null,
  })

  useEffect(() => {
    const record = existing.data?.data
    if (!record) return
    setFields({
      model_name: record.model_name,
      title: record.title,
      source_url: record.source_url,
      workflow_id: record.workflow_id,
      media_kind: record.media_kind,
      enabled: record.enabled,
    })
    setLink(record.source_url)
    setApiJson(record.api_json)
    setHasStoredUiJson(record.has_ui_json)
    setAnalysis(record.analysis)
    setMapping(
      record.input_mapping?.inputs ? record.input_mapping : { inputs: [] }
    )
    setOutputNodes(record.output_nodes ?? [])
    setTemplate(record.prompt_template ?? {})
  }, [existing.data])

  const applyAnalysis = (result: AnalyzeResult) => {
    setAnalysis(result.analysis)
    setSuggested(result)
    if (mapping.inputs.length === 0) {
      setMapping({ ...mapping, inputs: result.suggested_mapping.inputs })
      setOutputNodes(result.suggested_output_nodes)
    }
    if (result.analysis.media_kind && savedId === null) {
      setFields((current) => ({
        ...current,
        media_kind: result.analysis.media_kind as MediaKind,
      }))
    }
  }

  const fetchFromLink = async () => {
    setBusy('fetch')
    setNotice('')
    try {
      const result = await fetchMediaWorkflow(link)
      if (!result.success || !result.data) {
        return
      }
      const data = result.data
      setFields((current) => ({
        ...current,
        workflow_id: data.link.id,
        source_url: link.trim(),
      }))
      if (data.link.kind === 'post') {
        setNotice(
          t(
            'This is a community post link. Open it, clone the workflow into your RunningHub account, run it once, then paste the /workflow/ link of your copy.'
          )
        )
      }
      if (data.api_json && data.analyzed) {
        setApiJson(data.api_json)
        applyAnalysis(data.analyzed)
      } else if (data.fetch_error) {
        setNotice((current) =>
          `${current} ${t('Could not download the workflow')}: ${data.fetch_error}. ${t('Upload the exported API JSON instead.')}`.trim()
        )
      }
    } finally {
      setBusy(null)
    }
  }

  const readFiles = async (files: FileList | null) => {
    if (!files) return
    for (const file of files) {
      const text = await file.text()
      if (isEditorExport(text)) {
        setUiJson(text)
      } else {
        setApiJson(text)
      }
    }
  }

  const runAnalyze = async () => {
    setBusy('analyze')
    try {
      const result = await analyzeMediaWorkflow(apiJson, uiJson ?? '')
      if (!result.success || !result.data) {
        return
      }
      applyAnalysis(result.data)
    } finally {
      setBusy(null)
    }
  }

  const save = async () => {
    setBusy('save')
    try {
      const result = await saveMediaWorkflow({
        id: savedId ?? undefined,
        ...fields,
        executor: 'runninghub',
        api_json: apiJson,
        ui_json: uiJson,
        input_mapping: mapping,
        output_nodes: outputNodes,
        prompt_template: template,
      })
      if (!result.success || !result.data) {
        return
      }
      setSavedId(result.data.id)
      setAnalysis(result.data.analysis)
      if (fields.enabled) {
        const enabled = await enableMediaWorkflow(result.data.id, true)
        const channels = enabled.data?.channels_updated ?? []
        if (channels.length > 0) {
          toast.success(
            t('Model added to channels: {{names}}', {
              names: channels.join(', '),
            })
          )
        }
      }
      toast.success(t('Saved'))
    } finally {
      setBusy(null)
    }
  }

  const activeOutputs = (analysis?.outputs ?? []).filter(
    (output) => output.active
  )
  const toggleOutput = (nodeId: string) =>
    setOutputNodes((current) =>
      current.includes(nodeId)
        ? current.filter((id) => id !== nodeId)
        : [...current, nodeId]
    )

  return (
    <Sheet open onOpenChange={(open) => !open && props.onClose()}>
      <SheetContent className={sideDrawerContentClassName('sm:max-w-3xl')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>
            {savedId
              ? t('Edit RunningHub workflow')
              : t('Add RunningHub workflow')}
          </SheetTitle>
          <SheetDescription>
            {t(
              'Paste a RunningHub workflow link or upload the exported JSON, check the analysis, then save and enable.'
            )}
          </SheetDescription>
        </SheetHeader>
        <div className={sideDrawerFormClassName()}>
          <SideDrawerSection>
            <SideDrawerSectionHeader title={t('Source')} />
            <div className='flex gap-2'>
              <Input
                placeholder='https://www.runninghub.ai/workflow/...'
                value={link}
                onChange={(event) => setLink(event.target.value)}
              />
              <Button
                variant='outline'
                disabled={!link.trim() || busy !== null}
                onClick={fetchFromLink}
              >
                {busy === 'fetch' && (
                  <Loader2 className='h-4 w-4 animate-spin' />
                )}
                {t('Fetch')}
              </Button>
            </div>
            <div className='flex flex-col gap-1'>
              <Label>
                {t(
                  'Or upload JSON files (API export, and optionally the full workflow)'
                )}
              </Label>
              <Input
                type='file'
                accept='.json,application/json'
                multiple
                onChange={(event) => void readFiles(event.target.files)}
              />
              <p className='text-muted-foreground text-xs'>
                {apiJson ? t('API JSON loaded') : t('API JSON missing')} ·{' '}
                {uiJson || hasStoredUiJson
                  ? t('Full workflow JSON loaded')
                  : t(
                      'Full workflow JSON optional (shows disabled nodes and packages)'
                    )}
              </p>
            </div>
            <Button
              size='sm'
              className='self-start'
              disabled={!apiJson || busy !== null}
              onClick={runAnalyze}
            >
              {busy === 'analyze' && (
                <Loader2 className='h-4 w-4 animate-spin' />
              )}
              {t('Analyze')}
            </Button>
            {notice && (
              <Alert>
                <AlertDescription>{notice}</AlertDescription>
              </Alert>
            )}
          </SideDrawerSection>

          {analysis && (
            <SideDrawerSection>
              <SideDrawerSectionHeader title={t('Analysis')} />
              <AnalysisPanel analysis={analysis} />
            </SideDrawerSection>
          )}

          <SideDrawerSection>
            <SideDrawerSectionHeader title={t('Model')} />
            <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
              <div className='flex flex-col gap-1'>
                <Label>{t('Model name (used by KSB)')}</Label>
                <Input
                  placeholder='rh-h3-real-skin'
                  value={fields.model_name}
                  onChange={(event) =>
                    setFields({ ...fields, model_name: event.target.value })
                  }
                />
              </div>
              <div className='flex flex-col gap-1'>
                <Label>{t('Title')}</Label>
                <Input
                  value={fields.title}
                  onChange={(event) =>
                    setFields({ ...fields, title: event.target.value })
                  }
                />
              </div>
              <div className='flex flex-col gap-1'>
                <Label>{t('RunningHub workflow ID')}</Label>
                <Input
                  className='font-mono'
                  value={fields.workflow_id}
                  onChange={(event) =>
                    setFields({ ...fields, workflow_id: event.target.value })
                  }
                />
              </div>
              <div className='flex flex-col gap-1'>
                <Label>{t('Output type')}</Label>
                <NativeSelect
                  className='w-full'
                  value={fields.media_kind}
                  onChange={(event) =>
                    setFields({
                      ...fields,
                      media_kind: event.target.value as MediaKind,
                    })
                  }
                >
                  {(['video', 'image', 'audio'] as const).map((kind) => (
                    <NativeSelectOption key={kind} value={kind}>
                      {t(roleLabelKey(kind))}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
              </div>
            </div>
            <label className='flex items-center gap-2 text-sm'>
              <Switch
                checked={fields.enabled}
                onCheckedChange={(checked) =>
                  setFields({ ...fields, enabled: checked })
                }
              />
              {t('Enabled (adds the model to every RunningHub channel)')}
            </label>
            <p className='text-muted-foreground text-xs'>
              {t('Set the price of this model in')}{' '}
              <Link
                to='/system-settings/billing/$section'
                params={{ section: 'model-pricing' }}
                className='underline'
              >
                {t('Model Pricing')}
              </Link>
              . {t('Without a price the model cannot be used.')}
            </p>
          </SideDrawerSection>

          {analysis && (
            <SideDrawerSection>
              <SideDrawerSectionHeader title={t('Inputs')} />
              {suggested && mapping.inputs.length > 0 && (
                <Button
                  variant='ghost'
                  size='sm'
                  className='self-start'
                  onClick={() => {
                    setMapping({
                      ...mapping,
                      inputs: suggested.suggested_mapping.inputs,
                    })
                    setOutputNodes(suggested.suggested_output_nodes)
                  }}
                >
                  {t('Use suggested mapping')}
                </Button>
              )}
              <MappingEditor
                mapping={mapping}
                nodes={analysis.nodes}
                onChange={setMapping}
              />
              <div className='flex flex-col gap-1'>
                <Label>{t('Output nodes')}</Label>
                {activeOutputs.map((output) => (
                  <label
                    key={output.node_id}
                    className='flex items-center gap-2 text-sm'
                  >
                    <input
                      type='checkbox'
                      checked={outputNodes.includes(output.node_id)}
                      onChange={() => toggleOutput(output.node_id)}
                    />
                    #{output.node_id} {output.class_type} (
                    {t(roleLabelKey(output.kind))})
                  </label>
                ))}
              </div>
            </SideDrawerSection>
          )}

          <SideDrawerSection>
            <SideDrawerSectionHeader title={t('Prompt template')} />
            <PromptTemplateEditor value={template} onChange={setTemplate} />
          </SideDrawerSection>

          {savedId !== null && (
            <SideDrawerSection>
              <SideDrawerSectionHeader title={t('Test run')} />
              <TestRunPanel
                workflowId={savedId}
                mediaKind={fields.media_kind}
              />
            </SideDrawerSection>
          )}
        </div>
        <SheetFooter className={sideDrawerFooterClassName()}>
          <Button variant='outline' onClick={props.onClose}>
            {t('Close')}
          </Button>
          <Button
            disabled={!apiJson || !fields.model_name || busy !== null}
            onClick={save}
          >
            {busy === 'save' && <Loader2 className='h-4 w-4 animate-spin' />}
            {t('Save')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
