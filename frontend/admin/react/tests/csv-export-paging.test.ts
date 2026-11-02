import { describe, expect, it } from 'vitest';

import { fetchAllRows } from '@/utils/csv';

/** 模拟后端 go-crud：页长超过 100 被静默截成 100，offset 按截断后的页长算 */
function backendLike(total: number) {
  return async (query: any) => {
    const { page, pageSize } = query.paging;
    const size = Math.min(pageSize, 100);
    const start = (page - 1) * size;
    const items = Array.from({ length: Math.max(0, Math.min(size, total - start)) }, (_, i) => ({
      id: start + i + 1,
    }));
    return { items, total };
  };
}

describe('fetchAllRows', () => {
  // 修复前 PAGE_SIZE=1000：total=5000 时只拉 5 页、每页实际 100 行，导出 500 行。
  it('exports every row when total is under maxRows', async () => {
    const rows = await fetchAllRows({
      fetcher: backendLike(5000),
      filename: 'x',
      columns: [{ key: 'id', title: 'id' }],
    });
    expect(rows).toHaveLength(5000);
    expect(rows.map((r) => r.id)).toEqual(Array.from({ length: 5000 }, (_, i) => i + 1));
  });

  it('stops at maxRows', async () => {
    const rows = await fetchAllRows({
      fetcher: backendLike(50_000),
      filename: 'x',
      columns: [{ key: 'id', title: 'id' }],
      maxRows: 250,
    });
    expect(rows).toHaveLength(250);
  });
});
