<script setup lang="ts">
import { computed, reactive, ref, onMounted, watch } from 'vue';
import { message, Modal } from 'antdv-next';
import type { TableColumnsType, UploadProps } from 'antdv-next';
import {
  PlusOutlined,
  EditOutlined,
  DeleteOutlined,
  ReloadOutlined,
  SearchOutlined,
  DownloadOutlined,
  UploadOutlined,
  FileExcelOutlined,
  EyeOutlined,
  ExportOutlined,
  DownOutlined,
} from '@antdv-next/icons';
import { useHotWordLibraryStore } from '../../store/hotWordLibrary';
import type { HotWordLibrary, LibraryWord, LibraryStatus } from '../../store/hotWordLibrary';
import {
  exportLibraryToExcel,
  exportLibraryTemplate,
  exportAllLibrariesToExcel,
} from '../../utils/excel';
import { hotWordApi } from '../../services/hotWordApi';

const store = useHotWordLibraryStore();

/* ----------------------------- 热词库列表 ----------------------------- */
const selectedLibId = ref<number | null>(null);
const searchText = ref('');
const statusFilter = ref<'all' | LibraryStatus>('all');
/** 已应用（点击搜索后生效）的筛选条件 */
const appliedKeyword = ref('');
const appliedStatus = ref<'all' | LibraryStatus>('all');
const loading = ref(false);

const libOptions = computed(() =>
  store.libraries.map((lib) => ({
    value: lib.id,
    label: lib.name,
  })),
);

function filterLibOption(input: string, option: { label?: string }) {
  return (option?.label ?? '').toLowerCase().includes(input.toLowerCase());
}

function onLibSearch(val: string) {
  searchText.value = val;
}

function onLibChange(val: number | null) {
  if (val != null) {
    const lib = store.libraries.find((l) => l.id === val);
    searchText.value = lib ? lib.name : '';
  } else {
    searchText.value = '';
  }
}

function handleSearch() {
  appliedKeyword.value = searchText.value.trim();
  appliedStatus.value = statusFilter.value;
}

function handleReset() {
  selectedLibId.value = null;
  searchText.value = '';
  statusFilter.value = 'all';
  appliedKeyword.value = '';
  appliedStatus.value = 'all';
}

const filteredLibraries = computed<HotWordLibrary[]>(() => {
  const kw = appliedKeyword.value.trim().toLowerCase();
  return store.libraries.filter((lib) => {
    const matchKw = !kw || lib.name.toLowerCase().includes(kw);
    const matchStatus = appliedStatus.value === 'all' || lib.status === appliedStatus.value;
    return matchKw && matchStatus;
  });
});

/** 仅加载热词库元数据（含 word_count），不加载热词详情。 */
async function loadLibraries() {
  loading.value = true;
  try {
    const res = await hotWordApi.listLibraries({ page: 1, pageSize: 200 });
    store.replaceAll(
      res.items.map((dto) => ({
        id: dto.id,
        name: dto.name,
        description: dto.description,
        status: dto.status,
        createdAt: (dto.created_at || '').slice(0, 10),
        wordCount: dto.word_count,
        words: [], // 按需在抽屉中加载
      })),
    );
  } catch (e) {
    message.error((e as Error).message || '加载热词库失败');
  } finally {
    loading.value = false;
  }
}

function refresh() {
  loadLibraries();
}

/* ----------------------------- 热词库 增删改 ----------------------------- */
const libModalVisible = ref(false);
const libEditingId = ref<number | null>(null);
const libForm = reactive({ name: '', description: '', status: 'active' as LibraryStatus });

function openEditLib(lib: HotWordLibrary) {
  libEditingId.value = lib.id;
  Object.assign(libForm, { name: lib.name, description: lib.description, status: lib.status });
  libModalVisible.value = true;
}
async function submitLib() {
  if (!libForm.name.trim()) {
    message.warning('请输入热词库名称');
    return;
  }
  try {
    if (libEditingId.value == null) {
      await hotWordApi.createLibrary({
        name: libForm.name.trim(),
        description: libForm.description.trim(),
        status: libForm.status,
      });
      message.success('热词库创建成功');
    } else {
      await hotWordApi.updateLibrary(libEditingId.value, {
        name: libForm.name.trim(),
        description: libForm.description.trim(),
        status: libForm.status,
      });
      message.success('热词库更新成功');
    }
    libModalVisible.value = false;
    await loadLibraries();
  } catch (e) {
    message.error((e as Error).message || '保存失败');
  }
}
function removeLib(lib: HotWordLibrary) {
  Modal.confirm({
    title: '确认删除',
    content: `确定删除热词库「${lib.name}」及其 ${lib.wordCount} 条热词吗？`,
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      try {
        await hotWordApi.deleteLibrary(lib.id);
        message.success('删除成功');
        await loadLibraries();
      } catch (e) {
        message.error((e as Error).message || '删除失败');
      }
    },
  });
}

/* ----------------------------- Excel 导入 / 导出 ----------------------------- */
const importing = ref(false);

async function handleImport(file: File): Promise<void> {
  importing.value = true;
  try {
    await hotWordApi.importLibrary(file);
    message.success(`已通过 Excel 导入热词库「${file.name.replace(/\.[^.]+$/, '')}」`);
    await loadLibraries();
  } catch (e) {
    message.error((e as Error).message || '导入失败');
  } finally {
    importing.value = false;
  }
}

const libBeforeUpload: UploadProps['beforeUpload'] = (file) => {
  handleImport(file as File);
  return false;
};

const drawerImportBeforeUpload: UploadProps['beforeUpload'] = (file) => {
  handleImport(file as File);
  return false;
};

async function exportLib() {
  if (currentLibraryId.value == null) return;
  const lib = store.getLibrary(currentLibraryId.value);
  if (!lib) return;
  if (lib.words.length === 0) {
    message.warning('暂无热词数据可导出');
    return;
  }
  exportLibraryToExcel(`${lib.name}.xlsx`, lib.name, lib.words);
  message.success(`已导出「${lib.name}」`);
}

async function exportAll() {
  if (store.libraries.length === 0) {
    message.warning('暂无可导出的热词库');
    return;
  }
  // 逐库拉取全部热词用于导出
  const all: HotWordLibrary[] = [];
  for (const lib of store.libraries) {
    if (lib.words.length === 0 && lib.wordCount > 0) {
      try {
        const res = await hotWordApi.listWords(lib.id, { page: 1, pageSize: Math.max(lib.wordCount, 1000) });
        lib.words = res.items.map((w) => ({
          id: w.id,
          word: w.word,
          weight: w.weight,
        }));
      } catch { /* 忽略单个库加载失败 */ }
    }
    all.push(lib);
  }
  exportAllLibrariesToExcel('热词库汇总.xlsx', all);
  message.success(`已导出 ${all.length} 个热词库`);
}

function downloadTemplate() {
  exportLibraryTemplate('热词库导入模板.xlsx');
}

/* ----------------------------- 抽屉：热词明细（后端分页） ----------------------------- */
const drawerVisible = ref(false);
const currentLibraryId = ref<number | null>(null);
const wordKeyword = ref('');
const wordPage = ref(1);
const wordTotal = ref(0);
const wordLoading = ref(false);

const currentLibrary = computed<HotWordLibrary | undefined>(() =>
  currentLibraryId.value != null ? store.getLibrary(currentLibraryId.value) : undefined,
);

/** 与抽屉绑定的 word 本地缓存 */
const drawerWords = ref<LibraryWord[]>([]);
const drawerWordColumns: TableColumnsType<LibraryWord> = [
  { title: '热词', dataIndex: 'word', key: 'word', width: 180 },
  { title: '权重', key: 'weight', width: 200 },
  { title: '操作', key: 'action', width: 90, fixed: 'right' },
];

/** 从后端拉取当前库的热词分页 */
async function loadDrawerWords() {
  if (currentLibraryId.value == null) return;
  wordLoading.value = true;
  try {
    const res = await hotWordApi.listWords(currentLibraryId.value, {
      page: wordPage.value,
      pageSize: 10,
      keyword: wordKeyword.value || undefined,
    });
    drawerWords.value = res.items.map((dto) => ({
      id: dto.id,
      word: dto.word,
      weight: dto.weight,
    }));
    wordTotal.value = res.total;
    // 同步最新 wordCount 到 store
    const lib = store.getLibrary(currentLibraryId.value);
    if (lib) lib.wordCount = res.total;
  } catch (e) {
    message.error((e as Error).message || '加载热词失败');
  } finally {
    wordLoading.value = false;
  }
}

function openDrawer(lib: HotWordLibrary) {
  currentLibraryId.value = lib.id;
  wordKeyword.value = '';
  wordPage.value = 1;
  drawerVisible.value = true;
  loadDrawerWords();
}

/** 搜索 / 翻页时重新加载 */
watch([wordKeyword, wordPage], () => {
  if (drawerVisible.value) loadDrawerWords();
});

watch(wordKeyword, () => {
  wordPage.value = 1;
});

function onWordTableChange(pag: { current: number }) {
  wordPage.value = pag.current;
}

/* ----------------------------- 热词 增删改 ----------------------------- */
const wordModalVisible = ref(false);
const wordEditing = reactive({ libraryId: 0, wordId: 0 });
const wordForm = reactive({ word: '', weight: 50 });

function openCreateWord() {
  if (currentLibraryId.value == null) return;
  wordEditing.libraryId = currentLibraryId.value;
  wordEditing.wordId = 0;
  Object.assign(wordForm, { word: '', weight: 50 });
  wordModalVisible.value = true;
}
function openEditWord(w: LibraryWord) {
  wordEditing.libraryId = currentLibraryId.value as number;
  wordEditing.wordId = w.id;
  Object.assign(wordForm, { word: w.word, weight: w.weight });
  wordModalVisible.value = true;
}
async function submitWord() {
  if (!wordForm.word.trim()) {
    message.warning('请输入热词');
    return;
  }
  const payload = { word: wordForm.word.trim(), weight: wordForm.weight };
  try {
    if (wordEditing.wordId === 0) {
      await hotWordApi.createWord(wordEditing.libraryId, payload);
      message.success('热词添加成功');
    } else {
      await hotWordApi.updateWord(wordEditing.libraryId, wordEditing.wordId, payload);
      message.success('热词更新成功');
    }
    wordModalVisible.value = false;
    await loadDrawerWords();
    await loadLibraries(); // 更新列表中的 wordCount
  } catch (e) {
    message.error((e as Error).message || '保存失败');
  }
}
function removeWord(w: LibraryWord) {
  Modal.confirm({
    title: '确认删除',
    content: `确定删除热词「${w.word}」吗？`,
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      const libraryId = currentLibraryId.value;
      if (libraryId != null) {
        try {
          await hotWordApi.deleteWord(libraryId, w.id);
          message.success('删除成功');
          await loadDrawerWords();
          await loadLibraries();
        } catch (e) {
          message.error((e as Error).message || '删除失败');
        }
      }
    },
  });
}

/* ----------------------------- 表格列定义 ----------------------------- */
const libColumns: TableColumnsType<HotWordLibrary> = [
  { title: '热词库名称', key: 'name', width: 200 },
  { title: '描述', key: 'description', ellipsis: true },
  {
    title: '状态',
    key: 'status',
    width: 90,
    filters: [
      { text: '启用', value: 'active' },
      { text: '禁用', value: 'inactive' },
    ],
    onFilter: (value, record) => record.status === value,
  },
  { title: '热词数量', key: 'wordCount', width: 110, align: 'center' },
  { title: '创建时间', dataIndex: 'createdAt', key: 'createdAt', width: 130 },
  { title: '操作', key: 'action', width: 90, fixed: 'right' },
];

onMounted(loadLibraries);

function confirmRemoveLib(record: HotWordLibrary) {
  Modal.confirm({
    title: '确认删除该热词库？',
    content: '删除后该热词库及其下所有热词将一并移除，且不可恢复。',
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: () => removeLib(record),
  });
}
function confirmRemoveWord(record: LibraryWord) {
  Modal.confirm({
    title: '确认删除该热词？',
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: () => removeWord(record),
  });
}
</script>

<template>
  <div class="page">
    <!-- 工具栏 -->
    <a-card class="toolbar" variant="borderless">
      <div class="form-row">
        <a-select
          v-model:value="selectedLibId"
          class="search-select"
          placeholder="搜索热词库名称"
          allow-clear
          show-search
          :filter-option="filterLibOption"
          :options="libOptions"
          @search="onLibSearch"
          @change="onLibChange"
        />
        <a-select v-model:value="statusFilter" class="status-select">
          <a-select-option value="all">全部状态</a-select-option>
          <a-select-option value="active">启用</a-select-option>
          <a-select-option value="inactive">禁用</a-select-option>
        </a-select>
        <a-button type="primary" @click="handleSearch"><SearchOutlined />搜索</a-button>
        <a-button @click="handleReset">重置</a-button>
        <div class="actions">
          <a-upload :before-upload="libBeforeUpload" :show-upload-list="false" accept=".xlsx,.xls">
            <a-button :loading="importing"><UploadOutlined />导入</a-button>
          </a-upload>
          <a-button @click="exportAll"><ExportOutlined />导出全部</a-button>
          <a-button @click="downloadTemplate"><FileExcelOutlined />模板导出</a-button>
          <a-button @click="refresh"><ReloadOutlined />刷新</a-button>
        </div>
      </div>
    </a-card>

    <!-- 热词库表格 -->
    <a-card class="table-card" variant="borderless">
      <a-table
        row-key="id"
        :columns="libColumns"
        :data-source="filteredLibraries"
        :loading="loading"
        :pagination="{ pageSize: 8, showTotal: (t: number) => `共 ${t} 个热词库` }"
        :scroll="{ x: 'max-content' }"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'name'">
            <a-button type="link" class="lib-name" @click="openDrawer(record)">
              {{ record.name }}
            </a-button>
          </template>
          <template v-else-if="column.key === 'status'">
            <a-tag :color="record.status === 'active' ? 'success' : 'default'">
              {{ record.status === 'active' ? '启用' : '禁用' }}
            </a-tag>
          </template>
          <template v-else-if="column.key === 'wordCount'">
            <a-badge :count="record.wordCount" :number-style="{ backgroundColor: '#1677ff' }" />
            <span class="count-text">{{ record.wordCount }} 条</span>
          </template>
          <template v-else-if="column.key === 'action'">
            <a-dropdown :trigger="['click']" placement="bottomRight">
              <a-button type="link" size="small">
                操作<DownOutlined />
              </a-button>
              <template #popupRender>
                <a-menu class="action-menu">
                  <a-menu-item key="view" @click="openDrawer(record)">
                    <EyeOutlined />查看热词
                  </a-menu-item>
                  <a-menu-item key="edit" @click="openEditLib(record)">
                    <EditOutlined />编辑
                  </a-menu-item>
                  <a-menu-item key="delete" danger @click="confirmRemoveLib(record)">
                    <DeleteOutlined />删除
                  </a-menu-item>
                </a-menu>
              </template>
            </a-dropdown>
          </template>
          <template v-else>{{ record[column.dataIndex] }}</template>
        </template>
        <template #emptyText>
          <a-empty description="暂无热词库，点击「导入」开始" />
        </template>
      </a-table>
    </a-card>

    <!-- 热词库 新增/编辑 -->
    <a-modal
      v-model:open="libModalVisible"
      :title="libEditingId == null ? '新增热词库' : '编辑热词库'"
      @ok="submitLib"
      ok-text="保存"
      cancel-text="取消"
      destroy-on-hidden
    >
      <a-form layout="vertical" class="lib-form">
        <a-form-item label="热词库名称" required>
          <a-input v-model:value="libForm.name" placeholder="如：金融专业术语" />
        </a-form-item>
        <a-form-item label="描述">
          <a-textarea v-model:value="libForm.description" :rows="3" placeholder="简要描述该热词库用途" />
        </a-form-item>
        <a-form-item label="状态">
          <a-radio-group v-model:value="libForm.status">
            <a-radio value="active">启用</a-radio>
            <a-radio value="inactive">禁用</a-radio>
          </a-radio-group>
        </a-form-item>
      </a-form>
    </a-modal>

    <!-- 热词明细抽屉 -->
    <a-drawer
      v-model:open="drawerVisible"
      :title="currentLibrary ? `热词明细 · ${currentLibrary.name}` : '热词明细'"
      :size="560"
      destroy-on-hidden
    >
      <template #extra>
        <a-upload
          :before-upload="drawerImportBeforeUpload"
          :show-upload-list="false"
          accept=".xlsx,.xls"
        >
          <a-button size="small"><UploadOutlined />导入到本库</a-button>
        </a-upload>
      </template>

      <div v-if="currentLibrary" class="drawer-meta">
        <a-descriptions :column="1" size="small" bordered>
          <a-descriptions-item label="状态">
            <a-tag :color="currentLibrary.status === 'active' ? 'success' : 'default'">
              {{ currentLibrary.status === 'active' ? '启用' : '禁用' }}
            </a-tag>
          </a-descriptions-item>
          <a-descriptions-item label="热词数量">{{ currentLibrary.wordCount }} 条</a-descriptions-item>
          <a-descriptions-item label="描述">{{ currentLibrary.description || '—' }}</a-descriptions-item>
        </a-descriptions>
      </div>

      <div class="drawer-toolbar">
        <a-input
          v-model:value="wordKeyword"
          placeholder="搜索热词"
          allow-clear
          class="word-search"
        >
          <template #prefix><SearchOutlined /></template>
        </a-input>
        <a-button type="primary" size="small" @click="openCreateWord">
          <PlusOutlined />新增热词
        </a-button>
        <a-button size="small" @click="exportLib">
          <DownloadOutlined />导出本库
        </a-button>
      </div>

      <a-table
        row-key="id"
        size="small"
        :columns="drawerWordColumns"
        :data-source="drawerWords"
        :loading="wordLoading"
        :pagination="{
          current: wordPage,
          pageSize: 10,
          total: wordTotal,
          showTotal: (t: number) => `共 ${t} 条热词`,
        }"
        :scroll="{ x: 'max-content' }"
        @change="onWordTableChange"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'word'">
            <span class="word-text">{{ record.word }}</span>
          </template>
          <template v-else-if="column.key === 'weight'">
            <a-progress
              :percent="Math.min(Number(record.weight) || 0, 100)"
              size="small"
              :stroke-color="Number(record.weight) >= 70 ? '#52c41a' : '#1677ff'"
            />
            <span class="weight-num">{{ record.weight }}</span>
          </template>
          <template v-else-if="column.key === 'action'">
            <a-dropdown :trigger="['click']" placement="bottomRight">
              <a-button type="link" size="small">
                操作<DownOutlined />
              </a-button>
              <template #popupRender>
                <a-menu class="action-menu">
                  <a-menu-item key="edit" @click="openEditWord(record)">
                    <EditOutlined />编辑
                  </a-menu-item>
                  <a-menu-item key="delete" danger @click="confirmRemoveWord(record)">
                    <DeleteOutlined />删除
                  </a-menu-item>
                </a-menu>
              </template>
            </a-dropdown>
          </template>
          <template v-else>{{ record[column.dataIndex] }}</template>
        </template>
        <template #emptyText>
          <a-empty description="该热词库暂无热词" />
        </template>
      </a-table>
    </a-drawer>

    <!-- 热词 新增/编辑 -->
    <a-modal
      v-model:open="wordModalVisible"
      :title="wordEditing.wordId === 0 ? '新增热词' : '编辑热词'"
      @ok="submitWord"
      ok-text="保存"
      cancel-text="取消"
      destroy-on-hidden
    >
      <a-form layout="vertical" class="word-form">
        <a-form-item label="热词" required>
          <a-input v-model:value="wordForm.word" placeholder="如：人工智能" />
        </a-form-item>
        <a-form-item label="权重（0-100）">
          <a-input-number v-model:value="wordForm.weight" :min="0" :max="100" class="full" />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.toolbar {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-sm);
  padding: 12px 16px;
}
.form-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
}
.search-select {
  width: 280px;
}
.status-select {
  width: 140px;
}
.actions {
  margin-left: auto;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.table-card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-sm);
}
.lib-name {
  padding: 0;
  font-weight: 600;
}
.count-text {
  margin-left: 6px;
  color: var(--color-text-secondary);
  font-size: 13px;
}

.drawer-meta {
  margin-bottom: 12px;
}
.drawer-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}
.word-search {
  flex: 1;
}
.word-text {
  font-weight: 500;
}
.weight-num {
  margin-left: 8px;
  color: var(--color-text-secondary);
  font-size: 12px;
}

.lib-form,
.word-form {
  padding-top: 16px;
}
.full {
  width: 100%;
}
.action-menu {
  min-width: 140px;
}
.action-menu :deep(.ant-dropdown-menu-item) {
  display: flex;
  align-items: center;
  gap: 6px;
}
</style>
