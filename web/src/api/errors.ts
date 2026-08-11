export function apiErrorMessage(error: unknown, fallback: string): string {
  if (!isRecord(error) || !isRecord(error.error) || typeof error.error.message !== 'string') {
    return fallback
  }
  return error.error.message
}

export function apiFieldErrors(error: unknown): { name: string; errors: string[] }[] {
  if (!isRecord(error) || !isRecord(error.error) || !Array.isArray(error.error.field_errors)) {
    return []
  }
  return error.error.field_errors.flatMap((item) => {
    if (!isRecord(item) || typeof item.field !== 'string' || typeof item.message !== 'string') {
      return []
    }
    return [{ name: item.field, errors: [item.message] }]
  })
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}
