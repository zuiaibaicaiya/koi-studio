import { describe, expect, test } from '@rstest/core';
import * as XLSX from 'xlsx';
import { parseLibraryFromExcel } from '../src/utils/excel';

/** 用 SheetJS 现写一份 xlsx，再交给被测函数解析，验证解析逻辑闭环。 */
function buildExcelFile(aoa: (string | number)[][], name: string): File {
  const ws = XLSX.utils.aoa_to_sheet(aoa);
  const wb = XLSX.utils.book_new();
  XLSX.utils.book_append_sheet(wb, ws, 'Sheet1');
  const buf = XLSX.write(wb, { type: 'array', bookType: 'xlsx' }) as ArrayBuffer;
  return new File([buf], name);
}

describe('parseLibraryFromExcel', () => {
  test('含表头：解析热词/权重/分类/描述并跳过表头', async () => {
    const file = buildExcelFile(
      [
        ['热词', '权重', '分类', '描述'],
        ['锦鲤', '80', '通用', '示例一'],
        ['鲛青', '60', '科技', ''],
      ],
      '我的热词库.xlsx',
    );
    const lib = await parseLibraryFromExcel(file);
    expect(lib.name).toBe('我的热词库');
    expect(lib.words).toEqual([
      { word: '锦鲤', weight: 80, category: '通用', description: '示例一' },
      { word: '鲛青', weight: 60, category: '科技', description: '' },
    ]);
  });

  test('重复热词去重，仅保留首次出现', async () => {
    const file = buildExcelFile(
      [
        ['热词', '权重'],
        ['锦鲤', '80'],
        ['锦鲤', '10'],
      ],
      'dup.xlsx',
    );
    const lib = await parseLibraryFromExcel(file);
    expect(lib.words).toHaveLength(1);
    expect(lib.words[0]).toEqual({ word: '锦鲤', weight: 80, category: '通用', description: '' });
  });

  test('非法分类回落「通用」，非法权重回落 0', async () => {
    const file = buildExcelFile(
      [['热词', '权重', '分类'], ['测试', 'abc', '未知分类']],
      'fallback.xlsx',
    );
    const lib = await parseLibraryFromExcel(file);
    expect(lib.words[0]).toEqual({ word: '测试', weight: 0, category: '通用', description: '' });
  });

  test('无表头时首行作为数据解析', async () => {
    const file = buildExcelFile([['锦鲤', '80']], 'noheader.xlsx');
    const lib = await parseLibraryFromExcel(file);
    expect(lib.words).toEqual([
      { word: '锦鲤', weight: 80, category: '通用', description: '' },
    ]);
  });

  test('仅表头 / 无有效数据抛错', async () => {
    const file = buildExcelFile([['热词', '权重']], 'empty.xlsx');
    await expect(parseLibraryFromExcel(file)).rejects.toThrow('没有有效的热词数据');
  });
});
