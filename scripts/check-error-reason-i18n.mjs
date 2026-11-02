#!/usr/bin/env node
// Error-reason i18n coverage check.
//
// The React client translates backend errors by `reason` only
// (src/bootstrap.ts getErrorMsg → `request.reason.<REASON>` in the common
// namespace); the backend `message` is never shown. A reason declared in an
// error proto but missing from a locale renders as the generic "unknown
// error" text, so e.g. a storage-quota rejection (FILE_TOO_LARGE) looked like
// a random failure. This check asserts: every enum value of every
// `option (errors.default_code)` enum under backend/api/protos is a key of
// `request.reason` in each locale's _core/common.json.
//
// Usage: node scripts/check-error-reason-i18n.mjs

import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

const PROTO_ROOT = 'backend/api/protos';
const LOCALE_ROOT = 'frontend/admin/react/src/locales';

function walk(dir) {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    return statSync(p).isDirectory() ? walk(p) : [p];
  });
}

const reasons = new Map(); // reason -> first proto file declaring it
for (const file of walk(PROTO_ROOT).filter((f) => f.endsWith('.proto'))) {
  const src = readFileSync(file, 'utf8');
  for (const m of src.matchAll(/enum\s+\w+\s*\{([\s\S]*?)\n\}/g)) {
    const body = m[1];
    if (!body.includes('errors.default_code')) continue;
    for (const v of body.matchAll(/^\s*([A-Z][A-Z0-9_]*)\s*=\s*\d+/gm)) {
      if (!reasons.has(v[1])) reasons.set(v[1], file);
    }
  }
}

if (reasons.size === 0) {
  console.error(`no error reasons found under ${PROTO_ROOT}; the parser is broken`);
  process.exit(1);
}

let failed = false;
const locales = readdirSync(LOCALE_ROOT).filter((d) => statSync(join(LOCALE_ROOT, d)).isDirectory());
for (const locale of locales) {
  const file = join(LOCALE_ROOT, locale, '_core', 'common.json');
  const keys = JSON.parse(readFileSync(file, 'utf8'))?.request?.reason ?? {};
  const missing = [...reasons.keys()].filter((r) => !(r in keys)).sort();
  if (missing.length > 0) {
    failed = true;
    console.error(`${file}: missing request.reason translations for ${missing.length} reason(s):`);
    for (const r of missing) console.error(`  ${r}  (${reasons.get(r)})`);
  }
}

if (failed) process.exit(1);
console.log(`error reason i18n OK: ${reasons.size} reason(s) translated in ${locales.length} locale(s).`);
