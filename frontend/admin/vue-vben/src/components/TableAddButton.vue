<script lang="ts" setup>
import { LucidePlus } from '@vben/icons';

/**
 * 列表工具栏「新增」按钮（与 TableExportButton 配对）。
 * antdv4 的 #icon 插槽渲染的是裸 svg（无 .ant-btn-icon 包裹），吃不到 antd
 * 的图标间距规则，这里用按钮级 flex+gap-2 统一 icon↔文本 8px 间距与 y 轴居中
 * （三端对齐 react 基准）。默认文案 ui.button.add、加号图标；
 * @click、disabled 等属性透传内部 a-button；text 传最终文案（需自行翻译）。
 */
defineProps<{ text?: string }>();
</script>

<template>
  <a-button
    type="primary"
    ghost
    class="mr-2 whitespace-nowrap inline-flex items-center justify-center gap-2"
  >
    <template #icon>
      <LucidePlus />
    </template>
    {{ text ?? $t('ui.button.add') }}
  </a-button>
</template>
