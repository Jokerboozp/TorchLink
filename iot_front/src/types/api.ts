// Response shapes of the platform API, field for field as the Go handlers
// write them. Only fields the front end reads are listed.

/** A paged list (internal/httpapi/pagination.go writeList). */
export interface ListResponse<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
  pagination: { page: number; pageSize: number; total: number }
  /** Raw message listing only: the total stopped at 10001. */
  totalCapped?: boolean
  /** Raw message listing only: an unfiltered query read the last 7 days. */
  window?: { start: number; defaulted: boolean }
}

/** Token usage reported on run.completed / run.failed (model.AIUsage). */
export interface AIUsage {
  inputTokens: number
  outputTokens: number
  cacheReadTokens: number
  reasoningTokens: number
}

/** One server-sent event of an AI workflow run (ports.AIWorkflowEvent). */
export interface AIWorkflowEvent {
  type: string
  runId?: string
  workflowId?: string
  workflowVersion?: string
  model?: string
  text?: string
  delta?: string
  answer?: string
  message?: string
  tool?: string
  callId?: string
  status?: string
  success?: boolean
  code?: string
  data?: Record<string, unknown>
  usage?: AIUsage
  toolCalls?: number
}

export type LiveVideoState = 'not_deployed' | 'misconfigured' | 'disabled' | 'enabled' | 'degraded'

/** Live video module status (video.Status), wrapped by GET /api/v1/video/status. */
export interface LiveVideoStatus {
  state: LiveVideoState
  deployed: boolean
  enabled: boolean
  mediaHealthy: boolean
  activeMediaServer?: string
  message: string
  transcodeAvailable: boolean
  updatedBy?: string
  updatedAt?: number
  brands?: Record<string, unknown>[]
  profiles?: Record<string, unknown>[]
  activeSessions: number
  activeSources: number
  activeTranscodes: number
  maxTranscodes?: number
  heartbeatSeconds?: number
  gb28181?: Record<string, unknown>
}

export interface LiveVideoStatusResponse {
  status: LiveVideoStatus
  canWatch: boolean
  canManageModule: boolean
}

/** GET /api/v1/events: alarms and device states changed since the cursor. */
export interface EventSnapshot {
  alarms: Record<string, unknown>[]
  devices: Record<string, unknown>[]
  deviceTotal: number
  permissions: string[]
  accessVersion: number
  /** True when only rows changed since the request cursor are included. */
  delta: boolean
  truncated: boolean
  snapshotLimit: number
  cursor: string
}

/** RFC 7807 style error body written by problem() and problemCode(). */
export interface ProblemBody {
  type?: string
  title?: string
  status?: number
  detail?: string
  /** ROLE_DENIED, DEVICE_SCOPE_DENIED, AI_REQUEST_REJECTED and other codes. */
  code?: string
  /** Reference of a logged server error (failure()). */
  traceId?: string
}
