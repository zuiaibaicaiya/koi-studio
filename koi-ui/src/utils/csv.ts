export interface CsvColumn {
  key: string;
  title: string;
}

function escapeCsv(value: unknown): string {
  if (value === null || value === undefined) return '';
  const s = String(value);
  if (/[",\n\r]/.test(s)) {
    return `"${s.replace(/"/g, '""')}"`;
  }
  return s;
}

/** 将二维数据导出为 CSV 文件并触发浏览器下载（带 UTF-8 BOM，Excel 可正常打开）。 */
export function exportToCsv(
  filename: string,
  columns: CsvColumn[],
  rows: Record<string, unknown>[],
): void {
  const header = columns.map((c) => escapeCsv(c.title)).join(',');
  const body = rows
    .map((row) => columns.map((c) => escapeCsv(row[c.key])).join(','))
    .join('\n');
  const content = `﻿${header}\n${body}`;
  const blob = new Blob([content], { type: 'text/csv;charset=utf-8;' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

/** 解析 CSV 文本为二维数组（支持双引号转义与换行）。 */
export function parseCsv(text: string): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = '';
  let inQuotes = false;
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    if (inQuotes) {
      if (ch === '"') {
        if (text[i + 1] === '"') {
          field += '"';
          i++;
        } else {
          inQuotes = false;
        }
      } else {
        field += ch;
      }
    } else if (ch === '"') {
      inQuotes = true;
    } else if (ch === ',') {
      row.push(field);
      field = '';
    } else if (ch === '\n' || ch === '\r') {
      if (ch === '\r' && text[i + 1] === '\n') i++;
      row.push(field);
      rows.push(row);
      row = [];
      field = '';
    } else {
      field += ch;
    }
  }
  if (field.length > 0 || row.length > 0) {
    row.push(field);
    rows.push(row);
  }
  return rows.filter((r) => r.some((c) => c.trim() !== ''));
}

/** 根据列定义，将 CSV 文本解析为对象数组（按表头标题匹配列）。 */
export function rowsFromCsv<T extends Record<string, unknown>>(
  text: string,
  columns: CsvColumn[],
): T[] {
  const matrix = parseCsv(text);
  if (matrix.length === 0) return [];
  const header = matrix[0];
  const keyByIndex = header.map((title) => {
    const col = columns.find((c) => c.title === title.trim());
    return col ? col.key : null;
  });
  return matrix.slice(1).map((row) => {
    const obj: Record<string, unknown> = {};
    row.forEach((cell, i) => {
      const key = keyByIndex[i];
      if (key) obj[key] = cell;
    });
    return obj as T;
  });
}
