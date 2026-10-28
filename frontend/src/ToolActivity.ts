import { createElement } from 'react';

const labels = { running: '正在执行', success: '已完成', error: '执行失败', rejected: '已拒绝', timeout: '执行超时' };
export type ToolEvent = { name: string; status: keyof typeof labels };

export function toolEvents(value: unknown): ToolEvent[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap(item => {
    if (!item || typeof item !== 'object' || typeof item.name !== 'string' || !item.name.trim() ||
        typeof item.status !== 'string' || !Object.hasOwn(labels, item.status)) return [];
    return [{ name: item.name.slice(0, 128), status: item.status as ToolEvent['status'] }];
  }).slice(-16);
}

export function appendToolActivity(current: ToolEvent[], event: ToolEvent): ToolEvent[] {
  const next = [...current];
  let active = -1;
  for (let index = next.length - 1; index >= 0; index--) {
    if (next[index].name === event.name && next[index].status === 'running') { active = index; break; }
  }
  if (event.status !== 'running' && active >= 0) next[active] = event;
  else next.push(event);
  return next.slice(-16);
}

export function ToolActivity({ events }: { events: ToolEvent[] }) {
  if (!events.length) return null;
  return createElement('section', { className: 'tool-activity', 'aria-label': '本轮工具调用', 'aria-live': 'polite' },
    createElement('strong', null, '本轮工具调用'),
    createElement('ul', null, events.map((event, index) => createElement('li', { key: `${index}-${event.name}` },
      createElement('span', null, event.name), ' · ', createElement('span', null, labels[event.status])))));
}
