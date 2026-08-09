import { defineStore } from 'pinia';
import { ref } from 'vue';

/* ========== 实时会议 ========== */
export type MeetingStatus = '进行中' | '已结束' | '已预约';

export interface Meeting {
  id: number;
  title: string;
  host: string;
  roomUrl: string;
  startTime: string;
  endTime: string;
  status: MeetingStatus;
  participants: number;
  description: string;
}

const statuses: MeetingStatus[] = ['进行中', '已结束', '已预约'];

function seedMeetings(): Meeting[] {
  const list: Meeting[] = [];
  const now = Date.now();
  for (let i = 1; i <= 25; i++) {
    const st = new Date(now - (i - 1) * 86400000 - i * 3600000);
    const et = new Date(st.getTime() + 3600000 * (1 + (i % 3)));
    list.push({
      id: i,
      title: `项目评审会议 #${i}`,
      host: i % 3 === 0 ? '张经理' : i % 3 === 1 ? '李主管' : '王总监',
      roomUrl: `https://meet.example.com/room-${1000 + i}`,
      startTime: st.toISOString().slice(0, 16).replace('T', ' '),
      endTime: et.toISOString().slice(0, 16).replace('T', ' '),
      status: statuses[i % 3],
      participants: Math.floor(Math.random() * 50) + 3,
      description: `第${i}次项目评审会议，讨论项目进度与风险`,
    });
  }
  return list;
}

/* ========== 音频转写 ========== */
export type TranscriptionStatus = '转写中' | '已完成' | '失败';

export interface Transcription {
  id: number;
  meetingTitle: string;
  fileName: string;
  audioUrl: string;
  status: TranscriptionStatus;
  duration: number; // 秒
  language: string;
  transcriptText: string;
  createdAt: string;
}

const languages = ['中文普通话', '英文', '粤语', '日语', '韩语'];

function seedTranscriptions(): Transcription[] {
  const list: Transcription[] = [];
  for (let i = 1; i <= 20; i++) {
    const durationSec = Math.floor(Math.random() * 5400) + 300; // 5~95 分钟
    const status: TranscriptionStatus =
      i <= 12 ? '已完成' : i <= 17 ? '转写中' : '失败';
    list.push({
      id: i,
      meetingTitle: `项目评审会议 #${i}`,
      fileName: `meeting-recording-${String(i).padStart(3, '0')}.mp3`,
      audioUrl: `https://storage.example.com/audio/meeting-${1000 + i}.mp3`,
      status,
      duration: durationSec,
      language: languages[i % languages.length],
      transcriptText:
        status === '已完成'
          ? `会议记录摘要：讨论了项目当前进展，确认了后续里程碑节点，分配了各团队任务。参与人员就技术方案达成一致意见。`
          : '',
      createdAt: new Date(Date.now() - i * 172800000).toISOString().slice(0, 10),
    });
  }
  return list;
}

/* ========== Store ========== */
export const useMeetingStore = defineStore('meeting', () => {
  /* ---- 实时会议 ---- */
  const meetings = ref<Meeting[]>(seedMeetings());
  let nextMeetingId = 1000;

  function getMeetingById(id: number) {
    return meetings.value.find((m) => m.id === id);
  }

  function addMeeting(data: Omit<Meeting, 'id'>) {
    const item: Meeting = {
      ...data,
      id: nextMeetingId++,
      participants: Number(data.participants) || 0,
    };
    meetings.value.unshift(item);
    return item;
  }

  function updateMeeting(id: number, data: Partial<Omit<Meeting, 'id'>>) {
    const idx = meetings.value.findIndex((m) => m.id === id);
    if (idx !== -1) {
      const merged = { ...meetings.value[idx], ...data };
      merged.participants = Number(merged.participants) || 0;
      meetings.value[idx] = merged;
    }
  }

  function removeMeeting(id: number) {
    meetings.value = meetings.value.filter((m) => m.id !== id);
  }

  function importMeetings(rows: Meeting[]) {
    rows.forEach((r) => addMeeting(r));
  }

  /* ---- 音频转写 ---- */
  const transcriptions = ref<Transcription[]>(seedTranscriptions());
  let nextTranscriptionId = 2000;

  function getTranscriptionById(id: number) {
    return transcriptions.value.find((t) => t.id === id);
  }

  function addTranscription(data: Omit<Transcription, 'id' | 'createdAt'>) {
    const item: Transcription = {
      ...data,
      id: nextTranscriptionId++,
      duration: Number(data.duration) || 0,
      createdAt: new Date().toISOString().slice(0, 10),
    };
    transcriptions.value.unshift(item);
    return item;
  }

  function updateTranscription(id: number, data: Partial<Omit<Transcription, 'id'>>) {
    const idx = transcriptions.value.findIndex((t) => t.id === id);
    if (idx !== -1) {
      const merged = { ...transcriptions.value[idx], ...data };
      merged.duration = Number(merged.duration) || 0;
      transcriptions.value[idx] = merged;
    }
  }

  function removeTranscription(id: number) {
    transcriptions.value = transcriptions.value.filter((t) => t.id !== id);
  }

  function importTranscriptions(rows: Transcription[]) {
    rows.forEach((r) => addTranscription(r));
  }

  return {
    meetings,
    getMeetingById,
    addMeeting,
    updateMeeting,
    removeMeeting,
    importMeetings,
    transcriptions,
    getTranscriptionById,
    addTranscription,
    updateTranscription,
    removeTranscription,
    importTranscriptions,
  };
});
