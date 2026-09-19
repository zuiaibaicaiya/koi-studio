import { describe, expect, test } from '@rstest/core';
import { crc32, createZip, downloadBlob, type ZipEntry } from '../src/utils/zip';

/**
 * 极简 ZIP 解析器（仅用于测试断言）：遍历本地文件头（签名 0x04034b50），
 * 按 STORE 方式取出文件名与原始数据，并读出中央目录里记录的 CRC。
 * STORE 压缩时「压缩后大小 == 未压缩大小」，故可直接比对原始字节。
 */
async function readZip(
  blob: Blob,
): Promise<{ name: string; data: Uint8Array; crc: number }[]> {
  const buf = new Uint8Array(await blob.arrayBuffer());
  const dv = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);
  const LOCAL_SIG = 0x04034b50;
  const EOCD_SIG = 0x06054b50;
  const entries: { name: string; data: Uint8Array; crc: number }[] = [];
  let off = 0;
  while (off + 4 <= buf.length) {
    const sig = dv.getUint32(off, true);
    if (sig === LOCAL_SIG) {
      const crc = dv.getUint32(off + 14, true);
      const compSize = dv.getUint32(off + 18, true);
      const nameLen = dv.getUint16(off + 26, true);
      const extraLen = dv.getUint16(off + 28, true);
      const name = new TextDecoder().decode(buf.subarray(off + 30, off + 30 + nameLen));
      const dataStart = off + 30 + nameLen + extraLen;
      const data = buf.subarray(dataStart, dataStart + compSize);
      entries.push({ name, data, crc });
      off = dataStart + compSize;
    } else if (sig === EOCD_SIG) {
      break;
    } else {
      break;
    }
  }
  return entries;
}

describe('crc32', () => {
  test('标准校验向量：crc32("123456789") === 0xCBF43926', () => {
    const bytes = new TextEncoder().encode('123456789');
    expect(crc32(bytes)).toBe(0xcbf43926);
  });

  test('空输入为 0', () => {
    expect(crc32(new Uint8Array(0))).toBe(0);
  });

  test('相同输入得到相同结果（确定性）', () => {
    const a = new TextEncoder().encode('锦鲤 koi-studio');
    expect(crc32(a)).toBe(crc32(a));
    expect(crc32(a)).not.toBe(0);
  });
});

describe('createZip', () => {
  test('STORE 打包后数据可原样解回，且 CRC 与 crc32(data) 一致', async () => {
    const entries: ZipEntry[] = [
      { name: 'hello.txt', data: new TextEncoder().encode('hello world') },
      { name: '中文名.txt', data: new Uint8Array([1, 2, 3, 4, 5]) },
    ];
    const blob = createZip(entries);
    expect(blob.type).toBe('application/zip');

    const read = await readZip(blob);
    expect(read.map((e) => e.name)).toEqual(['hello.txt', '中文名.txt']);
    expect(new TextDecoder().decode(read[0].data)).toBe('hello world');
    expect(Array.from(read[1].data)).toEqual([1, 2, 3, 4, 5]);
    expect(read[0].crc).toBe(crc32(read[0].data));
    expect(read[1].crc).toBe(crc32(read[1].data));
  });

  test('中文文件名经 UTF-8 标志位（0x0800）写入', async () => {
    const blob = createZip([{ name: '说话人A.wav', data: new Uint8Array([0]) }]);
    const buf = new Uint8Array(await blob.arrayBuffer());
    const dv = new DataView(buf.buffer);
    const flag = dv.getUint16(6, true);
    expect(flag & 0x0800).toBe(0x0800);
  });

  test('空条目数组生成合法（含 EOCD）的 zip', async () => {
    const blob = createZip([]);
    const read = await readZip(blob);
    expect(read).toEqual([]);
    expect(blob.size).toBeGreaterThan(0);
  });
});

describe('downloadBlob', () => {
  test('创建带 download 属性与正确文件名的锚点并触发点击', () => {
    let clicked = false;
    let appended: HTMLAnchorElement | null = null;

    const origCreate = (globalThis.URL as { createObjectURL?: unknown }).createObjectURL;
    const origRevoke = (globalThis.URL as { revokeObjectURL?: unknown }).revokeObjectURL;
    (globalThis.URL as { createObjectURL: () => string }).createObjectURL = () => 'blob:fake';
    (globalThis.URL as { revokeObjectURL: () => void }).revokeObjectURL = () => {};

    const origAppend = document.body.appendChild.bind(document.body);
    document.body.appendChild = ((node: Node) => {
      appended = node as HTMLAnchorElement;
      return origAppend(node);
    }) as typeof document.body.appendChild;

    const protoClick = HTMLAnchorElement.prototype.click;
    HTMLAnchorElement.prototype.click = function () {
      clicked = true;
    };

    downloadBlob(new Blob(['x']), '会议.zip');

    // downloadBlob 在 click 后会从 DOM 移除锚点，故通过 appendChild 捕获引用断言
    expect(appended).not.toBeNull();
    expect(appended!.download).toBe('会议.zip');
    expect(appended!.href).toContain('blob:fake');
    expect(clicked).toBe(true);
    // 点击后已从 DOM 移除
    expect(document.body.contains(appended)).toBe(false);

    HTMLAnchorElement.prototype.click = protoClick;
    document.body.appendChild = origAppend;
    (globalThis.URL as { createObjectURL?: unknown }).createObjectURL = origCreate;
    (globalThis.URL as { revokeObjectURL?: unknown }).revokeObjectURL = origRevoke;
  });
});
