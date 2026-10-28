export type SSEFrame = {event: string; data: Record<string, unknown>};

function parseFrame(raw: string): SSEFrame | null {
  const lines = raw.split(/\r?\n/);
  const event = lines.find(line => line.startsWith('event:'))?.slice(6).trim() || '';
  const data = lines.filter(line => line.startsWith('data:')).map(line => line.slice(5).trim()).join('\n');
  if (!event || !data) return null;
  try { return {event, data: JSON.parse(data) as Record<string, unknown>}; }
  catch { return null; }
}

export async function readSSE(response: Response, onFrame: (frame: SSEFrame) => void): Promise<void> {
  if (!response.body) throw new Error('浏览器没有提供流式响应');
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  while (true) {
    const {value, done} = await reader.read();
    buffer += done ? decoder.decode() : decoder.decode(value, {stream: true});
    const frames = buffer.split(/\r?\n\r?\n/);
    buffer = frames.pop() || '';
    for (const raw of frames) {
      const frame = parseFrame(raw);
      if (frame) onFrame(frame);
    }
    if (done) break;
  }
  const finalFrame = parseFrame(buffer);
  if (finalFrame) onFrame(finalFrame);
}
