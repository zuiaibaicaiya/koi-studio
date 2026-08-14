import { io, type Socket } from 'socket.io-client';

/**
 * 实时转写服务地址：优先 PUBLIC_SOCKET_URL，其次复用后端基地址 PUBLIC_API_BASE。
 */
const SOCKET_URL =
  (import.meta.env.PUBLIC_SOCKET_URL as string | undefined) ??
  (import.meta.env.PUBLIC_API_BASE as string | undefined) ??
  'http://127.0.0.1:8000';

/** 事件回调：参数类型由具体调用方声明 */
export type SocketHandler<T extends unknown[] = unknown[]> = (...args: T) => void;

/** 后端推送的说话人子对象 */
export interface SpeakerBrief {
  name?: string;
  id?: number;
  gender?: string;
  description?: string;
}

/** 后端推送的转写结果（兼容驼峰 / 下划线两种字段风格） */
export interface TranscriptPayload {
  text?: string;
  isFinal?: boolean;
  is_final?: boolean;
  /** 说话人 ID，可与本地说话人库匹配 */
  speakerId?: number | string;
  speaker_id?: number | string;
  /** 说话人名称，后端已完成声纹匹配时下发 */
  speakerName?: string;
  /** speaker 在后端新版协议中是嵌套对象 {name, id, ...}，旧版是纯字符串 */
  speaker?: string | SpeakerBrief;
  /** 语音段起始毫秒（相对音频开头），仅最终结果携带 */
  startMs?: number;
  start_ms?: number;
  /** 语音段结束毫秒（相对音频开头），仅最终结果携带 */
  endMs?: number;
  end_ms?: number;
}

/**
 * Socket.IO 单例封装：负责与转写后端建立长连接、上行 PCM 音频、下行转写文本。
 *
 * 约定的事件协议：
 * - 上行 `with-binary`：(pcmArrayBuffer, flag)，flag=1 表示音频分片，flag=0 表示本次会话结束
 * - 下行 `transcript`：{ text, isFinal, ... } 转写结果
 * - 下行 `with-binary-response`：音频分片处理回执
 */
class SocketioService {
  private socket: Socket | null = null;

  /** 建立连接（重复调用复用同一实例） */
  connect(): Socket {
    if (this.socket) return this.socket;

    this.socket = io(SOCKET_URL, {
      reconnection: true,
      reconnectionAttempts: 5,
      reconnectionDelay: 1000,
      timeout: 20000,
      // websocket 优先，保证二进制帧传输效率
      transports: ['websocket', 'polling'],
      autoUnref: false,
    });

    this.socket.on('connect', () => console.log('[socket] connected:', this.socket?.id));
    this.socket.on('disconnect', (reason) => console.log('[socket] disconnected:', reason));
    this.socket.on('connect_error', (error) => console.error('[socket] connect error:', error));

    return this.socket;
  }

  /** 发送自定义事件 */
  emit(event: string, ...args: unknown[]): void {
    this.socket?.emit(event, ...args);
  }

  /** 监听事件 */
  on<T extends unknown[]>(event: string, callback: SocketHandler<T>): void {
    this.socket?.on(event, callback as SocketHandler);
  }

  /** 移除事件监听；不传 callback 时移除该事件的全部监听 */
  off<T extends unknown[]>(event: string, callback?: SocketHandler<T>): void {
    if (callback) this.socket?.off(event, callback as SocketHandler);
    else this.socket?.off(event);
  }

  /** 断开连接并释放实例 */
  disconnect(): void {
    if (!this.socket) return;
    this.socket.removeAllListeners();
    this.socket.disconnect();
    this.socket = null;
  }

  isConnected(): boolean {
    return this.socket?.connected ?? false;
  }
}

export const socketioService = new SocketioService();

export default socketioService;
