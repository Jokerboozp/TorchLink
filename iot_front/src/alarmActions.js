export const canAcknowledgeAlarm = status => status === 'ACTIVE'

export const canCloseAlarm = status => ['ACTIVE', 'ACKED'].includes(status)
