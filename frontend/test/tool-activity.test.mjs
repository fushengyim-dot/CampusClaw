import test from 'node:test';
import assert from 'node:assert/strict';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { ToolActivity, appendToolActivity, toolEvents } from '../src/ToolActivity.ts';

test('tool events progress from running to a visible Chinese outcome, with escaped names', () => {
  let events = toolEvents([{ name: '<script>tool</script>', status: 'running' }]);
  assert.match(renderToStaticMarkup(createElement(ToolActivity, { events })), /正在执行/);
  events = appendToolActivity(events, { name: '<script>tool</script>', status: 'success' });
  const html = renderToStaticMarkup(createElement(ToolActivity, { events }));
  assert.match(html, /已完成/);
  assert.doesNotMatch(html, /正在执行|<script>/);
  assert.match(html, /&lt;script&gt;/);
  assert.equal(events.length, 1);
});

test('refused, failed and timed out calls remain visible; invalid events are ignored', () => {
  const events = toolEvents([{ name: 'a', status: 'rejected' }, { name: 'b', status: 'error' },
    { name: 'c', status: 'timeout' }, { name: 'x', status: '__proto__' }, null]);
  const html = renderToStaticMarkup(createElement(ToolActivity, { events }));
  for (const label of ['已拒绝', '执行失败', '执行超时']) assert.ok(html.includes(label));
  assert.equal(events.length, 3);
  assert.equal(renderToStaticMarkup(createElement(ToolActivity, { events: [] })), '');
});
