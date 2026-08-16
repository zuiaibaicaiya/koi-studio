// 会议导出：在浏览器端将会议详情打包为 zip（txt 文本 + 音频文件）。
import { createZip, downloadBlob } from './zip'

// 与 src/views/meeting/MeetingDetail.vue 中运行时数据结构保持一致：
// meeting 为 MeetingDTO，transcripts 为 TranscriptItem[]。
export interface ExportMeetingDTO {
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
  clock?: string
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

/** 从转写内容中推导出说话人列表（去重，按出现顺序）。 */
function collectSpeakers(
  meeting: ExportMeetingDTO,
  transcripts: ExportTranscriptItem[],
): string[] {
  const seen = new Set<string>()
  const list: string[] = []
  for (const t of transcripts) {
    const name = (t.speaker || '').trim()
    if (!name || seen.has(name)) continue
    seen.add(name)
    list.push(name)
  }
  if (list.length > 0) return list
  // 没有转写内容时，回退到会议上的说话人 ID 列表
  return parseSpeakerIds(meeting.speaker_ids)
}

/** 组装会议详情文本。 */
function buildMeetingText(
  meeting: ExportMeetingDTO,
  transcripts: ExportTranscriptItem[],
): string {
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
  const speakers = collectSpeakers(meeting, transcripts)
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
      const speaker = t.speaker || '未知说话人'
      push(`[${start} - ${end}] ${speaker}：${t.text}`)
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

/**
 * 将会议导出为 zip 并触发下载。
 * zip 文件名取会议名称；包内含：
 *  1. 「<会议名称>.txt」会议详情文本
 *  2. 对应的音频文件
 */
export async function exportMeetingZip(
  meeting: ExportMeetingDTO,
  transcripts: ExportTranscriptItem[],
): Promise<void> {
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

  // 下载音频文件
  if (meeting.audio_url) {
    const resp = await fetch(meeting.audio_url)
    if (!resp.ok) {
      throw new Error(`音频下载失败（HTTP ${resp.status}）`)
    }
    const buf = await resp.arrayBuffer()
    const audioData = new Uint8Array(buf)
    entries.push({ name: audioFileNameFromUrl(meeting.audio_url), data: audioData })
  }

  const blob = createZip(entries)
  downloadBlob(blob, `${safeName}.zip`)
}
