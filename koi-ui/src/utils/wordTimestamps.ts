import type { MeetingTranscriptDTO } from '../services/meetingApi';
import type { WordSpan } from '../types/transcript';

/** 是否为汉字（与后端 unicode.Is(unicode.Han) 语义一致，含扩展区） */
export function isCJKChar(ch: string): boolean {
  return /[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/.test(ch);
}

/** 含中文字符的词，渲染时无需额外空格 */
export function isCJK(word: string): boolean {
  return /[一-鿿]/.test(word);
}

/** 单个字/词按词长估计的常规发音时长（毫秒），用于零宽区间向前回退 */
export function nominalWordDurationMs(word: string): number {
  let total = 0;
  let n = 0;
  for (const ch of word) {
    n++;
    total += isCJKChar(ch) ? 300 : 120;
  }
  return n === 0 || total <= 0 ? 300 : total;
}

/**
 * 单个字/词按词长估计的最长合理时长（毫秒）：
 * 汉字按 500ms/字、其余字符按 200ms/字符累加；空词兜底 500ms。
 * 与后端 transcript.estimatedWordDurationMs 的口径保持一致。
 */
export function estimatedWordDurationMs(word: string): number {
  let total = 0;
  let n = 0;
  for (const ch of word) {
    n++;
    total += isCJKChar(ch) ? 500 : 200;
  }
  return n === 0 || total <= 0 ? 500 : total;
}

/**
 * 解析后端 word_timestamps（新版为 JSON 数组，旧版为 JSON 字符串），
 * 并归一化为「每个字/词都有长度 > 0、且互不重叠」的时间区间。
 *
 * 归一化是逐字高亮可靠性的前提：
 *  - 零宽区间（start === end）永远不包含播放头，既无法高亮为「正在播放」，
 *    点击又只能停在区间起点，历史上表现为「点不中 / 高亮串字」；
 *  - 互相重叠的区间会让多个字同时高亮。
 * 后端的 WordsFromCharTimesIntervals 已保证区间首尾相接，这里主要兼容
 * 历史数据（早期实时转写把句子起点误当作词区间下界，产生首字零宽）与异常数据。
 */
export function parseWordTimestamps(t: MeetingTranscriptDTO): WordSpan[] {
  if (!t || !t.word_timestamps) return [];
  let raw: unknown;
  if (typeof t.word_timestamps === 'string') {
    try {
      raw = JSON.parse(t.word_timestamps);
    } catch {
      return [];
    }
  } else {
    raw = t.word_timestamps;
  }
  if (!Array.isArray(raw)) return [];
  const segEnd = Number(t.end_ms);
  const spans: WordSpan[] = [];
  for (const w of raw) {
    if (!w || typeof w.word !== 'string') continue;
    const start = Number(w.start_ms);
    const end = Number(w.end_ms);
    if (!Number.isFinite(start)) continue;
    // 后端只给“起始时刻”（end_ms === start_ms）或结束时刻非法时先记为起点，
    // 下面统一推算一个有效的结束时刻。
    spans.push({ word: w.word, startMs: start, endMs: Number.isFinite(end) && end > start ? end : start });
  }
  normalizeWordSpans(spans, segEnd);
  return spans;
}

/**
 * 就地归一化字/词时间区间，保证：
 *  1. 每个区间长度 > 0（否则无法高亮）；
 *  2. 区间之间互不重叠、单调不减；
 *  3. 不把字与字之间的停顿/静音整段算到前一个字上——补足时长时以
 *     「按词长估计的最长时长」为上界，且不得越过下一个字的起点。
 *
 * 对零宽区间先尝试向后补足（受下一个字起点与词长上界约束）；若身后没有
 * 空间（下一个字与本字同起点，如早期数据里首字被压成 [t, t] 而第二字也从 t 开始），
 * 则改为向前回退一个常规发音时长，把区间“让”给同一个起点上的后一个字。
 */
export function normalizeWordSpans(spans: WordSpan[], segEnd: number): void {
  if (!spans.length) return;
  const MIN_SPAN_MS = 1;
  // nextStarts[i] = 第 i 个字之后「起点更大」的第一个字的起点，-1 表示没有。
  const nextStarts = new Array<number>(spans.length).fill(-1);
  let candidate = -1;
  for (let i = spans.length - 1; i >= 0; i--) {
    nextStarts[i] = candidate;
    candidate = spans[i].startMs;
  }

  let prevEnd = -1;
  for (let i = 0; i < spans.length; i++) {
    const cur = spans[i];

    if (cur.endMs <= cur.startMs) {
      const nextStart = nextStarts[i];
      if (nextStart > cur.startMs) {
        // 身后有空间：向后补足，受「按词长估计的最长时长」与「下一个字起点」双重约束。
        let end = cur.startMs + estimatedWordDurationMs(cur.word);
        if (nextStart < end) end = nextStart;
        if (i === spans.length - 1 && Number.isFinite(segEnd) && segEnd > cur.startMs && segEnd < end) {
          end = segEnd;
        }
        cur.endMs = end;
      } else {
        // 身后没有空间（下一个字与本字同起点，早期实时转写的首字正是这种数据）：
        // 改为向前回退一个常规发音时长，把区间让给同一个起点上的后一个字。
        const originalStart = cur.startMs;
        const floor = prevEnd >= 0 ? prevEnd : 0;
        cur.startMs = Math.max(floor, originalStart - nominalWordDurationMs(cur.word));
        cur.endMs = Math.max(originalStart, cur.startMs + MIN_SPAN_MS);
      }
    }

    // 与前一个字重叠（历史数据）：对齐到前一个字结束，保证不会同时高亮两个字。
    if (prevEnd >= 0 && cur.startMs < prevEnd) {
      cur.startMs = prevEnd;
    }
    if (cur.endMs < cur.startMs) cur.endMs = cur.startMs;
    if (cur.endMs <= cur.startMs) cur.endMs = cur.startMs + MIN_SPAN_MS;
    prevEnd = cur.endMs;
  }
}
