/**
 * 外部数据源页面共用：后端 ExternalErrorReason（backend/api/protos/external/service/v1/external_error.proto）
 * 里页面需要区别处理的 reason。译文在 common.json 的 request.reason.*，
 * 请求层已把 error.message 换成译文，这里只用 reason 做分支判断。
 */
export const EXTERNAL_SOURCE_REASON = {
  NOT_CONFIGURED: 'EXTERNAL_SOURCE_NOT_CONFIGURED',
  READ_ONLY: 'EXTERNAL_SOURCE_READ_ONLY',
  UNAVAILABLE: 'EXTERNAL_SOURCE_UNAVAILABLE',
  TIMEOUT: 'EXTERNAL_SOURCE_TIMEOUT',
} as const;

/** 取接口错误的 reason（请求层抛出的是后端响应体 { code, reason, message }）；取不到返回 undefined */
export function getErrorReason(error: unknown): string | undefined {
  if (error && typeof error === 'object' && 'reason' in error) {
    const { reason } = error as { reason?: unknown };
    return typeof reason === 'string' && reason ? reason : undefined;
  }
  return undefined;
}
