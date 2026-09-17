import type { TranscriptPayload } from '../services/socketio';

/**
 * 转写结果的说话人解析规则（实时转写页与第二屏投屏页共用，保证两处展示一致）。
 *
 * 解析顺序：
 * 1. 用 speaker 对象 / speakerId / speaker_id 的 id 在候选说话人中匹配；
 * 2. 后端显式下发了 speaker 对象时，一律尊重后端结果，不进入客户端兜底
 *    （无 id 的 {name:'未知说话人'} 表示未匹配到已知说话人）；
 * 3. 只有名称时按名称匹配候选说话人；
 * 4. 兜底：会议仅配置一位说话人时归属该人，否则标记未识别。
 */

/** 候选说话人的最小结构（本地说话人库 / 会议配置的说话人） */
export interface SpeakerCandidate {
  id: number;
  name: string;
}

/** 解析结果：id 为 -1 表示未匹配到本地说话人库 */
export interface ResolvedSpeaker {
  id: number;
  name: string;
}

/**
 * 把后端下发的 speaker 信息解析为本地说话人。
 *
 * @param payload 后端下发的转写负载
 * @param candidates 会议配置的候选说话人（优先用于 id / 名称匹配）
 * @param lookupById 本地说话人库按 id 查找，用于候选之外的兜底
 */
export function resolveTranscriptSpeaker(
  payload: TranscriptPayload,
  candidates: SpeakerCandidate[] = [],
  lookupById?: (id: number) => SpeakerCandidate | undefined,
): ResolvedSpeaker {
  // 规范化 speaker 字段：后端新版协议下 speaker 为嵌套对象 {name, id, ...}
  const speakerObj =
    typeof payload.speaker === 'object' && payload.speaker !== null ? payload.speaker : null;
  const speakerName: string | undefined =
    payload.speakerName ||
    (speakerObj ? speakerObj.name : undefined) ||
    (typeof payload.speaker === 'string' ? payload.speaker : undefined);
  const speakerObjId = speakerObj?.id != null ? Number(speakerObj.id) : undefined;

  const findById = (id: number) =>
    candidates.find((s) => s.id === id) ?? lookupById?.(id);

  // 优先使用 speaker 对象中的 id，其次使用顶层 speakerId / speaker_id
  const rawId = speakerObjId ?? payload.speakerId ?? payload.speaker_id;
  if (rawId !== undefined && rawId !== null && rawId !== '') {
    const id = Number(rawId);
    if (!Number.isNaN(id)) {
      const matched = findById(id);
      if (matched) return { id: matched.id, name: matched.name };
      return { id, name: speakerName || `说话人 ${id}` };
    }
  }

  // 后端显式下发了 speaker 对象 → 后端已做声纹识别，必须尊重其结果。
  // 无 id 的 speaker 对象（如 {name: "未知说话人"}）意味着未匹配到已知说话人，
  // 不应跳过此结果进入客户端的「配置仅一位 → 归给该人」兜底逻辑。
  if (speakerObj) {
    if (speakerName && speakerName !== '未知说话人') {
      const matched = candidates.find((s) => s.name === speakerName);
      return matched ? { id: matched.id, name: matched.name } : { id: -1, name: speakerName };
    }
    return { id: -1, name: '未知说话人' };
  }

  if (speakerName && speakerName !== '未知说话人') {
    const matched = candidates.find((s) => s.name === speakerName);
    return matched ? { id: matched.id, name: matched.name } : { id: -1, name: speakerName };
  }

  // 后端未做说话人分离或识别失败：仅配置一位说话人时归属该人
  if (candidates.length === 1) {
    return { id: candidates[0].id, name: candidates[0].name };
  }
  return { id: -1, name: '未识别说话人' };
}
