import { afterEach, beforeEach, describe, expect, rs, test } from '@rstest/core';
import { exportToCsv, parseCsv, rowsFromCsv } from '../src/utils/csv';

describe('parseCsv', () => {
  test('基础逗号分隔', () => {
    expect(parseCsv('a,b,c\n1,2,3')).toEqual([
      ['a', 'b', 'c'],
      ['1', '2', '3'],
    ]);
  });

  test('支持双引号字段（含逗号、换行与转义引号）', () => {
    expect(parseCsv('"a,1","b\n2","c""3"')).toEqual([['a,1', 'b\n2', 'c"3']]);
  });

  test('兼容 CRLF 换行', () => {
    expect(parseCsv('a,b\r\n1,2')).toEqual([
      ['a', 'b'],
      ['1', '2'],
    ]);
  });

  test('空字段保留为空串', () => {
    expect(parseCsv('a,,c')).toEqual([['a', '', 'c']]);
  });

  test('全空行被过滤', () => {
    expect(parseCsv('a,b\n\n1,2\n,,\n')).toEqual([
      ['a', 'b'],
      ['1', '2'],
    ]);
  });

  test('空文本返回空数组', () => {
    expect(parseCsv('')).toEqual([]);
  });
});

describe('rowsFromCsv', () => {
  const columns = [
    { key: 'word', title: '热词' },
    { key: 'weight', title: '权重' },
  ];

  test('按表头标题映射为对象数组', () => {
    const rows = rowsFromCsv<Record<string, unknown>>('热词,权重\n锦鲤,80\n鲛青,60', columns);
    expect(rows).toEqual([
      { word: '锦鲤', weight: '80' },
      { word: '鲛青', weight: '60' },
    ]);
  });

  test('未知表头列被忽略', () => {
    const rows = rowsFromCsv<Record<string, unknown>>('热词,备注\n锦鲤,测试', columns);
    expect(rows).toEqual([{ word: '锦鲤' }]);
  });

  test('带引号的 CSV 也能解析', () => {
    const rows = rowsFromCsv<Record<string, unknown>>('热词,权重\n"锦鲤,鲤",80', columns);
    expect(rows).toEqual([{ word: '锦鲤,鲤', weight: '80' }]);
  });

  test('空输入返回空数组', () => {
    expect(rowsFromCsv('', columns)).toEqual([]);
  });
});

describe('exportToCsv：生成带 BOM 的 CSV 并触发下载', () => {
  let createObjectURL: typeof URL.createObjectURL;
  let revokeObjectURL: typeof URL.revokeObjectURL;
  let protoClick: () => void;

  beforeEach(() => {
    createObjectURL = URL.createObjectURL;
    revokeObjectURL = URL.revokeObjectURL;
    protoClick = HTMLAnchorElement.prototype.click;
  });

  afterEach(() => {
    URL.createObjectURL = createObjectURL;
    URL.revokeObjectURL = revokeObjectURL;
    HTMLAnchorElement.prototype.click = protoClick;
  });

  test('内容含 UTF-8 BOM，且表头与行正确拼接', async () => {
    let capturedBlob: Blob | null = null;
    URL.createObjectURL = rs.fn((b: Blob) => {
      capturedBlob = b;
      return 'blob:csv';
    }) as typeof URL.createObjectURL;
    // 阻止 happy-dom 实际导航
    HTMLAnchorElement.prototype.click = () => {};

    exportToCsv('热词.csv', [{ key: 'word', title: '热词' }, { key: 'weight', title: '权重' }], [
      { word: '锦鲤', weight: 80 },
      { word: '鲛青', weight: 60 },
    ]);

    expect(capturedBlob).not.toBeNull();
    const text = await (capturedBlob as Blob).text();
    // BOM 后紧跟表头
    expect(text.startsWith('﻿热词,权重')).toBe(true);
    expect(text).toContain('锦鲤,80');
    expect(text).toContain('鲛青,60');
  });

  test('含逗号 / 引号 / 换行的字段被双引号包裹转义', async () => {
    let capturedBlob: Blob | null = null;
    URL.createObjectURL = rs.fn((b: Blob) => {
      capturedBlob = b;
      return 'blob:csv';
    }) as typeof URL.createObjectURL;
    HTMLAnchorElement.prototype.click = () => {};

    exportToCsv('x.csv', [{ key: 'v', title: '值' }], [{ v: 'a,b"c\nd' }]);

    const text = await (capturedBlob as Blob).text();
    expect(text.trimEnd()).toBe('﻿值\n"a,b""c\nd"');
  });

  test('下载锚点带有正确的 download 文件名', () => {
    let downloadedAs = '';
    URL.createObjectURL = rs.fn(() => 'blob:csv') as typeof URL.createObjectURL;
    const origAppend = document.body.appendChild.bind(document.body);
    document.body.appendChild = ((n: Node) => {
      const el = n as HTMLAnchorElement;
      if (el.tagName === 'A') downloadedAs = el.download;
      return origAppend(n);
    }) as typeof document.body.appendChild;
    HTMLAnchorElement.prototype.click = () => {};

    exportToCsv('会议.csv', [{ key: 'k', title: '列' }], [{ k: 'v' }]);

    expect(downloadedAs).toBe('会议.csv');
    document.body.appendChild = origAppend;
  });
});
