export const WORK_QUEUE_CHANGED_EVENT = 'nexflow:work-queue-changed'

export function notifyWorkQueueChanged() {
  window.dispatchEvent(new CustomEvent(WORK_QUEUE_CHANGED_EVENT))
}
