/**
 * 外部数据示例（联系人）常量
 * 页面和 Drawer 共用
 */

type TFn = (key: string, options?: Record<string, unknown>) => string;

/**
 * 金额：十进制字符串，最多两位小数，可带负号。全程按字符串传输与展示，不做浮点运算。
 * 位数与外部表的 decimal 列一致（示例表 decimal(18,2)：整数部分最多 16 位）。
 */
export const AMOUNT_PATTERN = /^-?\d{1,16}(\.\d{1,2})?$/;

/** 状态（isActive 布尔）映射 */
export function getActiveMap(t: TFn) {
  return {
    true: { text: t('statusMap.active'), color: 'success' },
    false: { text: t('statusMap.inactive'), color: 'default' },
  };
}

/** 状态筛选项：值保持布尔，查询走 EQ（字符串才会被转成 contains） */
export function getActiveOptions(t: TFn) {
  return [
    { label: t('statusMap.active'), value: true },
    { label: t('statusMap.inactive'), value: false },
  ];
}
