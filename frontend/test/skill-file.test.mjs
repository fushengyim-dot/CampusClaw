import assert from 'node:assert/strict';
import test from 'node:test';
import {readCourseSkillFile} from '../src/skillFile.ts';

test('reads a valid UTF-8 course skill text file', async () => {
  const file = new File(['名称：数学引导\n\n适用条件：解题\n启用：是\n\n先提示下一步'], 'math.MD');
  assert.match(await readCourseSkillFile(file), /数学引导/);
});

test('rejects unsupported extensions and invalid UTF-8', async () => {
  await assert.rejects(readCourseSkillFile(new File(['text'], 'skill.json')), /只支持/);
  await assert.rejects(readCourseSkillFile(new File([new Uint8Array([0xff, 0xfe])], 'skill.txt')), /UTF-8/);
});

test('counts Unicode characters and rejects files over the server limit', async () => {
  assert.equal(Array.from(await readCourseSkillFile(new File(['🧠'.repeat(8000)], 'skill.txt'))).length, 8000);
  await assert.rejects(readCourseSkillFile(new File(['🧠'.repeat(8001)], 'skill.txt')), /8000/);
});
