<script setup lang="ts">
import { computed, reactive, ref } from 'vue';
import { message } from 'antdv-next';
import type { UploadProps } from 'antdv-next';
import { useHotWordStore, type HotWord } from '../../store/hotWord';
import { exportToCsv, rowsFromCsv, type CsvColumn } from '../../utils/csv';
import {
  PlusOutlined,
  EditOutlined,
  DeleteOutlined,
  ReloadOutlined,
  SearchOutlined,
  DownloadOutlined,
  UploadOutlined,
} from '@antdv-next/icons';

const store = useHotWordStore();

const columns = [
  { title: 'ID', dataIndex: 'id', key: 'id', width: 70, sorter: (a: HotWord, b: HotWord) => a.id - b.id },
  { title: '热词', dataIndex: 'word', key: 'word', sorter: (a: HotWord, b: HotWord) => a.word.localeCompare(b.word) },
  { title: '分类', dataIndex: 'category', key: 'category', width: 110 },
  { title: '权重', dataIndex: 'weight', key: 'weight', width: 90, sorter: (a: HotWord, b: HotWord) => a.weight - b.weight },
  { title: '描述', dataIndex: 'description', key: 'description', ellipsis: true },
  { title: '创建时间', dataIndex: 'createdAt', key: 'createdAt', width: 120 },
  { title: '操作', key: 'action', width: 150, fixed: 'right' },
];

const csvColumns: CsvColumn[] = [
  { key: 'id', title: 'ID' },
  { key: 'word', title: '热词' },
  { key: 'category', title: '分类' },
  { key: 'weight', title: '权重' },
  { key: 'description', title: '描述' },
  { key: 'createdAt', title: '创建时间' },
];

const categories = ['通用', '金融', '医疗', '法律', '科技', '教育'];
const keyword = ref('');
const categoryFilter = ref<string | undefined>();
const pagination = reactive({ current: 1, pageSize: 10, showSizeChanger: true, showTotal: (t: number) => `共 ${t} 条` });

const filtered = computed(() =>
  store.list.filter((w) => {
    const kw = keyword.value.trim().toLowerCase();
    const matchKw = !kw || w.word.toLowerCase().includes(kw) || w.description.toLowerCase().includes(kw);
    const matchCat = !categoryFilter.value || w.category === categoryFilter.value;
    return matchKw && matchCat;
  }),
);

const modalVisible = ref(false);
const editingId = ref<number | null>(null);
const submitting = ref(false);
const formState = reactive<Omit<HotWord, 'id' | 'createdAt'>>({
  word: '',
  category: '通用',
  weight: 50,
  description: '',
});

function resetForm() {
  Object.assign(formState, { word: '', category: '通用', weight: 50, description: '' });
}
function openCreate() {
  editingId.value = null;
  resetForm();
  modalVisible.value = true;
}
function openEdit(record: HotWord) {
  editingId.value = record.id;
  Object.assign(formState, {
    word: record.word,
    category: record.category,
    weight: record.weight,
    description: record.description,
  });
  modalVisible.value = true;
}
function handleSubmit() {
  if (!formState.word.trim()) {
    message.warning('请填写热词');
    return;
  }
  submitting.value = true;
  if (editingId.value) {
    store.update(editingId.value, { ...formState });
    message.success('更新成功');
  } else {
    store.add({ ...formState });
    message.success('创建成功');
  }
  submitting.value = false;
  modalVisible.value = false;
}
function handleDelete(id: number) {
  store.remove(id);
  message.success('删除成功');
}
function handleRefresh() {
  keyword.value = '';
  categoryFilter.value = undefined;
  pagination.current = 1;
  message.success('已刷新');
}
function handleExport() {
  exportToCsv('热词库数据.csv', csvColumns, store.list as unknown as Record<string, unknown>[]);
  message.success(`已导出 ${store.list.length} 条数据`);
}

const beforeUpload: UploadProps['beforeUpload'] = (file) => {
  const reader = new FileReader();
  reader.onload = () => {
    const text = String(reader.result || '');
    const rows = rowsFromCsv<HotWord>(text, csvColumns).map((r) => ({
      word: r.word || '',
      category: r.category || '通用',
      weight: Number(r.weight) || 0,
      description: r.description || '',
    }));
    if (rows.length === 0) {
      message.warning('未解析到有效数据');
      return;
    }
    store.importRows(rows as HotWord[]);
    message.success(`成功导入 ${rows.length} 条数据`);
  };
  reader.readAsText(file);
  return false;
};
</script>

<template>
  <div class="page">
    <a-card class="toolbar" variant="borderless">
      <a-form layout="inline" class="filter-form">
        <a-form-item label="关键词">
          <a-input v-model:value="keyword" placeholder="热词 / 描述" allow-clear style="width: 220px">
            <template #prefix><SearchOutlined /></template>
          </a-input>
        </a-form-item>
        <a-form-item label="分类">
          <a-select v-model:value="categoryFilter" placeholder="全部" allow-clear style="width: 130px">
            <a-select-option v-for="c in categories" :key="c" :value="c">{{ c }}</a-select-option>
          </a-select>
        </a-form-item>
      </a-form>
      <div class="actions">
        <a-button type="primary" @click="openCreate"><PlusOutlined />新增</a-button>
        <a-upload :before-upload="beforeUpload" :show-upload-list="false" accept=".csv">
          <a-button><UploadOutlined />导入</a-button>
        </a-upload>
        <a-button @click="handleExport"><DownloadOutlined />导出</a-button>
        <a-button @click="handleRefresh"><ReloadOutlined />刷新</a-button>
      </div>
    </a-card>

    <a-card variant="borderless" class="table-card">
      <a-table
        :columns="columns"
        :data-source="filtered"
        :pagination="pagination"
        row-key="id"
        :scroll="{ x: 900 }"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'category'">
            <a-tag color="gold">{{ record.category }}</a-tag>
          </template>
          <template v-else-if="column.key === 'weight'">
            <a-progress :percent="record.weight" size="small" :show-info="false" />
            <span class="weight-text">{{ record.weight }}</span>
          </template>
          <template v-else-if="column.key === 'action'">
            <a-space>
              <a-button type="link" size="small" @click="openEdit(record)">
                <EditOutlined />编辑
              </a-button>
              <a-popconfirm title="确认删除该热词？" @confirm="handleDelete(record.id)">
                <a-button type="link" size="small" danger>
                  <DeleteOutlined />删除
                </a-button>
              </a-popconfirm>
            </a-space>
          </template>
        </template>
      </a-table>
    </a-card>

    <a-modal
      v-model:open="modalVisible"
      :title="editingId ? '编辑热词' : '新增热词'"
      @ok="handleSubmit"
      :confirm-loading="submitting"
      ok-text="保存"
      cancel-text="取消"
    >
      <a-form :label-col="{ span: 5 }" :wrapper-col="{ span: 17 }" class="modal-form">
        <a-form-item label="热词" required>
          <a-input v-model:value="formState.word" placeholder="请输入热词" />
        </a-form-item>
        <a-form-item label="分类">
          <a-select v-model:value="formState.category">
            <a-select-option v-for="c in categories" :key="c" :value="c">{{ c }}</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item label="权重">
          <a-input-number v-model:value="formState.weight" :min="0" :max="100" style="width: 100%" />
        </a-form-item>
        <a-form-item label="描述">
          <a-textarea v-model:value="formState.description" :rows="3" placeholder="请输入描述" />
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
  padding: 12px 16px;
}
.toolbar :deep(.ant-form-item-label > label) {
  color: var(--color-text-secondary);
}
.filter-form {
  margin-bottom: 12px;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.table-card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
}
.weight-text {
  color: var(--color-warning);
  margin-left: 6px;
}
.modal-form {
  margin-top: 16px;
}
.modal-form :deep(.ant-form-item-label > label) {
  color: var(--color-text);
}
</style>
