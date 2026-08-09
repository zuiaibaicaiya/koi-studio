<script setup lang="ts">
import { computed, reactive, ref } from 'vue';
import { message } from 'antdv-next';
import type { UploadProps } from 'antdv-next';
import { useSpeakerStore, type Speaker, type SpeakerGender } from '../../store/speaker';
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

const store = useSpeakerStore();

const columns = [
  { title: 'ID', dataIndex: 'id', key: 'id', width: 70, sorter: (a: Speaker, b: Speaker) => a.id - b.id },
  { title: '姓名', dataIndex: 'name', key: 'name', sorter: (a: Speaker, b: Speaker) => a.name.localeCompare(b.name) },
  { title: '性别', dataIndex: 'gender', key: 'gender', width: 80 },
  { title: '语言', dataIndex: 'language', key: 'language', width: 100 },
  { title: '样本数', dataIndex: 'sampleCount', key: 'sampleCount', width: 100, sorter: (a: Speaker, b: Speaker) => a.sampleCount - b.sampleCount },
  { title: '描述', dataIndex: 'description', key: 'description', ellipsis: true },
  { title: '创建时间', dataIndex: 'createdAt', key: 'createdAt', width: 120 },
  { title: '操作', key: 'action', width: 150, fixed: 'right' },
];

const csvColumns: CsvColumn[] = [
  { key: 'id', title: 'ID' },
  { key: 'name', title: '姓名' },
  { key: 'gender', title: '性别' },
  { key: 'language', title: '语言' },
  { key: 'sampleCount', title: '样本数' },
  { key: 'description', title: '描述' },
  { key: 'createdAt', title: '创建时间' },
];

const genders: SpeakerGender[] = ['男', '女', '未知'];
const languages = ['中文', '英文', '粤语', '四川话', '日语'];
const keyword = ref('');
const languageFilter = ref<string | undefined>();
const pagination = reactive({ current: 1, pageSize: 10, showSizeChanger: true, showTotal: (t: number) => `共 ${t} 条` });

const filtered = computed(() =>
  store.list.filter((s) => {
    const kw = keyword.value.trim().toLowerCase();
    const matchKw = !kw || s.name.toLowerCase().includes(kw) || s.description.toLowerCase().includes(kw);
    const matchLang = !languageFilter.value || s.language === languageFilter.value;
    return matchKw && matchLang;
  }),
);

const modalVisible = ref(false);
const editingId = ref<number | null>(null);
const submitting = ref(false);
const formState = reactive<Omit<Speaker, 'id' | 'createdAt'>>({
  name: '',
  gender: '未知',
  language: '中文',
  sampleCount: 0,
  description: '',
});

function resetForm() {
  Object.assign(formState, { name: '', gender: '未知', language: '中文', sampleCount: 0, description: '' });
}
function openCreate() {
  editingId.value = null;
  resetForm();
  modalVisible.value = true;
}
function openEdit(record: Speaker) {
  editingId.value = record.id;
  Object.assign(formState, {
    name: record.name,
    gender: record.gender,
    language: record.language,
    sampleCount: record.sampleCount,
    description: record.description,
  });
  modalVisible.value = true;
}
function handleSubmit() {
  if (!formState.name.trim()) {
    message.warning('请填写姓名');
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
  languageFilter.value = undefined;
  pagination.current = 1;
  message.success('已刷新');
}
function handleExport() {
  exportToCsv('说话人数据.csv', csvColumns, store.list as unknown as Record<string, unknown>[]);
  message.success(`已导出 ${store.list.length} 条数据`);
}

const beforeUpload: UploadProps['beforeUpload'] = (file) => {
  const reader = new FileReader();
  reader.onload = () => {
    const text = String(reader.result || '');
    const rows = rowsFromCsv<Speaker>(text, csvColumns).map((r) => ({
      name: r.name || '',
      gender: (r.gender as SpeakerGender) || '未知',
      language: r.language || '中文',
      sampleCount: Number(r.sampleCount) || 0,
      description: r.description || '',
    }));
    if (rows.length === 0) {
      message.warning('未解析到有效数据');
      return;
    }
    store.importRows(rows as Speaker[]);
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
          <a-input v-model:value="keyword" placeholder="姓名 / 描述" allow-clear style="width: 220px">
            <template #prefix><SearchOutlined /></template>
          </a-input>
        </a-form-item>
        <a-form-item label="语言">
          <a-select v-model:value="languageFilter" placeholder="全部" allow-clear style="width: 130px">
            <a-select-option v-for="l in languages" :key="l" :value="l">{{ l }}</a-select-option>
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
        :scroll="{ x: 960 }"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'gender'">
            <a-tag :color="record.gender === '男' ? 'blue' : record.gender === '女' ? 'magenta' : 'default'">
              {{ record.gender }}
            </a-tag>
          </template>
          <template v-else-if="column.key === 'sampleCount'">
            <a-badge :count="record.sampleCount" :number-style="{ backgroundColor: 'var(--color-success)' }" />
          </template>
          <template v-else-if="column.key === 'action'">
            <a-space>
              <a-button type="link" size="small" @click="openEdit(record)">
                <EditOutlined />编辑
              </a-button>
              <a-popconfirm title="确认删除该说话人？" @confirm="handleDelete(record.id)">
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
      :title="editingId ? '编辑说话人' : '新增说话人'"
      @ok="handleSubmit"
      :confirm-loading="submitting"
      ok-text="保存"
      cancel-text="取消"
    >
      <a-form :label-col="{ span: 5 }" :wrapper-col="{ span: 17 }" class="modal-form">
        <a-form-item label="姓名" required>
          <a-input v-model:value="formState.name" placeholder="请输入姓名" />
        </a-form-item>
        <a-form-item label="性别">
          <a-select v-model:value="formState.gender">
            <a-select-option v-for="g in genders" :key="g" :value="g">{{ g }}</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item label="语言">
          <a-select v-model:value="formState.language">
            <a-select-option v-for="l in languages" :key="l" :value="l">{{ l }}</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item label="样本数">
          <a-input-number v-model:value="formState.sampleCount" :min="0" style="width: 100%" />
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
.modal-form {
  margin-top: 16px;
}
.modal-form :deep(.ant-form-item-label > label) {
  color: var(--color-text);
}
</style>
