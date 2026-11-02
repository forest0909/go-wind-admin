import { describe, expect, it } from 'vitest';
import { EXTERNAL_SOURCE_REASON, getErrorReason } from '../src/utils/external-source';
import { AMOUNT_PATTERN } from '../src/pages/app/system/external-demo-contact/constants';

describe('getErrorReason', () => {
  it('reads reason from the thrown backend response body', () => {
    const body = { code: 503, reason: 'EXTERNAL_SOURCE_NOT_CONFIGURED', message: '外部数据源未配置' };
    expect(getErrorReason(body)).toBe(EXTERNAL_SOURCE_REASON.NOT_CONFIGURED);
  });

  it('returns undefined when there is no reason', () => {
    expect(getErrorReason(new Error('Network Error'))).toBeUndefined();
    expect(getErrorReason({ reason: '' })).toBeUndefined();
    expect(getErrorReason(undefined)).toBeUndefined();
  });
});

describe('AMOUNT_PATTERN', () => {
  it('accepts decimal strings with at most two fraction digits', () => {
    for (const v of ['0', '1234', '1234.5', '1234.50', '-12.30', '9999999999999999.99']) {
      expect(AMOUNT_PATTERN.test(v), v).toBe(true);
    }
  });

  it('rejects malformed or over-precise amounts', () => {
    for (const v of ['', '1.234', '1e3', '.5', '1.', '+1', '1,000', ' 1', '12345678901234567']) {
      expect(AMOUNT_PATTERN.test(v), v).toBe(false);
    }
  });
});
