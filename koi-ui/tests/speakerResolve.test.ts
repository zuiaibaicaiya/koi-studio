import { describe, expect, test } from '@rstest/core';
import type { TranscriptPayload } from '../src/services/socketio';
import { resolveTranscriptSpeaker } from '../src/utils/speakerResolve';

const candidates = [
  { id: 1, name: '张三' },
  { id: 2, name: '李四' },
];

const lookupById = (id: number) =>
  id === 9 ? { id: 9, name: '王五' } : undefined;

describe('resolveTranscriptSpeaker', () => {
  test('speaker 对象携带 id：优先按 id 匹配候选', () => {
    const payload: TranscriptPayload = { speaker: { name: '张三', id: 1 } as never };
    expect(resolveTranscriptSpeaker(payload, candidates, lookupById)).toEqual({
      id: 1,
      name: '张三',
    });
  });

  test('speaker 对象 id 不在候选中：回退本地说话人库 lookupById', () => {
    const payload: TranscriptPayload = { speaker: { name: '王五', id: 9 } as never };
    expect(resolveTranscriptSpeaker(payload, candidates, lookupById)).toEqual({
      id: 9,
      name: '王五',
    });
  });

  test('顶层 speakerId 匹配', () => {
    expect(resolveTranscriptSpeaker({ speakerId: 2 }, candidates)).toEqual({
      id: 2,
      name: '李四',
    });
  });

  test('字符串型 id（旧协议）可被 Number 化匹配', () => {
    expect(resolveTranscriptSpeaker({ speaker_id: '1' }, candidates)).toEqual({
      id: 1,
      name: '张三',
    });
  });

  test('id 匹配不到时沿用后端名称，不进入客户端兜底', () => {
    expect(resolveTranscriptSpeaker({ speakerId: 5 }, candidates)).toEqual({
      id: 5,
      name: '说话人 5',
    });
  });

  test('无 id 的 speaker 对象表示未匹配到已知说话人，必须尊重后端结果', () => {
    const payload: TranscriptPayload = { speaker: { name: '未知说话人' } as never };
    // 即使会议只配置了一位说话人，也不能归给该人
    expect(resolveTranscriptSpeaker(payload, [candidates[0]])).toEqual({
      id: -1,
      name: '未知说话人',
    });
  });

  test('speaker 对象只有名称：按名称匹配候选', () => {
    const payload: TranscriptPayload = { speaker: { name: '李四' } as never };
    expect(resolveTranscriptSpeaker(payload, candidates)).toEqual({
      id: 2,
      name: '李四',
    });
  });

  test('speaker 为字符串（旧协议）：按名称匹配', () => {
    expect(resolveTranscriptSpeaker({ speaker: '张三' }, candidates)).toEqual({
      id: 1,
      name: '张三',
    });
  });

  test('顶层 speakerName 匹配', () => {
    expect(resolveTranscriptSpeaker({ speakerName: '李四' }, candidates)).toEqual({
      id: 2,
      name: '李四',
    });
  });

  test('名称不在候选中：返回 id -1 与原名称', () => {
    expect(resolveTranscriptSpeaker({ speakerName: '路人甲' }, candidates)).toEqual({
      id: -1,
      name: '路人甲',
    });
  });

  test('无说话人信息且会议仅一位说话人：归属该人', () => {
    expect(resolveTranscriptSpeaker({}, [candidates[0]])).toEqual({
      id: 1,
      name: '张三',
    });
  });

  test('无说话人信息且多位/零位候选：标记未识别', () => {
    expect(resolveTranscriptSpeaker({}, candidates)).toEqual({
      id: -1,
      name: '未识别说话人',
    });
    expect(resolveTranscriptSpeaker({}, [])).toEqual({ id: -1, name: '未识别说话人' });
  });

  test('非法 id（不可数字化的字符串）被忽略，进入名称/兜底分支', () => {
    expect(resolveTranscriptSpeaker({ speakerId: 'abc' }, [candidates[0]])).toEqual({
      id: 1,
      name: '张三',
    });
  });
});
