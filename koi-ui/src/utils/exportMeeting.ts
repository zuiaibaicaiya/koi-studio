// 会议导出：在浏览器端将会议详情打包为 zip（txt 文本 + 音频文件）。
import { createZip, downloadBlob } from './zip'
import { meetingApi } from '../services/meetingApi'

// 与 src/views/meeting/MeetingDetail.vue 中运行时数据结构保持一致。
export interface ExportMeetingDTO {
  id: number
  name: string
  status: string
  start_time?: string
  end_time?: string
  participants?: string
  audio_url?: string
  /** 后端返回逗号分隔字符串，亦兼容数组 */
  speaker_ids?: string | string[]
}

export interface ExportTranscriptItem {
  speaker: string
  text: string
  startMs: number
  endMs: number
}

export interface ExportResult {
  /** 音频是否成功写入压缩包（失败时会仅导出文本） */
  audioIncluded: boolean
}

const STATUS_LABEL: Record<string, string> = {
  created: '已创建',
  ongoing: '进行中',
  finished: '已结束',
}

function pad(n: number, len = 2): string {
  return String(n).padStart(len, '0')
}

/** 将毫秒格式化为 HH:MM:SS.mmm */
function formatMs(ms: number): string {
  const totalSec = Math.floor(ms / 1000)
  const h = Math.floor(totalSec / 3600)
  const m = Math.floor((totalSec % 3600) / 60)
  const s = totalSec % 60
  const milli = Math.floor(ms % 1000)
  return `${pad(h)}:${pad(m)}:${pad(s)}.${pad(milli, 3)}`
}

/** 去除文件名中的非法字符。 */
function sanitizeFileName(name: string): string {
  return name.replace(/[\\/:*?"<>|]/g, '_').trim() || '未命名会议'
}

function parseSpeakerIds(ids?: string | string[]): string[] {
  if (!ids) return []
  if (Array.isArray(ids)) return ids.map((s) => String(s).trim()).filter(Boolean)
  return ids
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
}

/** 将后端转写 DTO 映射为导出用的精简结构。 */
function toExportItem(t: {
  speaker_name?: string
  text?: string
  start_ms?: number
  end_ms?: number
}): ExportTranscriptItem {
  return {
    speaker: t.speaker_name || '未知说话人',
    text: t.text || '',
    startMs: t.start_ms ?? 0,
    endMs: t.end_ms ?? 0,
  }
}

/** 遍历所有分页，拉取会议的全部转写段落（避免使用详情页已加载的部分数据，保证完整）。 */
async function fetchAllTranscripts(meetingId: number): Promise<ExportTranscriptItem[]> {
  const out: ExportTranscriptItem[] = []
  const pageSize = 200
  let page = 1
  // 最多保护 100 页，避免异常时死循环
  while (page <= 100) {
    const res = await meetingApi.getMeetingTranscripts(meetingId, { page, pageSize })
    out.push(...res.items.map(toExportItem))
    const totalPages = res.pageSize ? Math.max(1, Math.ceil(res.total / res.pageSize)) : 1
    if (page >= totalPages) break
    page++
  }
  return out
}

/** 从转写内容中推导出说话人列表（去重，按出现顺序）。 */
function collectSpeakers(transcripts: ExportTranscriptItem[]): string[] {
  const seen = new Set<string>()
  const list: string[] = []
  for (const t of transcripts) {
    const name = (t.speaker || '').trim()
    if (!name || seen.has(name)) continue
    seen.add(name)
    list.push(name)
  }
  return list
}

/** 组装会议详情文本。 */
function buildMeetingText(meeting: ExportMeetingDTO, transcripts: ExportTranscriptItem[]): string {
  const lines: string[] = []
  const push = (s = '') => lines.push(s)

  push('会议详情')
  push('='.repeat(40))
  push('')

  push('一、会议基本信息')
  push(`会议名称：${meeting.name || ''}`)
  push(`会议状态：${STATUS_LABEL[meeting.status] || meeting.status || ''}`)
  push(`开始时间：${meeting.start_time || ''}`)
  push(`结束时间：${meeting.end_time || ''}`)
  push(`参会人：${meeting.participants || ''}`)
  push('')

  push('二、说话人信息')
  const speakers = collectSpeakers(transcripts)
  if (speakers.length === 0) {
    push('（无）')
  } else {
    speakers.forEach((name, i) => {
      push(`说话人${i + 1}：${name}`)
    })
  }
  push('')

  push('三、转写内容')
  if (transcripts.length === 0) {
    push('（无转写内容）')
  } else {
    transcripts.forEach((t) => {
      const start = formatMs(t.startMs)
      const end = formatMs(t.endMs)
      push(`[${start} - ${end}] ${t.speaker}：${t.text}`)
    })
  }
  push('')

  return lines.join('\n')
}

/** 从音频 URL 中解析文件名（含扩展名）。 */
function audioFileNameFromUrl(url: string): string {
  try {
    const u = new URL(url)
    const base = u.pathname.split('/').pop() || ''
    if (base) return base
  } catch {
    // 忽略，使用相对路径兜底
  }
  const seg = url.split('/').pop() || ''
  return seg || 'audio.wav'
}

/** 下载音频字节（Electron 已允许跨域，普通 fetch 即可读取音频内容）。 */
async function fetchAudio(audioUrl: string): Promise<Uint8Array> {
  const resp = await fetch(audioUrl)
  if (!resp.ok) {
    throw new Error(`音频下载失败（HTTP ${resp.status}）`)
  }
  const buf = await resp.arrayBuffer()
  return new Uint8Array(buf)
}

/**
 * 将会议详情（文本 + 音频）在浏览器端打包为 zip 并触发下载。
 * zip 文件名取会议名称；包内含：
 *  1. 「<会议名称>.txt」会议详情文本
 *  2. 对应的音频文件（下载失败则仅导出文本）
 */
export async function exportMeetingZip(
  meeting: ExportMeetingDTO,
  transcripts: ExportTranscriptItem[],
): Promise<ExportResult> {
  const safeName = sanitizeFileName(meeting.name || '会议')

  const text = buildMeetingText(meeting, transcripts)
  // 添加 UTF-8 BOM，便于 Windows 记事本正确识别中文
  const bom = new Uint8Array([0xef, 0xbb, 0xbf])
  const textBytes = new TextEncoder().encode(text)
  const txtData = new Uint8Array(bom.length + textBytes.length)
  txtData.set(bom, 0)
  txtData.set(textBytes, bom.length)

  const entries: { name: string; data: Uint8Array }[] = [
    { name: `${safeName}.txt`, data: txtData },
  ]

  let audioIncluded = false
  if (meeting.audio_url) {
    try {
      const audioData = await fetchAudio(meeting.audio_url)
      entries.push({ name: audioFileNameFromUrl(meeting.audio_url), data: audioData })
      audioIncluded = true
    } catch (e) {
      // 音频下载失败不阻断文本导出
      console.warn('音频文件导出失败：', e)
    }
  }

  const blob = createZip(entries)
  downloadBlob(blob, `${safeName}.zip`)
  return { audioIncluded }
}

/**
 * 通过会议 ID 完成「拉取会议 + 拉取全部转写 + 打包下载」的完整导出流程。
 * 适用于会议详情页与会议管理列表页。
 */
export async function exportMeetingById(meetingId: number): Promise<ExportResult> {
  const meeting = await meetingApi.getMeeting(meetingId)
  const transcripts = await fetchAllTranscripts(meetingId)
  const dto: ExportMeetingDTO = {
    id: meeting.id,
    name: meeting.name,
    status: meeting.status,
    start_time: meeting.start_time,
    end_time: meeting.end_time,
    participants: meeting.participants,
    audio_url: meeting.audio_url,
    speaker_ids: meeting.speaker_ids,
  }
  return exportMeetingZip(dto, transcripts)
}
