import assert from 'node:assert/strict'
import test from 'node:test'
import { readAPIJSON, maxAPIJSONBytes } from './apiResponse.ts'

const json = (body, status=200) => new Response(JSON.stringify(body), {
  status,
  headers: { 'Content-Type': 'application/json; charset=utf-8' }
})

test('decodes valid same-origin object and array JSON without exposing raw data in errors', async () => {
  assert.deepEqual(await readAPIJSON(json({ connected: true })), { connected: true })
  assert.deepEqual(await readAPIJSON(json([{ id: 'snapshot123' }])), [{ id: 'snapshot123' }])
})

test('non-2xx response never displays a raw server error body or credential', async () => {
  const token = 'SECRET_SERVER_CREDENTIAL_SHOULD_NEVER_DISPLAY'
  const result = json({ error: token, sourceRoot: '/private/home' }, 403)
  await assert.rejects(() => readAPIJSON(result), error =>
    error.message === 'API request failed (HTTP 403)' &&
    !error.message.includes(token) && !error.message.includes('/private/home'))
})

test('fails closed for missing/wrong content type and invalid length', async () => {
  await assert.rejects(() => readAPIJSON(new Response('private file', {
    headers: { 'Content-Type': 'text/html' }
  })), /non-JSON/)
  await assert.rejects(() => readAPIJSON(new Response(null, {
    headers: { 'Content-Type': 'application/json' }
  })), /empty response/)
  await assert.rejects(() => readAPIJSON(new Response('{}', {
    headers: { 'Content-Type': 'application/json', 'Content-Length': 'bad' }
  })), /invalid response size/)
  await assert.rejects(() => readAPIJSON(new Response('{}', {
    headers: { 'Content-Type': 'application/json', 'Content-Length': String(maxAPIJSONBytes+1) }
  })), /safety limit/)
})

test('bounds a chunked response even without a Content-Length header', async () => {
  let cancelled = false
  const stream = new ReadableStream({
    pull(controller) {
      controller.enqueue(new Uint8Array(1024 * 1024 + 1))
    },
    cancel() { cancelled = true }
  })
  const response = new Response(stream, { headers: { 'Content-Type': 'application/json' } })
  await assert.rejects(() => readAPIJSON(response), /safety limit/)
  assert.equal(cancelled, true)
})

test('parsing fails closed without echoing raw malformed JSON or private content', async () => {
  await assert.rejects(() => readAPIJSON(new Response('{"sensitive":"private", bad}', {
    headers: { 'Content-Type': 'application/json' }
  })), error => error.message === 'API returned invalid JSON' && !error.message.includes('private'))
  await assert.rejects(() => readAPIJSON(new Response('null', {
    headers: { 'Content-Type': 'application/json' }
  })), /invalid JSON/)
  await assert.rejects(() => readAPIJSON(new Response('false', {
    headers: { 'Content-Type': 'application/json' }
  })), /invalid JSON/)
  await assert.rejects(() => readAPIJSON(new Response(new Uint8Array([0xc3, 0x28]), {
    headers: { 'Content-Type': 'application/json' }
  })), /invalid JSON/)
})
