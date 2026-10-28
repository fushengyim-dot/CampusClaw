const MAX_SKILL_TEXT_CHARS = 8000;

export async function readCourseSkillFile(file: File): Promise<string> {
  const name = file.name.toLowerCase();
  if (!name.endsWith('.txt') && !name.endsWith('.md')) {
    throw new Error('技能文件只支持 .txt 或 .md 格式');
  }
  let text: string;
  try {
    text = new TextDecoder('utf-8', {fatal: true}).decode(await file.arrayBuffer());
  } catch {
    throw new Error('文件不是有效的 UTF-8 文本');
  }
  if (Array.from(text).length > MAX_SKILL_TEXT_CHARS) {
    throw new Error('技能文件不能超过8000个字符');
  }
  return text;
}
