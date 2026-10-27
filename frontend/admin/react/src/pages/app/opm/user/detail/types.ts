/**
 * 用户详情页 - 标签页常量
 */
export const TabEnum = {
  BASIC_INFO: 'basicInfo',
  INTERNAL_MESSAGE: 'internalMessage',
} as const;

export type TabEnum = (typeof TabEnum)[keyof typeof TabEnum];
