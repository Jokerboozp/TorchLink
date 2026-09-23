export const canAcknowledgeAlarm = status => status === 'ACTIVE' /* 执行当前语句并推进处理流程。 */

export const canCloseAlarm = status => ['ACTIVE', 'ACKED'].includes(status) /* 执行当前语句并推进处理流程。 */
