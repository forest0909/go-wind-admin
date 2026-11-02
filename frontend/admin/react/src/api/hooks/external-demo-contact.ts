import {
  useMutation,
  type UseMutationOptions,
  useQuery,
  type UseQueryOptions,
} from '@tanstack/react-query';
import {
  type externalservicev1_CreateDemoContactRequest,
  type externalservicev1_DeleteDemoContactRequest,
  type externalservicev1_DemoContact,
  type externalservicev1_GetDemoContactRequest,
  type externalservicev1_ListDemoContactResponse,
} from '@/api/generated/admin/service/v1';
import { makeUpdateMask, type PaginationQuery, queryClient } from '@/core';
import { apiClient } from '@/api/client';

// ==============================
// 外部数据示例：联系人（外部数据源 demo）
// 外部资源的 id 是字符串，Update/Delete 的 id 不要转成数字。
// ==============================

export function useListExternalDemoContacts(
  query: PaginationQuery,
  options?: UseQueryOptions<externalservicev1_ListDemoContactResponse, Error>,
) {
  return useQuery({
    queryKey: ['listExternalDemoContacts', query],
    queryFn: () => apiClient.externalDemoContactService.List(query.toRawParams()),
    ...options,
  });
}

export async function fetchListExternalDemoContacts(params: PaginationQuery) {
  return queryClient.fetchQuery({
    queryKey: ['listExternalDemoContacts', params],
    queryFn: () => apiClient.externalDemoContactService.List(params.toRawParams()),
    retry: 0,
  });
}

export function useGetExternalDemoContact(
  req: externalservicev1_GetDemoContactRequest,
  options?: UseQueryOptions<externalservicev1_DemoContact, Error>,
) {
  return useQuery({
    queryKey: ['getExternalDemoContact', req],
    queryFn: () => apiClient.externalDemoContactService.Get(req),
    ...options,
  });
}

export function useCreateExternalDemoContact(
  options?: UseMutationOptions<unknown, Error, externalservicev1_CreateDemoContactRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.externalDemoContactService.Create(data),
    ...options,
  });
}

export function useUpdateExternalDemoContact(
  options?: UseMutationOptions<unknown, Error, { id: string; values: externalservicev1_DemoContact }>,
) {
  return useMutation({
    mutationFn: ({ id, values }: { id: string; values: externalservicev1_DemoContact }) =>
      apiClient.externalDemoContactService.Update({
        id,
        data: { ...values },
        updateMask: makeUpdateMask(Object.keys(values ?? {})),
      }),
    ...options,
  });
}

export function useDeleteExternalDemoContact(
  options?: UseMutationOptions<unknown, Error, externalservicev1_DeleteDemoContactRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.externalDemoContactService.Delete(data),
    ...options,
  });
}
