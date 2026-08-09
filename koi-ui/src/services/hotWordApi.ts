import http from './http';
import type { LibraryStatus } from '../store/hotWordLibrary';

/** 后端 hot_word_library 模型（见 models/hot_word_library.go） */
export interface HotWordLibraryDTO {
  id: number;
  name: string;
  description: string;
  status: LibraryStatus;
  word_count: number;
  created_at: string;
  updated_at?: string;
}

/** 后端 hot_word 模型（见 models/hot_word.go）：仅含 word + weight */
export interface HotWordDTO {
  id: number;
  library_id: number;
  word: string;
  weight: number;
  created_at: string;
}

export interface Paginated<T> {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
  totalPage: number;
}

export interface LibraryListParams {
  page?: number;
  pageSize?: number;
  keyword?: string;
  status?: LibraryStatus | 'all';
}

export interface WordListParams {
  page?: number;
  pageSize?: number;
  keyword?: string;
}

export interface CreateLibraryPayload {
  name: string;
  description?: string;
  status?: LibraryStatus;
}

export type UpdateLibraryPayload = Partial<CreateLibraryPayload>;

export interface CreateWordPayload {
  word: string;
  weight?: number;
}

export type UpdateWordPayload = Partial<CreateWordPayload>;

export const hotWordApi = {
  // 热词库
  listLibraries: (params: LibraryListParams = {}) =>
    http.get<Paginated<HotWordLibraryDTO>>('/api/hot-word-library', params),
  getLibrary: (id: number) => http.get<HotWordLibraryDTO>(`/api/hot-word-library/${id}`),
  createLibrary: (payload: CreateLibraryPayload) =>
    http.post<HotWordLibraryDTO>('/api/hot-word-library', payload),
  updateLibrary: (id: number, payload: UpdateLibraryPayload) =>
    http.put<HotWordLibraryDTO>(`/api/hot-word-library/${id}`, payload),
  deleteLibrary: (id: number) => http.delete<void>(`/api/hot-word-library/${id}`),

  /** 通过 Excel 文件导入热词库，库名取自文件名（见 ImportLibrary 控制器）。 */
  importLibrary: (file: File, description?: string) => {
    const formData = new FormData();
    formData.append('file', file);
    if (description) formData.append('description', description);
    return http.upload<HotWordLibraryDTO>('/api/hot-word-library/import', formData);
  },

  // 热词
  listWords: (libraryId: number, params: WordListParams = {}) =>
    http.get<Paginated<HotWordDTO>>(`/api/hot-word-library/${libraryId}/word`, params),
  createWord: (libraryId: number, payload: CreateWordPayload) =>
    http.post<HotWordDTO>(`/api/hot-word-library/${libraryId}/word`, payload),
  updateWord: (libraryId: number, wordId: number, payload: UpdateWordPayload) =>
    http.put<HotWordDTO>(`/api/hot-word-library/${libraryId}/word/${wordId}`, payload),
  deleteWord: (libraryId: number, wordId: number) =>
    http.delete<void>(`/api/hot-word-library/${libraryId}/word/${wordId}`),
};

export default hotWordApi;
