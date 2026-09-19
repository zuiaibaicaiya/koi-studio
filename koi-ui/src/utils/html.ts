/** HTML 转义工具：用于 v-html 渲染前的文本净化 */
const ESCAPE_MAP: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
};

export function escapeHtml(text: string): string {
  return text.replace(/[&<>'"]/g, (c) => ESCAPE_MAP[c]);
}
