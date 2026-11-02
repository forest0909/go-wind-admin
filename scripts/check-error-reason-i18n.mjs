#!/usr/bin/env node
// Error-reason i18n coverage check.
//
// The React client translates backend errors by `reason` only
// (src/bootstrap.ts getErrorMsg → `request.reason.<REASON>` in the common
// namespace); the backend `message` is never shown. A reason declared in an
// error proto but missing from a locale renders as the generic "unknown
// error" text, so e.g. a storage-quota rejection (FILE_TOO_LARGE) looked like
// a random failure. This check asserts: every enum value of every
// `option (errors.default_code)` enum under the proto root is an own key of
// `request.reason` in each locale's _core/common.json, and its value is a
// non-empty string.
//
// The proto scan strips comments and matches braces by depth, so indentation
// and formatting do not matter. A file that mentions errors.default_code but
// yields no recognizable error enum (or an error enum with no values) fails
// closed instead of being skipped.
//
// Usage: node scripts/check-error-reason-i18n.mjs [--proto-root DIR] [--locale-root DIR]
// Tests: node --test scripts/check-error-reason-i18n.test.mjs

import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

function argValue(name, fallback) {
  const i = process.argv.indexOf(name);
  return i >= 0 && process.argv[i + 1] ? process.argv[i + 1] : fallback;
}

const PROTO_ROOT = argValue('--proto-root', 'backend/api/protos');
const LOCALE_ROOT = argValue('--locale-root', 'frontend/admin/react/src/locales');

function walk(dir) {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    return statSync(p).isDirectory() ? walk(p) : [p];
  });
}

// stripComments removes // and /* */ comments, keeping string literals intact.
function stripComments(src) {
  let out = '';
  for (let i = 0; i < src.length; i++) {
    const c = src[i];
    const n = src[i + 1];
    if (c === '"' || c === "'") {
      let j = i + 1;
      while (j < src.length && src[j] !== c) j += src[j] === '\\' ? 2 : 1;
      out += src.slice(i, j + 1);
      i = j;
    } else if (c === '/' && n === '/') {
      while (i < src.length && src[i] !== '\n') i++;
      out += '\n';
    } else if (c === '/' && n === '*') {
      const end = src.indexOf('*/', i + 2);
      i = end < 0 ? src.length : end + 1;
      out += ' ';
    } else {
      out += c;
    }
  }
  return out;
}

// enumBodies yields { name, body } for every enum, matching braces by depth.
function* enumBodies(src) {
  const re = /\benum\s+(\w+)\s*\{/g;
  let m;
  while ((m = re.exec(src)) !== null) {
    let depth = 1;
    let i = re.lastIndex;
    for (; i < src.length && depth > 0; i++) {
      if (src[i] === '{') depth++;
      else if (src[i] === '}') depth--;
    }
    yield { name: m[1], body: src.slice(re.lastIndex, i - 1) };
    re.lastIndex = i;
  }
}

const errors = [];
const reasons = new Map(); // reason -> first proto file declaring it

for (const file of walk(PROTO_ROOT).filter((f) => f.endsWith('.proto'))) {
  const src = stripComments(readFileSync(file, 'utf8'));
  if (!src.includes('errors.default_code')) continue;

  let errorEnums = 0;
  for (const { name, body } of enumBodies(src)) {
    if (!body.includes('errors.default_code')) continue;
    errorEnums++;
    // Enum values are statements: NAME = <int> [options];  (options/reserved lines never match)
    const values = [...body.matchAll(/(?:^|[;{}\s])([A-Z][A-Z0-9_]*)\s*=\s*-?\d+/g)].map((v) => v[1]);
    if (values.length === 0) {
      errors.push(`${file}: error enum ${name} has errors.default_code but no recognizable values`);
    }
    for (const v of values) if (!reasons.has(v)) reasons.set(v, file);
  }
  if (errorEnums === 0) {
    errors.push(`${file}: mentions errors.default_code but no error enum was recognized`);
  }
}

if (reasons.size === 0 && errors.length === 0) {
  errors.push(`no error reasons found under ${PROTO_ROOT}; the parser is broken`);
}

const locales = readdirSync(LOCALE_ROOT).filter((d) => statSync(join(LOCALE_ROOT, d)).isDirectory());
for (const locale of locales) {
  const file = join(LOCALE_ROOT, locale, '_core', 'common.json');
  const table = JSON.parse(readFileSync(file, 'utf8'))?.request?.reason ?? {};
  const bad = [...reasons.keys()]
    .filter((r) => {
      const v = Object.hasOwn(table, r) ? table[r] : undefined;
      return typeof v !== 'string' || v.trim() === '';
    })
    .sort();
  if (bad.length > 0) {
    errors.push(
      `${file}: missing or empty request.reason translations for ${bad.length} reason(s):\n` +
        bad.map((r) => `  ${r}  (${reasons.get(r)})`).join('\n'),
    );
  }
}

if (errors.length > 0) {
  for (const e of errors) console.error(e);
  process.exit(1);
}
console.log(`error reason i18n OK: ${reasons.size} reason(s) translated in ${locales.length} locale(s).`);
