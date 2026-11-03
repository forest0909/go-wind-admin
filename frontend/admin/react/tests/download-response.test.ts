import axios, { type AxiosAdapter } from 'axios';
import { describe, expect, it } from 'vitest';
import { downloadTransformResponse } from '../src/api/hooks/download-response';

function bytes(s: string): ArrayBuffer {
  return new Uint8Array(new TextEncoder().encode(s)).buffer as ArrayBuffer;
}

// 用真实 axios 走一遍 transformResponse（status 由 axios 传入），adapter 模拟后端响应。
function client(status: number, contentType: string, body: ArrayBuffer) {
  const adapter: AxiosAdapter = async (config) => {
    const response = {
      data: body,
      status,
      statusText: String(status),
      headers: { 'content-type': contentType },
      config,
      request: {},
    };
    if (status >= 400) {
      throw new axios.AxiosError('failed', String(status), config, {}, response as never);
    }
    return response as never;
  };
  return axios.create({ adapter, responseType: 'arraybuffer', transformResponse: [downloadTransformResponse] });
}

describe('downloadTransformResponse', () => {
  it('keeps a downloaded .json file byte-for-byte', async () => {
    const raw = '{"hello":"world","n":[1,2,3]}\n';
    const resp = await client(200, 'application/json', bytes(raw)).get('/admin/v1/file/download');
    expect(Object.prototype.toString.call(resp.data)).toBe('[object ArrayBuffer]');
    expect(new TextDecoder().decode(resp.data)).toBe(raw);
    const blob = new Blob([resp.data]);
    expect(new TextDecoder().decode(await blob.arrayBuffer())).toBe(raw);
  });

  it('keeps invalid JSON content of a successful download', async () => {
    const raw = '{not json';
    const resp = await client(200, 'application/json', bytes(raw)).get('/x');
    expect(new TextDecoder().decode(resp.data)).toBe(raw);
  });

  it('keeps binary content', async () => {
    const png = new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0xff, 0x00]);
    const resp = await client(200, 'image/png', png.buffer).get('/x');
    expect(new Uint8Array(resp.data)).toEqual(png);
  });

  it('parses the JSON error body so the reason can be translated', async () => {
    const err = await client(
      409,
      'application/json',
      bytes('{"code":409,"reason":"STORAGE_PROVIDER_MISMATCH","message":"x"}'),
    )
      .get('/x')
      .catch((e) => e);
    expect(err.response.data.reason).toBe('STORAGE_PROVIDER_MISMATCH');
  });
});
