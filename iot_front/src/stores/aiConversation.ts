import { defineStore } from 'pinia'
import { shallowRef } from 'vue'
import type { AIConversation } from '../aiConversation.ts'

/**
 * The assistant conversation of the signed-in identity. It is reused when the
 * assistant page is entered again, so a running answer keeps streaming; the
 * conversation object owns its stream and is kept raw (shallowRef).
 */
export const useAIConversationStore = defineStore('aiConversation', () => {
  const current = shallowRef<AIConversation | null>(null)
  return { current }
})
