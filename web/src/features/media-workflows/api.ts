import { api } from '@/lib/api'

import type {
  AnalyzeResult,
  ApiResult,
  FetchResult,
  MediaWorkflow,
  MediaWorkflowPayload,
  MediaWorkflowSummary,
  RunningHubAccountStatus,
  TestRunStart,
  TestRunStatus,
} from './types'

const BASE = '/api/media_workflow'

export const mediaWorkflowQueryKeys = {
  all: ['media-workflows'] as const,
  list: () => [...mediaWorkflowQueryKeys.all, 'list'] as const,
  detail: (id: number) =>
    [...mediaWorkflowQueryKeys.all, 'detail', id] as const,
  account: () => [...mediaWorkflowQueryKeys.all, 'account'] as const,
}

export async function listMediaWorkflows(): Promise<
  ApiResult<MediaWorkflowSummary[]>
> {
  const res = await api.get(`${BASE}/`)
  return res.data
}

export async function getMediaWorkflow(
  id: number
): Promise<ApiResult<MediaWorkflow>> {
  const res = await api.get(`${BASE}/${id}`)
  return res.data
}

export async function analyzeMediaWorkflow(
  apiJson: string,
  uiJson: string
): Promise<ApiResult<AnalyzeResult>> {
  const res = await api.post(`${BASE}/analyze`, {
    api_json: apiJson,
    ui_json: uiJson,
  })
  return res.data
}

export async function fetchMediaWorkflow(
  link: string
): Promise<ApiResult<FetchResult>> {
  const res = await api.post(`${BASE}/fetch`, { link })
  return res.data
}

export async function saveMediaWorkflow(
  payload: MediaWorkflowPayload
): Promise<ApiResult<MediaWorkflow>> {
  const res = payload.id
    ? await api.put(`${BASE}/`, payload)
    : await api.post(`${BASE}/`, payload)
  return res.data
}

export async function deleteMediaWorkflow(
  id: number
): Promise<ApiResult<null>> {
  const res = await api.delete(`${BASE}/${id}`)
  return res.data
}

export async function enableMediaWorkflow(
  id: number,
  enabled: boolean
): Promise<ApiResult<{ enabled: boolean; channels_updated: string[] }>> {
  const res = await api.post(`${BASE}/${id}/enable`, { enabled })
  return res.data
}

export async function startTestRun(
  id: number,
  body: { prompt: string; images: string[]; duration?: number; ratio?: string }
): Promise<ApiResult<TestRunStart>> {
  const res = await api.post(`${BASE}/${id}/test`, body)
  return res.data
}

export async function getTestRunStatus(
  id: number,
  channelId: number,
  taskId: string
): Promise<ApiResult<TestRunStatus>> {
  const res = await api.get(`${BASE}/${id}/test`, {
    params: { channel_id: channelId, task_id: taskId },
    skipBusinessError: true,
  })
  return res.data
}

export async function getRunningHubAccount(): Promise<
  ApiResult<RunningHubAccountStatus>
> {
  const res = await api.get(`${BASE}/account`)
  return res.data
}
