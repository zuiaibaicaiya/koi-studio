import { afterEach, beforeEach, describe, expect, rs, test } from '@rstest/core';
import * as zip from '../src/utils/zip';
import {
  audioFileNameFromUrl,
  buildMeetingText,
  collectSpeakers,
  exportMeetingZip,
  formatMs,
  parseSpeakerIds,
  sanitizeFileName,
  type ExportMeetingDTO,
  type ExportTranscriptItem,
} from '../src/utils/exportMeeting';

/** 极简 ZIP 解析器（仅用于测试断言，复用 zip.test 的 STORE 解析思路）。 */
async function readZipEntries(blob: Blob): Promise<{ name: string; data: Uint8Array }[]> {
  const buf = new Uint8Array(await blob.arrayBuffer());
  const dv = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);
  const LOCAL_SIG = 0x04034b50;
  const EOCD_SIG = 0x06054b50;
  const entries: { name: string; data: Uint8Array }[] = [];
  let off = 0;
  while (off + 4 <= buf.length) {
    const sig = dv.getUint32(off, true);
    if (sig === LOCAL_SIG) {
      const compSize = dv.getUint32(off + 18, true);
      const nameLen = dv.getUint16(off + 26, true);
      const extraLen = dv.getUint16(off + 28, true);
      const name = new TextDecoder().decode(buf.subarray(off + 30, off + 30 + nameLen));
      const dataStart = off + 30 + nameLen + extraLen;
      entries.push({ name, data: buf.subarray(dataStart, dataStart + compSize) });
      off = dataStart + compSize;
    } else if (sig === EOCD_SIG) {
      break;
    } else {
      break;
    }
  }
  return entries;
}

const sampleMeeting: ExportMeetingDTO = {
  id: 1,
  name: '周会 2026',
  status: 'finished',
  start_time: '2026-01-01 10:00',
  end_time: '2026-01-01 11:00',
  participants: '张三,李四',
  audio_url: 'https://example.com/audio/koi-123.wav?token=abc',
};

const sampleTranscripts: ExportTranscriptItem[] = [
  { speaker: '张三', text: '大家好', startMs: 1000, endMs: 4000 },
  { speaker: '李四', text: '开始吧', startMs: 5000, endMs: 8000 },
  { speaker: '张三', text: '好的', startMs: 9000, endMs: 10_000 },
];

beforeEach(() => {
  rs.restoreAllMocks();
});

describe('formatMs：毫秒 → HH:MM:SS.mmm', () => {
  test('整点补零', () => {
    expect(formatMs(0)).toBe('00:00:00.000');
    expect(formatMs(1000)).toBe('00:00:01.000');
    expect(formatMs(4000)).toBe('00:00:04.000');
  });

  test('跨时分秒与毫秒', () => {
    expect(formatMs(3_661_000)).toBe('01:01:01.000');
    expect(formatMs(1234)).toBe('00:00:01.234');
    expect(formatMs(10_000)).toBe('00:00:10.000');
  });
});

describe('sanitizeFileName：去除文件名非法字符', () => {
  test('将 / : * ? " < > | 替换为下划线', () => {
    expect(sanitizeFileName('a/b:c*?')).toBe('a_b_c__');
    expect(sanitizeFileName('会议:终稿*')).toBe('会议_终稿_');
  });

  test('空名或仅空白兜底为「未命名会议」', () => {
    expect(sanitizeFileName('')).toBe('未命名会议');
    expect(sanitizeFileName('   ')).toBe('未命名会议');
  });
});

describe('parseSpeakerIds：兼容字符串与数组', () => {
  test('undefined / 空返回空数组', () => {
    expect(parseSpeakerIds(undefined)).toEqual([]);
    expect(parseSpeakerIds('')).toEqual([]);
  });

  test('逗号分隔字符串按逗号切分并 trim', () => {
    expect(parseSpeakerIds('1,2,3')).toEqual(['1', '2', '3']);
    expect(parseSpeakerIds(' 1 , 2 ')).toEqual(['1', '2']);
  });

  test('数组逐个 trim 并过滤空值', () => {
    expect(parseSpeakerIds(['1', '2', '  ', '3'])).toEqual(['1', '2', '3']);
    expect(parseSpeakerIds(['a', 'b'])).toEqual(['a', 'b']);
  });
});

describe('collectSpeakers：按出现顺序去重', () => {
  test('同一说话人多次出现只保留首次', () => {
    expect(collectSpeakers(sampleTranscripts)).toEqual(['张三', '李四']);
  });

  test('空 speaker / 空白名被跳过', () => {
    expect(
      collectSpeakers([
        { speaker: '', text: 'x', startMs: 0, endMs: 1 },
        { speaker: '  ', text: 'y', startMs: 0, endMs: 1 },
        { speaker: '王五', text: 'z', startMs: 0, endMs: 1 },
      ]),
    ).toEqual(['王五']);
  });

  test('无转写返回空数组', () => {
    expect(collectSpeakers([])).toEqual([]);
  });
});

describe('buildMeetingText：组装会议详情文本', () => {
  test('包含基本信息、说话人列表与带时间戳的转写内容', () => {
    const text = buildMeetingText(sampleMeeting, sampleTranscripts);
    expect(text).toContain('会议名称：周会 2026');
    expect(text).toContain('会议状态：已结束'); // finished → 已结束
    expect(text).toContain('开始时间：2026-01-01 10:00');
    // 说话人按出现顺序去重
    expect(text).toContain('说话人1：张三');
    expect(text).toContain('说话人2：李四');
    // 转写行使用 formatMs 的时间戳
    expect(text).toContain('[00:00:01.000 - 00:00:04.000] 张三：大家好');
    expect(text).toContain('[00:00:05.000 - 00:00:08.000] 李四：开始吧');
  });

  test('无转写内容时转写段落显示占位', () => {
    const text = buildMeetingText(sampleMeeting, []);
    expect(text).toContain('三、转写内容');
    expect(text).toContain('（无转写内容）');
    expect(text).toContain('（无）'); // 说话人信息为空
  });

  test('未知状态回退为原始 status 文本', () => {
    const text = buildMeetingText({ ...sampleMeeting, status: 'weird' }, []);
    expect(text).toContain('会议状态：weird');
  });
});

describe('audioFileNameFromUrl：从音频 URL 解析文件名', () => {
  test('带查询参数的完整 URL 取 pathname 最后的文件名', () => {
    expect(audioFileNameFromUrl('https://example.com/audio/koi-123.wav?token=abc')).toBe('koi-123.wav');
    expect(audioFileNameFromUrl('https://example.com/a/b/clip.mp3')).toBe('clip.mp3');
  });

  test('相对路径与无文件名兜底 audio.wav', () => {
    expect(audioFileNameFromUrl('audio/clip.mp3')).toBe('clip.mp3');
    expect(audioFileNameFromUrl('https://example.com/')).toBe('audio.wav');
    expect(audioFileNameFromUrl('')).toBe('audio.wav');
  });
});

describe('exportMeetingZip：打包文本 + 音频并下载', () => {
  afterEach(() => {
    rs.restoreAllMocks();
  });

  test('含音频 URL 时音频成功写入压缩包，audioIncluded 为 true', async () => {
    const captured = { blob: null as Blob | null, name: '' };
    rs.spyOn(zip, 'downloadBlob').mockImplementation((b, n) => {
      captured.blob = b as Blob;
      captured.name = n;
    });
    (globalThis as { fetch?: unknown }).fetch = rs.fn(async () => ({
      ok: true,
      arrayBuffer: async () => new Uint8Array([9, 8, 7, 6]).buffer,
    }));

    const result = await exportMeetingZip(sampleMeeting, sampleTranscripts);

    expect(result.audioIncluded).toBe(true);
    expect(captured.name).toBe('周会 2026.zip');
    const entries = await readZipEntries(captured.blob!);
    expect(entries.map((e) => e.name)).toEqual(['周会 2026.txt', 'koi-123.wav']);
    expect(Array.from(entries[1].data)).toEqual([9, 8, 7, 6]);
    // 文本文件含 UTF-8 BOM（TextDecoder 默认会剥离 BOM，故直接校验原始字节）
    const head = entries[0].data;
    expect([head[0], head[1], head[2]]).toEqual([0xef, 0xbb, 0xbf]);
    const text = new TextDecoder('utf-8').decode(entries[0].data);
    expect(text).toContain('会议名称：周会 2026');
  });

  test('无音频 URL 时仅导出文本，audioIncluded 为 false', async () => {
    const captured = { blob: null as Blob | null, name: '' };
    rs.spyOn(zip, 'downloadBlob').mockImplementation((b, n) => {
      captured.blob = b as Blob;
      captured.name = n;
    });
    const withoutAudio = { ...sampleMeeting, audio_url: undefined };

    const result = await exportMeetingZip(withoutAudio, sampleTranscripts);

    expect(result.audioIncluded).toBe(false);
    const entries = await readZipEntries(captured.blob!);
    expect(entries).toHaveLength(1);
    expect(entries[0].name).toBe('周会 2026.txt');
  });

  test('音频下载失败不阻断文本导出，audioIncluded 为 false', async () => {
    const captured = { blob: null as Blob | null, name: '' };
    rs.spyOn(zip, 'downloadBlob').mockImplementation((b, n) => {
      captured.blob = b as Blob;
      captured.name = n;
    });
    (globalThis as { fetch?: unknown }).fetch = rs.fn(async () => ({
      ok: false,
      status: 404,
      arrayBuffer: async () => new ArrayBuffer(0),
    }));

    const result = await exportMeetingZip(sampleMeeting, sampleTranscripts);

    expect(result.audioIncluded).toBe(false);
    const entries = await readZipEntries(captured.blob!);
    // 仅文本一件，且断言不抛错
    expect(entries).toHaveLength(1);
    expect(entries[0].name).toBe('周会 2026.txt');
  });

  test('文件名含非法字符时被清洗', async () => {
    const captured = { name: '' };
    rs.spyOn(zip, 'downloadBlob').mockImplementation((_b, n) => {
      captured.name = n;
    });
    const weird = { ...sampleMeeting, name: 'a/b:c*?', audio_url: undefined };

    await exportMeetingZip(weird, []);

    expect(captured.name).toBe('a_b_c__.zip');
  });
});
