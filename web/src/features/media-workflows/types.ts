export type MediaKind = 'image' | 'video' | 'audio'

export type InputRole =
  | 'prompt'
  | 'negative_prompt'
  | 'image'
  | 'video'
  | 'audio'
  | 'last_frame'
  | 'duration'
  | 'aspect_ratio'
  | 'width'
  | 'height'
  | 'seed'
  | 'fixed'

export interface InputBinding {
  role: InputRole
  index?: number
  node_id: string
  field: string
  required?: boolean
  min?: number
  max?: number
  value_type?: 'int' | 'float' | 'string'
  enum?: Record<string, string>
  value?: unknown
}

export interface InputMapping {
  inputs: InputBinding[]
  billing?: 'per_call' | 'per_second'
  instance_type?: string
}

export interface PromptTemplate {
  template?: string
  prefix?: string
  suffix?: string
  reference_tags?: Record<string, string>
}

export interface AnalysisNode {
  id: string
  class_type: string
  title?: string
  state: 'active' | 'bypassed' | 'muted' | 'virtual'
  package?: string
}

export interface AnalysisModelFile {
  node_id: string
  class_type: string
  field: string
  name: string
  active: boolean
}

export interface AnalysisPackage {
  name: string
  node_types: string[]
  active: boolean
}

export interface AnalysisInput {
  role: string
  index: number
  node_id: string
  class_type: string
  title?: string
  field: string
  current_value?: unknown
  active: boolean
}

export interface AnalysisOutput {
  node_id: string
  class_type: string
  title?: string
  kind: MediaKind
  active: boolean
}

export interface WorkflowAnalysis {
  active_nodes: number
  disabled_nodes: number
  editor_nodes?: number
  media_kind?: MediaKind
  nodes: AnalysisNode[]
  models: AnalysisModelFile[]
  packages: AnalysisPackage[]
  inputs: AnalysisInput[]
  outputs: AnalysisOutput[]
  warnings?: string[]
}

export interface AnalyzeResult {
  analysis: WorkflowAnalysis
  suggested_mapping: InputMapping
  suggested_output_nodes: string[]
}

export interface MediaWorkflowSummary {
  id: number
  model_name: string
  title: string
  source_url: string
  workflow_id: string
  media_kind: MediaKind
  executor: string
  enabled: boolean
  updated_time: number
}

export interface MediaWorkflow extends MediaWorkflowSummary {
  api_json: string
  has_ui_json: boolean
  analysis: WorkflowAnalysis | null
  input_mapping: InputMapping
  output_nodes: string[]
  prompt_template: PromptTemplate
}

export interface MediaWorkflowPayload {
  id?: number
  model_name: string
  title: string
  source_url: string
  workflow_id: string
  media_kind: MediaKind
  executor: string
  api_json?: string
  ui_json?: string
  input_mapping: InputMapping
  output_nodes: string[]
  prompt_template: PromptTemplate
  enabled: boolean
}

export interface FetchResult {
  link: { id: string; kind: 'workflow' | 'post' | 'app' }
  channel_id?: number
  api_json?: string
  analyzed?: AnalyzeResult
  fetch_error?: string
}

export interface TestRunStart {
  channel_id: number
  task_id: string
  task_status: string
}

export interface TestRunOutput {
  fileUrl: string
  fileType: string
  nodeId: string
  taskCostTime?: string
  consumeCoins?: string
}

export interface TestRunStatus {
  status: string
  outputs?: TestRunOutput[]
  result?: TestRunOutput
  error?: string
}

export interface ApiResult<T> {
  success: boolean
  message?: string
  data?: T
}

export interface RunningHubAccountStatus {
  configured: boolean
  error?: string
  channel_id?: number
  channel_name?: string
  account?: {
    remain_coins: string
    remain_money: string
    currency: string
    current_task_counts: string
  }
}
