// Keep the editable contract separate from server-owned identity/session fields.
export function userAccessPayload(value = {}) {
  return {
    username: value.username || '',
    displayName: value.displayName || '',
    email: value.email || '',
    phone: value.phone || '',
    password: value.password || '',
    mustChangePassword: value.mustChangePassword !== false,
    enabled: value.enabled !== false,
    roleIds: [...(value.roleIds || [])],
    permissions: [...(value.permissions || [])],
    deviceScope: value.deviceScope || 'none',
    deviceIds: [...(value.deviceIds || [])]
  }
}
