import { describe, expect, it } from 'vitest';
import { migratePreferences } from '../src/core/preferences/store';
import { defaultPreferences } from '../src/core/preferences/config/default';

function persisted(app: object, copyright: object, logo: object) {
  return { preferences: { app, copyright, logo } };
}

describe('preferences v3 → v4 品牌迁移', () => {
  it('仍是旧出厂值的品牌字段换成新默认值', () => {
    const { preferences } = migratePreferences(
      persisted(
        { name: 'GoWind Admin', accessMode: 'backend' },
        { companyName: 'GoWind', companySiteLink: 'https://www.gowind.cloud' },
        { enable: true, source: '/logo.png' },
      ),
      3,
    );
    expect(preferences.app.name).toBe('林间 Admin');
    expect(preferences.copyright.companyName).toBe(defaultPreferences.copyright.companyName);
    expect(preferences.copyright.companySiteLink).toBe(
      defaultPreferences.copyright.companySiteLink,
    );
    expect(preferences.logo.source).toBe('/logo.svg');
  });

  it('用户自己改过的值保留', () => {
    const { preferences } = migratePreferences(
      persisted(
        { name: '客户系统', accessMode: 'backend' },
        { companyName: '客户公司', companySiteLink: 'https://customer.example' },
        { enable: true, source: '/custom.png' },
      ),
      3,
    );
    expect(preferences.app.name).toBe('客户系统');
    expect(preferences.copyright.companyName).toBe('客户公司');
    expect(preferences.copyright.companySiteLink).toBe('https://customer.example');
    expect(preferences.logo.source).toBe('/custom.png');
  });
});
