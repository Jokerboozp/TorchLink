const archived = value => /^(local|minio):\/\//.test(String(value || ''))
export function alarmMediaItems(alarm) {
  const event = alarm?.details?.videoEvent
  if (!event) return []
  const raw = event.raw || {}
  return [{ kind:'snapshot', label:'告警截图', source:event.snapshotUrl }, { kind:'clip', label:'视频片段', source:event.videoClipUrl }].filter(item => item.source || raw[`${item.kind}TransferStatus`]).map(item => {
    const status = raw[`${item.kind}TransferStatus`] || (archived(item.source) ? 'STORED' : raw.mediaTransferStatus || 'PENDING')
    return { ...item, status, stored:archived(item.source), error:raw[`${item.kind}TransferError`] || (status === 'FAILED' ? raw.mediaTransferError : '') || '' }
  })
}
export function mediaMIMEAllowed(kind, value) {
  const type = String(value || '').split(';')[0].trim().toLowerCase()
  return (kind === 'snapshot' ? ['image/png', 'image/jpeg', 'image/gif', 'image/webp', 'image/bmp'] : ['video/mp4', 'video/webm', 'video/mpeg', 'video/ogg', 'application/ogg']).includes(type)
}
export function mediaDownloadName(alarmId, kind, mime) {
  const ext = { 'image/png':'png', 'image/jpeg':'jpg', 'image/gif':'gif', 'image/webp':'webp', 'image/bmp':'bmp', 'video/mp4':'mp4', 'video/webm':'webm', 'video/mpeg':'mpeg', 'video/ogg':'ogv', 'application/ogg':'ogg' }[String(mime).split(';')[0]] || 'bin'
  return `${String(alarmId).replace(/[^a-zA-Z0-9_-]/g, '_')}-${kind}.${ext}`
}
