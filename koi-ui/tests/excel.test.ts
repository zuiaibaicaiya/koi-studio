import { afterEach, beforeEach, describe, expect, rs, test } from '@rstest/core';
import * as XLSX from 'xlsx';
import {
  exportAllLibrariesToExcel,
  exportLibraryTemplate,
  exportLibraryToExcel,
  parseLibraryFromExcel,
} from '../src/utils/excel';
import type { HotWordLibrary, LibraryWord } from '../src/store/hotWordLibrary';

// 自动 mock xlsx 但保留原始实现：writeFile 会被包成 spy 供断言，其余方法保持真实
rs.mock('xlsx', { spy: true });

let writeFileSpy: ReturnType<typeof rs.mocked<typeof XLSX.writeFile>>;

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

describe('Excel 导出函数', () => {
  beforeEach(() => {
    writeFileSpy = rs.mocked(XLSX.writeFile);
    writeFileSpy.mockClear();
  });

  afterEach(() => {
    rs.restoreAllMocks();
  });

  /** 读取 workbook 中某张表的所有行（含表头）。 */
  function sheetRows(workbook: { Sheets: Record<string, unknown> }, name: string): unknown[][] {
    return XLSX.utils.sheet_to_json(workbook.Sheets[name] as never, {
      header: 1,
      raw: false,
    }) as unknown[][];
  }

  const words: LibraryWord[] = [
    { id: 1, word: '锦鲤', weight: 80, category: '通用', description: '示例一' },
    { id: 2, word: '鲛青', weight: 60, category: '科技', description: '示例二' },
  ];

  test('exportLibraryToExcel：四列 + 热词数据写入指定工作表', () => {
    exportLibraryToExcel('导出.xlsx', '热词库A', words);

    expect(writeFileSpy).toHaveBeenCalledTimes(1);
    const [workbook, filename] = writeFileSpy.mock.calls[0] as [{
      SheetNames: string[];
      Sheets: Record<string, unknown>;
    }, string];
    expect(filename).toBe('导出.xlsx');
    expect(workbook.SheetNames).toContain('热词库A');
    const rows = sheetRows(workbook, '热词库A');
    expect(rows[0]).toEqual(['热词', '权重', '分类', '描述']);
    expect(rows[1]).toEqual(['锦鲤', '80', '通用', '示例一']);
    expect(rows[2]).toEqual(['鲛青', '60', '科技', '示例二']);
  });

  test('exportLibraryTemplate：含数据模板表与填写说明表', () => {
    exportLibraryTemplate('模板.xlsx');
    const [workbook, filename] = writeFileSpy.mock.calls[0] as [{
      SheetNames: string[];
    }, string];
    expect(filename).toBe('模板.xlsx');
    expect(workbook.SheetNames).toEqual(['热词库模板', '填写说明']);
  });

  test('exportAllLibrariesToExcel：汇总表 + 每个库一张表，状态映射为中文', () => {
    const libraries: HotWordLibrary[] = [
      { id: 1, name: '库一', description: 'd1', status: 'active', createdAt: '', wordCount: 2, words },
      { id: 2, name: '库二', description: 'd2', status: 'disabled', createdAt: '', wordCount: 0, words: [] },
    ];
    exportAllLibrariesToExcel('全部.xlsx', libraries);

    const [workbook, filename] = writeFileSpy.mock.calls[0] as [{
      SheetNames: string[];
      Sheets: Record<string, unknown>;
    }, string];
    expect(filename).toBe('全部.xlsx');
    // 汇总表在最前，且包含库名与状态中文
    expect(workbook.SheetNames[0]).toBe('汇总');
    const summary = sheetRows(workbook, '汇总');
    expect(summary[0]).toEqual(['热词库名称', '热词数量', '状态', '描述']);
    expect(summary[1]).toEqual(['库一', '2', '启用', 'd1']);
    expect(summary[2]).toEqual(['库二', '0', '禁用', 'd2']);
    // 每个库一张表，含其热词
    expect(workbook.SheetNames).toContain('库一');
    const libSheet = sheetRows(workbook, '库一');
    expect(libSheet[1]).toEqual(['锦鲤', '80', '通用', '示例一']);
  });
});
