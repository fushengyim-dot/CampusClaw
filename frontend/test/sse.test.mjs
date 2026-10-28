import assert from 'node:assert/strict';
import test from 'node:test';
import {readSSE} from '../src/sse.ts';

test('SSE parser rebuilds UTF-8 and events split across arbitrary byte chunks', async () => {
  const source = 'event: meta\r\ndata: {"skill":{"name":"数学引导"}}\r\n\r\nevent: delta\ndata: {"text":"逐段输出中文"}\n\nevent: error\ndata: {"message":"本轮中断"}\n\n';
  const bytes = new TextEncoder().encode(source);
  const chunks = [];
  for (let i = 0; i < bytes.length; i += 3) chunks.push(bytes.slice(i, i + 3));
  const response = new Response(new ReadableStream({start(controller) { for (const chunk of chunks) controller.enqueue(chunk); controller.close(); }}));
  const frames = [];
  await readSSE(response, frame => frames.push(frame));
  assert.deepEqual(frames, [
    {event: 'meta', data: {skill: {name: '数学引导'}}},
    {event: 'delta', data: {text: '逐段输出中文'}},
    {event: 'error', data: {message: '本轮中断'}},
  ]);
});

test('SSE parser ignores malformed JSON frames and reads the final unterminated frame', async () => {
  const response = new Response('event: delta\ndata: {broken}\n\nevent: done\ndata: {"message_id":42}');
  const frames = [];
  await readSSE(response, frame => frames.push(frame));
  assert.deepEqual(frames, [{event: 'done', data: {message_id: 42}}]);
});
