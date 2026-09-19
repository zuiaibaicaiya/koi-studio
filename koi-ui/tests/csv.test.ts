import { describe, expect, test } from '@rstest/core';
import { parseCsv, rowsFromCsv } from '../src/utils/csv';

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
