/**
 * Parse authenticated same-origin API JSON with a strict 2 MiB response cap.
 * Do not surface arbitrary upstream/server response bodies in UI errors.
 */
export const maxAPIJSONBytes = 2 * 1024 * 1024

export async function readAPIJSON<T>(response: Response): Promise<T> {
  if (response.redirected || response.type === 'opaqueredirect') {
    throw new Error('Unexpected API redirect')
  }
  if (!response.ok) {
    throw new Error("API request failed (HTTP " + response.status + ")")
  }
  const mediaType = response.headers.get('content-type')?.split(';', 1)[0].trim().toLowerCase()
  if (mediaType !== 'application/json') {
    throw new Error('API returned a non-JSON response')
  }
  const lengthHeader = response.headers.get('content-length')
  if (lengthHeader !== null) {
    if (!/^(0|[1-9][0-9]*)$/.test(lengthHeader)) {
      throw new Error('API returned an invalid response size')
    }
    if (Number(lengthHeader) > maxAPIJSONBytes) {
      throw new Error('API response exceeded the 2 MiB safety limit')
    }
  }
  if (!response.body) throw new Error('API returned an empty response')
  const reader = response.body.getReader()
  const chunks: Uint8Array[] = []
  let total = 0
  try {
    for (;;) {
      const { value, done } = await reader.read()
      if (done) break
      if (!value) continue
      total += value.byteLength
      if (total > maxAPIJSONBytes) {
        void reader.cancel().catch(() => {})
        throw new Error('API response exceeded the 2 MiB safety limit')
      }
      chunks.push(value)
    }
  } catch (cause) {
    if (cause instanceof Error && cause.message.includes('safety limit')) throw cause
    throw new Error('API response could not be read')
  } finally {
    reader.releaseLock()
  }
  if (!total) throw new Error('API returned an empty response')
  const body = new Uint8Array(total)
  let offset = 0
  for (const chunk of chunks) {
    body.set(chunk, offset)
    offset += chunk.byteLength
  }
  try {
    const decoded = new TextDecoder('utf-8', { fatal: true }).decode(body)
    const parsed: unknown = JSON.parse(decoded)
    if (parsed === null || typeof parsed !== 'object') {
      throw new Error('Unexpected API JSON shape')
    }
    return parsed as T
  } catch {
    throw new Error('API returned invalid JSON')
  }
}
