/**
 * 转写条目的展示模型（与后端 DTO 解耦，供会议详情/实时转写等视图复用）。
 */

/** 转写中的最小时间单元（中文按字、英文按词），带相对于音频开头的起止毫秒 */
export interface WordSpan {
  word: string;
  startMs: number;
  endMs: number;
}

/** 会议详情页的一条转写（已解析词级时间轴与说话人配色） */
export interface TranscriptItem {
  id: number;
  speaker: string;
  text: string;
  startMs: number;
  endMs: number;
  isFinal: boolean;
  clock: string;
  /** 说话人配色下标（见 useSpeakerPalette）：颜色随主题实时变化，故只存下标 */
  colorIndex: number;
  /** 词级时间轴；为空表示后端未提供 word_timestamps，此时整体使用段级时间 */
  words: WordSpan[];
}

/** 实时转写页的一条定稿转写 */
export interface LiveTranscriptItem {
  id: number;
  speakerId: number;
  speakerName: string;
  text: string;
  time: string;
}
