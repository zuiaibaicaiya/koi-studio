import { defineStore } from 'pinia';
import { ref, computed } from 'vue';

export type LibraryStatus = 'active' | 'inactive';
export type WordCategory = '通用' | '金融' | '医疗' | '法律' | '科技' | '教育';

/** 热词库下的单条热词。category / description 仅在 Excel 导入导出时使用，
 *  后端 hot_word 表仅存储 word + weight。 */
export interface LibraryWord {
  id: number;
  word: string;
  weight: number;
  category?: WordCategory;
  description?: string;
}

/** 热词库。wordCount 来自后端 word_count 字段，words 按需加载。 */
export interface HotWordLibrary {
  id: number;
  name: string;
  description: string;
  status: LibraryStatus;
  createdAt: string;
  wordCount: number;
  words: LibraryWord[];
}

export const useHotWordLibraryStore = defineStore('hotWordLibrary', () => {
  const libraries = ref<HotWordLibrary[]>([]);

  const libraryCount = computed(() => libraries.value.length);
  const totalWordCount = computed(() =>
    libraries.value.reduce((sum, lib) => sum + lib.wordCount, 0),
  );

  function getLibrary(id: number) {
    return libraries.value.find((lib) => lib.id === id);
  }

  /** 使用后端 DTO 列表替换本地热词库（不含 words，仅含 word_count）。 */
  function replaceAll(list: HotWordLibrary[]) {
    libraries.value = list;
  }

  /** 将后端返回的 word 列表写入指定热词库的 words 字段。 */
  function setWords(libraryId: number, words: LibraryWord[]) {
    const lib = getLibrary(libraryId);
    if (lib) lib.words = words;
  }

  return {
    libraries,
    libraryCount,
    totalWordCount,
    getLibrary,
    replaceAll,
    setWords,
  };
});

