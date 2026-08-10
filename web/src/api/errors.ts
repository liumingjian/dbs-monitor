export function apiErrorMessage(error: unknown, fallback: string): string {
  if (!isRecord(error) || !isRecord(error.error) || typeof error.error.message !== 'string') {
    return fallback
  }
  return error.error.message
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}
