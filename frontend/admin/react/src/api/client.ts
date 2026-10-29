/**
 * ApiClient 单例
 *
 * 生成的代码提供了统一的 ApiClient，它通过 ClientTransport 接口发送请求。
 * 这里将已有的 requestApi（基于 axios 的 RequestClient）适配为 ClientTransport，
 * 保留 token 注入、错误拦截、自动刷新等全部已有逻辑。
 */
import { type ClientTransport, createApiClient } from "@/api/generated/admin/service/v1";
import { consumeCaptchaHeaders, requestApi } from "@/core/transport/rest";

// 整表同步类接口要成批读写数据库，远程库上会超过默认的 10 秒超时
const LONG_RUNNING_TIMEOUT = 5 * 60_000;
const LONG_RUNNING_METHODS = new Set([
  'ApiService.SyncApis',
  'MenuService.SyncMenus',
  'PermissionService.SyncPermissions',
]);

const transport: ClientTransport = {
  unary(...args: Parameters<ClientTransport['unary']>) {
    // 参数个数由生成的接口规定，解构以满足 max-params
    const [path, method, body, meta] = args;
    // 消费登录前由 auth store 设置的验证码 Header（一次性，取后即清）
    const headers = consumeCaptchaHeaders() ?? undefined;
    const timeout = LONG_RUNNING_METHODS.has(`${meta.service}.${meta.method}`)
      ? LONG_RUNNING_TIMEOUT
      : undefined;
    return requestApi({ body, headers, method, path, timeout });
  },
  serverStream(path) {
    // SSE 流由 transport/sse 模块独立管理，此处不应被调用
    throw new Error(`serverStream not supported via ApiClient: ${path}`);
  },
  duplexStream(path) {
    throw new Error(`duplexStream not supported via ApiClient: ${path}`);
  },
};

export const apiClient = createApiClient(transport);
