<script lang="ts" setup>
import type { VxeGridProps } from '#/adapter/vxe-table';

import { Page, useVbenDrawer } from '@vben/common-ui';
import { $t } from '@vben/locales';

import { notification } from 'ant-design-vue';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  deleteAiAgentApp,
  fetchListAiAgentApps,
} from '#/api';
import TableAddButton from '#/components/TableAddButton.vue';
import TableExportButton from '#/components/TableExportButton.vue';
import type {
  aiservicev1_AiAgentApp as AiAgentApp,
} from '#/api/generated/admin/service/v1';
import AgentCanvas from './agent-canvas.vue';
import AgentEditDrawer from './agent-edit-drawer.vue';
import AgentRunDrawer from './agent-run-drawer.vue';
import AgentRunsDrawer from './agent-runs-drawer.vue';

// 抽屉们：画布（全屏编辑器）/ 编辑（表单）/ 试运行（输入表单+运行）/ 运行台账（只读列表）
const [CanvasDrawer, canvasDrawerApi] = useVbenDrawer({ connectedComponent: AgentCanvas });
const [EditDrawer, editDrawerApi] = useVbenDrawer({ connectedComponent: AgentEditDrawer });
const [RunDrawer, runDrawerApi] = useVbenDrawer({ connectedComponent: AgentRunDrawer });
const [RunsDrawer, runsDrawerApi] = useVbenDrawer({ connectedComponent: AgentRunsDrawer });

const gridOptions: VxeGridProps<AiAgentApp> = {
  toolbarConfig: {
    custom: true,
    refresh: true,
    zoom: true,
  },
  height: 'auto',
  pagerConfig: {},
  rowConfig: { isHover: true, keyField: 'id' },
  stripe: true,
  proxyConfig: {
    ajax: {
      query: async ({ page }) => {
        return await fetchListAiAgentApps({
          page: page.currentPage,
          pageSize: page.pageSize,
        });
      },
    },
  },
  columns: [
    { title: $t('ui.table.seq'), type: 'seq', width: 50 },
    { title: $t('page.aiAgent.name'), field: 'name', minWidth: 160 },
    {
      title: $t('page.aiAgent.description'),
      field: 'description',
      minWidth: 200,
      showOverflow: 'tooltip',
    },
    {
      title: $t('page.aiAgent.enabled'),
      field: 'enabled',
      width: 90,
      slots: { default: 'enabled' },
    },
    {
      title: $t('page.aiAgent.updatedAt'),
      field: 'updatedAt',
      width: 170,
    },
    {
      title: $t('ui.table.action'),
      field: 'action',
      fixed: 'right',
      slots: { default: 'action' },
      width: 300,
    },
  ],
};

const exportFetcher = (page: number, pageSize: number) =>
  fetchListAiAgentApps({ page, pageSize });

const [Grid, gridApi] = useVbenVxeGrid({ gridOptions });

function openCreate() {
  editDrawerApi.setState({ title: $t('page.aiAgent.create') });
  editDrawerApi.setData({ create: true });
  editDrawerApi.open();
}

function openCanvas(row: AiAgentApp) {
  canvasDrawerApi.setData({ row });
  canvasDrawerApi.open();
}

function openEdit(row: AiAgentApp) {
  editDrawerApi.setState({ title: $t('page.aiAgent.edit') });
  editDrawerApi.setData({ create: false, row });
  editDrawerApi.open();
}

function openRun(row: AiAgentApp) {
  runDrawerApi.setData({ row });
  runDrawerApi.open();
}

function openRuns(row: AiAgentApp) {
  runsDrawerApi.setData({ row });
  runsDrawerApi.open();
}

async function handleDelete(row: AiAgentApp) {
  try {
    await deleteAiAgentApp(row.id!);
    notification.success({ message: $t('ui.notification.delete_success') });
    await gridApi.reload();
  } catch (error: any) {
    console.error('[ai-agent] delete failed', error);
    notification.error({ message: error?.message || $t('ui.notification.delete_failed') });
  }
}
</script>

<template>
  <Page auto-content-height>
    <Grid :table-title="$t('page.aiAgent.moduleName')">
      <template #toolbar-tools>
        <TableAddButton @click="openCreate" />
        <TableExportButton :fetcher="exportFetcher" :columns="gridOptions.columns" filename="ai-agent-apps" />
      </template>
      <template #enabled="{ row }">
        <a-tag v-if="row.enabled" color="success">
          {{ $t('page.aiAgent.enabledOn') }}
        </a-tag>
        <a-tag v-else>{{ $t('page.aiAgent.enabledOff') }}</a-tag>
      </template>
      <template #action="{ row }">
        <a-button type="link" size="small" @click.stop="openCanvas(row)">
          {{ $t('page.aiAgent.openCanvas') }}
        </a-button>
        <a-button type="link" size="small" @click.stop="openEdit(row)">
          {{ $t('common.edit') }}
        </a-button>
        <a-button type="link" size="small" @click.stop="openRun(row)">
          {{ $t('page.aiAgent.testRun') }}
        </a-button>
        <a-button type="link" size="small" @click.stop="openRuns(row)">
          {{ $t('page.aiAgent.runsTitle') }}
        </a-button>
        <a-popconfirm
          :cancel-text="$t('ui.button.cancel')"
          :ok-text="$t('ui.button.ok')"
          :title="$t('page.aiAgent.deleteConfirm')"
          @confirm="handleDelete(row)"
        >
          <a-button danger type="link" size="small">
            {{ $t('common.delete') }}
          </a-button>
        </a-popconfirm>
      </template>
    </Grid>
    <CanvasDrawer />
    <EditDrawer />
    <RunDrawer />
    <RunsDrawer />
  </Page>
</template>
