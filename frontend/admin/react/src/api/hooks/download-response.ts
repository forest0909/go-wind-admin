/**
 * 文件下载接口（admin/v1/file/download）的 axios transformResponse。
 *
 * 成功响应是文件原始字节，不论 Content-Type（包括 application/json 文件）一律原样返回 ArrayBuffer；
 * 只有 HTTP 出错时响应体才是 JSON 错误（含 reason），解析回对象，全局错误提示才能按 reason 翻译。
 */
export function downloadTransformResponse(
  body: unknown,
  headers?: Record<string, unknown>,
  status?: number,
): unknown {
  // 用 toString 判断而不是 instanceof：跨 realm（iframe、测试环境）的 ArrayBuffer 也要认
  if (status === undefined || status < 400 || Object.prototype.toString.call(body) !== '[object ArrayBuffer]') {
    return body;
  }
  const ct = String(headers?.['content-type'] ?? '');
  if (!ct.includes('application/json')) {
    return body;
  }
  try {
    return JSON.parse(new TextDecoder().decode(body as ArrayBuffer));
  } catch (error) {
    // 错误体不是合法 JSON：保留原始字节，交给通用错误提示
    console.warn('download: failed to parse error body as JSON', error);
    return body;
  }
}
