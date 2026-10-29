import { RequestClient } from './request-client';

export function requestApi({
  path,
  method,
  body,
  headers,
  timeout,
}: {
  path: string;
  method: string;
  body: null | string;
  headers?: Record<string, string>;
  /** 覆盖默认的 10 秒超时（毫秒） */
  timeout?: number;
}) {
  return RequestClient.getInstance().request(path, {
    method,
    data: body,
    headers,
    ...(timeout ? { timeout } : {}),
  } as never);
}
