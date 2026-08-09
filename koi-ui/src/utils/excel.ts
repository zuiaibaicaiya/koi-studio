import * as XLSX from 'xlsx';
import type { LibraryWord, WordCategory, HotWordLibrary } from '../store/hotWordLibrary';

/** 从 Excel 解析出的单条热词（不含 id，导入时由 store 分配）。 */
export interface ImportedWord {
  word: string;
  weight: number;
  category: WordCategory;
  description: string;
}

/** 解析 Excel 得到的热词库名称与热词列表。 */
export interface ParsedLibrary {
  name: string;
  words: ImportedWord[];
}

/** 表头识别关键字（与后端 hot_word_excel_service 约定一致）。 */
const HEADER_HINTS = ['热词', '热词内容', '词语', 'word', 'hotword', 'hot word'];
const CATEGORY_SET: ReadonlySet<string> = new Set(['通用', '金融', '医疗', '法律', '科技', '教育']);

function isHeaderRow(firstCell: string): boolean {
  return HEADER_HINTS.includes(firstCell.trim().toLowerCase());
}

/** 解析 Excel 文件：约定第一个工作表第一列为热词、第二列为权重（可选），
 *  可选第三列分类、第四列描述；首行若为表头则自动跳过；重复热词去重。
 *  库名取文件名（去掉扩展名）。 */
export async function parseLibraryFromExcel(file: File): Promise<ParsedLibrary> {
  const buffer = await file.arrayBuffer();
  const workbook = XLSX.read(buffer, { type: 'array', cellDates: false });
  const sheetName = workbook.SheetNames[0];
  if (!sheetName) {
    throw new Error('Excel 文件没有可用的工作表');
  }
  const sheet = workbook.Sheets[sheetName];
  const rows = XLSX.utils.sheet_to_json<unknown[]>(sheet, {
    header: 1,
    defval: '',
    raw: false,
  }) as unknown[][];

  const dataRows = rows.filter(
    (r) => Array.isArray(r) && r.some((c) => String(c).trim() !== ''),
  );

  const words: ImportedWord[] = [];
  const existed = new Set<string>();
  dataRows.forEach((row, index) => {
    const first = String(row[0] ?? '').trim();
    if (!first) return;
    if (index === 0 && isHeaderRow(first)) return;
    if (existed.has(first)) return;
    existed.add(first);

    const weightRaw = String(row[1] ?? '').trim();
    const parsedWeight = Number(weightRaw);
    const categoryRaw = String(row[2] ?? '').trim();
    const category = (CATEGORY_SET.has(categoryRaw) ? categoryRaw : '通用') as WordCategory;
    const description = String(row[3] ?? '').trim();

    words.push({
      word: first,
      weight: Number.isFinite(parsedWeight) ? parsedWeight : 0,
      category,
      description,
    });
  });

  if (words.length === 0) {
    throw new Error('Excel 文件中没有有效的热词数据');
  }

  const name = file.name.replace(/\.[^.]+$/, '').trim();
  return { name: name || '未命名热词库', words };
}

/** 将一个热词库的热词导出为 Excel 文件（含热词 / 权重 / 分类 / 描述 四列）。 */
export function exportLibraryToExcel(filename: string, sheetName: string, words: LibraryWord[]): void {
  const aoa: (string | number)[][] = [['热词', '权重', '分类', '描述']];
  words.forEach((w) => aoa.push([w.word, w.weight, w.category ?? '', w.description ?? '']));

  const worksheet = XLSX.utils.aoa_to_sheet(aoa);
  worksheet['!cols'] = [
    { wch: 24 },
    { wch: 8 },
    { wch: 10 },
    { wch: 40 },
  ];

  const workbook = XLSX.utils.book_new();
  XLSX.utils.book_append_sheet(workbook, worksheet, sheetName.slice(0, 28) || 'Sheet1');
  XLSX.writeFile(workbook, filename);
}

/** 导出 Excel 导入模板：
 *  - 第一个工作表「热词库模板」为数据模板（表头 + 示例）；
 *  - 第二个工作表「填写说明」为各列规则说明。
 *  解析时默认取 SheetNames[0]，故说明表置于其后。 */
export function exportLibraryTemplate(filename: string): void {
  // 数据模板表
  const dataAoa: (string | number)[][] = [
    ['热词', '权重', '分类', '描述'],
    ['示例热词一', 80, '通用', '这是一个示例热词'],
    ['示例热词二', 60, '科技', '权重为数字，分类可选'],
  ];
  const dataSheet = XLSX.utils.aoa_to_sheet(dataAoa);
  dataSheet['!cols'] = [
    { wch: 24 },
    { wch: 8 },
    { wch: 10 },
    { wch: 40 },
  ];

  // 填写说明表
  const tipAoa: (string | number)[][] = [
    ['填写说明（导入前请删除「热词库模板」中的示例行）'],
    [''],
    ['1. 在「热词库模板」工作表中填写数据，导入时按此格式解析。'],
    ['2. 第一列「热词」为必填，不可为空，重复热词会自动去重（仅保留首次出现）。'],
    ['3. 第二列「权重」为 0-100 的数字，留空或非法时默认为 0。'],
    ['4. 第三列「分类」可选：通用 / 金融 / 医疗 / 法律 / 科技 / 教育，留空默认为「通用」。'],
    ['5. 第四列「描述」可选，留空即可。'],
    ['6. 导入时热词库名称取自文件名，请按需重命名文件后再导入。'],
  ];
  const tipSheet = XLSX.utils.aoa_to_sheet(tipAoa);
  tipSheet['!cols'] = [{ wch: 64 }];

  const workbook = XLSX.utils.book_new();
  XLSX.utils.book_append_sheet(workbook, dataSheet, '热词库模板');
  XLSX.utils.book_append_sheet(workbook, tipSheet, '填写说明');
  XLSX.writeFile(workbook, filename);
}

/** 将所有热词库导出为单个 Excel 工作簿，每个热词库一个工作表（含汇总表）。 */
export function exportAllLibrariesToExcel(filename: string, libraries: HotWordLibrary[]): void {
  const workbook = XLSX.utils.book_new();

  // 汇总表
  const summary: (string | number)[][] = [['热词库名称', '热词数量', '状态', '描述']];
  libraries.forEach((lib) => {
    summary.push([
      lib.name,
      lib.wordCount,
      lib.status === 'active' ? '启用' : '禁用',
      lib.description,
    ]);
  });
  const summarySheet = XLSX.utils.aoa_to_sheet(summary);
  summarySheet['!cols'] = [{ wch: 24 }, { wch: 10 }, { wch: 8 }, { wch: 40 }];
  XLSX.utils.book_append_sheet(workbook, summarySheet, '汇总');

  // 每个热词库一个工作表
  const usedNames = new Set<string>(['汇总']);
  libraries.forEach((lib, idx) => {
    const aoa: (string | number)[][] = [['热词', '权重', '分类', '描述']];
    lib.words.forEach((w) => aoa.push([w.word, w.weight, w.category ?? '', w.description ?? '']));
    const worksheet = XLSX.utils.aoa_to_sheet(aoa);
    worksheet['!cols'] = [
      { wch: 24 },
      { wch: 8 },
      { wch: 10 },
      { wch: 40 },
    ];

    let sheetName = lib.name.slice(0, 28) || `热词库${idx + 1}`;
    if (usedNames.has(sheetName)) sheetName = `${sheetName}_${idx + 1}`;
    usedNames.add(sheetName);
    XLSX.utils.book_append_sheet(workbook, worksheet, sheetName);
  });

  XLSX.writeFile(workbook, filename);
}
