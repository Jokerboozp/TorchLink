// 带进度的上传与下载：认证头、错误信息与 api 一致，进度只反映实际传输的字节。
import { ApiError, apiResponse, saveBlob, session } from './api'
import type { ApiErrorDetails } from './api'
import { errorMessage } from './presentation'

export type ProgressHandler = (loaded: number, total: number) => void

/**
 * Downloads a file with the session credentials, reporting received bytes as
 * onProgress(loaded, total); total comes from Content-Length and is 0 when the
 * server does not send it. The file is handed to the browser once complete.
 */
export async function downloadWithProgress(
  path: string,
  filename: string,
  onProgress: ProgressHandler = () => {},
  options: RequestInit = {}
) {
  const response = await apiResponse(path, options)
  const total = Number(response.headers.get('Content-Length') || 0)
  let blob: Blob
  if (response.body?.getReader) {
    const reader = response.body.getReader()
    const chunks: Uint8Array[] = []
    let loaded = 0
    try {
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        chunks.push(value)
        loaded += value.length
        onProgress(loaded, total)
      }
    } catch (error: any) {
      if (error?.name === 'AbortError') throw error
      // 传输中断时浏览器给出英文错误，统一换成中文说明。
      throw new ApiError('下载中断，请检查网络后重试', { code: 'DOWNLOAD_INTERRUPTED', retryable: true })
    }
    blob = new Blob(chunks as BlobPart[], { type: response.headers.get('Content-Type') || 'application/octet-stream' })
  } else {
    blob = await response.blob()
    onProgress(blob.size, total)
  }
  saveBlob(blob, filename)
  return { size: blob.size }
}

/**
 * Uploads a form with XMLHttpRequest, which reports the bytes actually sent;
 * credentials and errors match api(). The signal aborts the upload.
 */
export function uploadWithProgress<T = any>(
  path: string,
  body: FormData,
  onProgress: ProgressHandler = () => {},
  signal?: AbortSignal
): Promise<T> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('POST', path)
    if (session.token) xhr.setRequestHeader('Authorization', `Bearer ${session.token}`)
    xhr.upload.onprogress = event => {
      if (event.lengthComputable) onProgress(event.loaded, event.total)
    }
    xhr.onload = () => {
      let data: ApiErrorDetails & Record<string, unknown> = {}
      try {
        data = xhr.responseText ? JSON.parse(xhr.responseText) : {}
      } catch {
        /* 非 JSON 响应按空对象处理。 */
      }
      if (xhr.status >= 200 && xhr.status < 300) return resolve(data as T)
      const error = new ApiError(errorMessage({ message: data.detail || data.message || '', status: xhr.status }), {
        ...data,
        status: xhr.status
      })
      if (xhr.status === 401) {
        window.dispatchEvent(new Event('iot:unauthorized'))
        error.sessionExpired = true
      }
      reject(error)
    }
    xhr.onerror = () => reject(new ApiError('无法连接服务，请检查网络后重试', { code: 'NETWORK_ERROR', retryable: true }))
    xhr.onabort = () => reject(Object.assign(new Error('上传已取消'), { name: 'AbortError' }))
    if (signal?.aborted) return xhr.abort()
    signal?.addEventListener('abort', () => xhr.abort(), { once: true })
    xhr.send(body)
  })
}

/** "45%" while a transfer with a known size runs, "1.2 MB" when the size is unknown. */
export function transferText(loaded: number, total: number): string {
  if (total > 0) return `${Math.min(100, Math.floor((loaded / total) * 100))}%`
  return loaded >= 1 << 20 ? `${(loaded / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(loaded / 1024))} KB`
}
