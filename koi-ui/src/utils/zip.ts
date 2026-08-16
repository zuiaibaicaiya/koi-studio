// 轻量级 ZIP 打包工具（纯前端，无第三方依赖）。
// 采用 STORE（不压缩）方式写入，适合文本与已压缩音频文件。

const CRC_TABLE = (() => {
  const table = new Uint32Array(256)
  for (let n = 0; n < 256; n++) {
    let c = n
    for (let k = 0; k < 8; k++) {
      c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
    }
    table[n] = c >>> 0
  }
  return table
})()

export function crc32(buf: Uint8Array): number {
  let crc = 0xffffffff
  for (let i = 0; i < buf.length; i++) {
    crc = (crc >>> 8) ^ CRC_TABLE[(crc ^ buf[i]) & 0xff]
  }
  return (crc ^ 0xffffffff) >>> 0
}

export interface ZipEntry {
  /** 文件名，支持中文（以 UTF-8 写入并设置语言编码标志位） */
  name: string
  data: Uint8Array
}

function u16(v: number): Uint8Array {
  const a = new Uint8Array(2)
  new DataView(a.buffer).setUint16(0, v, true)
  return a
}

function u32(v: number): Uint8Array {
  const a = new Uint8Array(4)
  new DataView(a.buffer).setUint32(0, v >>> 0, true)
  return a
}

/**
 * 将多个文件打包为一个 ZIP Blob（STORE 方式）。
 */
export function createZip(entries: ZipEntry[]): Blob {
  const enc = new TextEncoder()
  const chunks: Uint8Array[] = []
  const central: Uint8Array[] = []
  let offset = 0

  for (const entry of entries) {
    const nameBytes = enc.encode(entry.name)
    const crc = crc32(entry.data)
    const size = entry.data.length
    const localOffset = offset

    const header = new Uint8Array(30)
    const hv = new DataView(header.buffer)
    hv.setUint32(0, 0x04034b50, true) // 本地文件头签名
    hv.setUint16(4, 20, true) // 所需版本
    hv.setUint16(6, 0x0800, true) // 通用标志位：UTF-8 文件名
    hv.setUint16(8, 0, true) // 压缩方式：0=STORE
    hv.setUint16(10, 0, true) // 最后修改时间
    hv.setUint16(12, 0, true) // 最后修改日期
    hv.setUint32(14, crc, true)
    hv.setUint32(18, size, true) // 压缩后大小
    hv.setUint32(22, size, true) // 未压缩大小
    hv.setUint16(26, nameBytes.length, true)
    hv.setUint16(28, 0, true) // 扩展字段长度

    chunks.push(header, nameBytes, entry.data)
    offset += header.length + nameBytes.length + entry.data.length

    // 中央目录条目
    const cd = new Uint8Array(46)
    const cdv = new DataView(cd.buffer)
    cdv.setUint32(0, 0x02014b50, true) // 中央目录签名
    cdv.setUint16(4, 20, true) // 版本（由谁创建）
    cdv.setUint16(6, 20, true) // 所需版本
    cdv.setUint16(8, 0x0800, true) // 通用标志位：UTF-8
    cdv.setUint16(10, 0, true) // 压缩方式
    cdv.setUint16(12, 0, true) // 时间
    cdv.setUint16(14, 0, true) // 日期
    cdv.setUint32(16, crc, true)
    cdv.setUint32(20, size, true)
    cdv.setUint32(24, size, true)
    cdv.setUint16(28, nameBytes.length, true)
    cdv.setUint16(30, 0, true) // 扩展字段长度
    cdv.setUint16(32, 0, true) // 注释长度
    cdv.setUint16(34, 0, true) // 磁盘起始号
    cdv.setUint16(36, 0, true) // 内部属性
    cdv.setUint32(38, 0, true) // 外部属性
    cdv.setUint32(42, localOffset, true)

    central.push(cd, nameBytes)
  }

  const cdStart = offset
  const centralSize = central.reduce((s, c) => s + c.length, 0)

  const eocd = new Uint8Array(22)
  const edv = new DataView(eocd.buffer)
  edv.setUint32(0, 0x06054b50, true) // 中央目录结束签名
  edv.setUint16(4, 0, true) // 当前磁盘编号
  edv.setUint16(6, 0, true) // 中央目录所在磁盘
  edv.setUint16(8, entries.length, true) // 本磁盘条目数
  edv.setUint16(10, entries.length, true) // 总条目数
  edv.setUint32(12, centralSize, true) // 中央目录大小
  edv.setUint32(16, cdStart, true) // 中央目录偏移
  edv.setUint16(20, 0, true) // 注释长度

  const all = [...chunks, ...central, eocd]
  const total = all.reduce((s, c) => s + c.length, 0)
  const result = new Uint8Array(total)
  let pos = 0
  for (const c of all) {
    result.set(c, pos)
    pos += c.length
  }
  return new Blob([result], { type: 'application/zip' })
}

/** 触发浏览器下载。 */
export function downloadBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
