import { createPinia } from 'pinia'

// One Pinia instance for the app. Modules outside components (permissions,
// realtime) use it directly, so a store is the same object everywhere.
export const pinia = createPinia()
