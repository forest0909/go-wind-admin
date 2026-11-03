import { useMutation, type UseMutationOptions } from '@tanstack/react-query';
import { apiClient } from '@/api/client';
import { RequestClient } from '@/core';
import { downloadTransformResponse } from './download-response';

// ==============================
// 文件下载 / 上传辅助函数
// ==============================

/**
 * 按文件记录 ID 下载文件（后端不再接受直接给 bucket/objectName）
 * @param fileId 文件记录 ID
 * @param fileName 保存到本地时的文件名
 * @param preferPresignedUrl 是否优先使用预签名URL下载
 */
async function downloadFile(
  fileId: number,
  fileName: string,
  preferPresignedUrl: boolean,
) {
  if (preferPresignedUrl) {
    const resp = await apiClient.fileTransferService.DownloadFile({
      fileId,
      preferPresignedUrl,
    });

    const url = (resp as any).downloadUrl || '';
    if (!url) return;

    console.log('Downloading file transfer...', url);

    const a = document.createElement('a');
    a.href = url;
    a.target = '_blank';
    a.download = fileName || 'download';
    document.body.append(a);
    a.click();
    a.remove();
    return;
  }

  // 后端直接返回文件字节，不能走默认的 JSON 解析（二进制会被当成文本损坏，.json 文件会被解析成对象）。
  const data = await RequestClient.getInstance().get<ArrayBuffer>('admin/v1/file/download', {
    params: { fileId, preferPresignedUrl },
    responseType: 'arraybuffer',
    transformResponse: [downloadTransformResponse],
  } as never);

  const blob = new Blob([data]);
  const objectUrl = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = objectUrl;
  a.download = fileName || 'download';
  document.body.append(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(objectUrl);
}

/**
 * 上传文件到MinIO（桶和对象名由后端决定）
 * @param fileDirectory 远端存储文件目录
 * @param fileData 文件数据
 * @param method 上传方法，支持 'post' 和 'put'
 * @param onUploadProgress 上传进度回调函数
 */
export async function uploadFile(
  fileDirectory: string,
  fileData: File,
  method: 'post' | 'put' = 'post',
  onUploadProgress?: (progressEvent: any) => void,
) {
  // kratos 未注册 form-data codec，multipart 上传是死路径（后端 400
  // unregister Content-Type，go 参照后端同型）。JSON 形态（bytes 走 base64）
  // 才是生成客户端定义的活路；手工组 protojson 同形请求体以保留
  // axios 的 onUploadProgress（生成客户端的 transport.unary 不支持上传进度）。
  // 响应经 ResponseData 拦截器解包，直接是 UploadFileResponse（含 publicUrl）。
  const file = await fileToBase64(fileData);
  const body = {
    file,
    mime: fileData.type,
    size: fileData.size,
    sourceFileName: fileData.name,
    storageObject: { fileDirectory },
  };
  return await RequestClient.getInstance().request('admin/v1/file/upload', {
    method: method === 'put' ? 'PUT' : 'POST',
    data: body,
    onUploadProgress,
  });
}

function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error ?? new Error('文件读取失败'));
    reader.onload = () => {
      const dataUrl = String(reader.result ?? '');
      const idx = dataUrl.indexOf('base64,');
      resolve(idx >= 0 ? dataUrl.slice(idx + 'base64,'.length) : '');
    };
    reader.readAsDataURL(file);
  });
}

// -----------------------------------------------------------------------------
// 下载文件 Hook
// -----------------------------------------------------------------------------
export function useDownloadFile(
  options?: UseMutationOptions<
    void,
    Error,
    {
      fileId: number;
      fileName: string;
      preferPresignedUrl?: boolean;
    }
  >,
) {
  return useMutation({
    mutationFn: async ({ fileId, fileName, preferPresignedUrl = false }) => {
      return downloadFile(fileId, fileName, preferPresignedUrl);
    },
    ...options,
  });
}

// -----------------------------------------------------------------------------
// 上传文件 Hook（支持进度）
// -----------------------------------------------------------------------------
export function useUploadFile(
  options?: UseMutationOptions<
    void,
    Error,
    {
      fileDirectory: string;
      file: File;
      method?: 'post' | 'put';
      onUploadProgress?: (progress: any) => void;
    }
  >,
) {
  return useMutation({
    mutationFn: async ({ fileDirectory, file, method = 'post', onUploadProgress }) => {
      await uploadFile(fileDirectory, file, method, onUploadProgress);
    },
    ...options,
  });
}
