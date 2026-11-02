// Tests for check-error-reason-i18n.mjs. Run: node --test scripts/check-error-reason-i18n.test.mjs
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const SCRIPT = join(dirname(fileURLToPath(import.meta.url)), 'check-error-reason-i18n.mjs');

// run builds a fixture tree (one proto, zh-CN + en-US locales) and runs the script on it.
function run(proto, reasonsByLocale) {
  const root = mkdtempSync(join(tmpdir(), 'reason-i18n-'));
  try {
    const protoDir = join(root, 'protos', 'demo', 'v1');
    mkdirSync(protoDir, { recursive: true });
    writeFileSync(join(protoDir, 'demo_error.proto'), proto);
    for (const [locale, reason] of Object.entries(reasonsByLocale)) {
      const dir = join(root, 'locales', locale, '_core');
      mkdirSync(dir, { recursive: true });
      writeFileSync(join(dir, 'common.json'), JSON.stringify({ request: { reason } }));
    }
    const r = spawnSync(process.execPath, [SCRIPT, '--proto-root', join(root, 'protos'), '--locale-root', join(root, 'locales')], {
      encoding: 'utf8',
    });
    return { code: r.status, out: r.stdout + r.stderr };
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

const both = (reason) => ({ 'zh-CN': reason, 'en-US': reason });

const PROTO = `syntax = "proto3";
package demo.v1;
import "errors/errors.proto";
enum DemoErrorReason {
  option (errors.default_code) = 500;
  BAD_REQUEST = 0 [(errors.code) = 400]; // 错误请求
  DEMO_FAILED = 1;
}
`;

test('all reasons translated passes', () => {
  const r = run(PROTO, both({ BAD_REQUEST: 'bad', DEMO_FAILED: 'failed' }));
  assert.equal(r.code, 0, r.out);
  assert.match(r.out, /2 reason\(s\)/);
});

test('missing key fails', () => {
  const r = run(PROTO, both({ BAD_REQUEST: 'bad' }));
  assert.equal(r.code, 1, r.out);
  assert.match(r.out, /DEMO_FAILED/);
});

test('empty or whitespace value fails', () => {
  const r = run(PROTO, { 'zh-CN': { BAD_REQUEST: 'bad', DEMO_FAILED: '失败' }, 'en-US': { BAD_REQUEST: 'bad', DEMO_FAILED: '  ' } });
  assert.equal(r.code, 1, r.out);
  assert.match(r.out, /en-US[\s\S]*DEMO_FAILED/);
  const empty = run(PROTO, { 'zh-CN': { BAD_REQUEST: 'bad', DEMO_FAILED: '' }, 'en-US': { BAD_REQUEST: 'bad', DEMO_FAILED: 'x' } });
  assert.equal(empty.code, 1, empty.out);
});

test('non-string value fails', () => {
  const r = run(PROTO, both({ BAD_REQUEST: 'bad', DEMO_FAILED: { text: 'x' } }));
  assert.equal(r.code, 1, r.out);
  const num = run(PROTO, both({ BAD_REQUEST: 'bad', DEMO_FAILED: 1 }));
  assert.equal(num.code, 1, num.out);
});

test('indented closing brace still parses the enum', () => {
  const indented = PROTO.replace(/\n\}\n$/, '\n    }\n');
  assert.notEqual(indented, PROTO);
  const r = run(indented, both({ BAD_REQUEST: 'bad' }));
  assert.equal(r.code, 1, r.out);
  assert.match(r.out, /DEMO_FAILED/);
});

test('single-line enum and braces inside comments are handled', () => {
  const proto = `import "errors/errors.proto";
// enum Fake { } } comment braces must not end the enum
enum DemoErrorReason { option (errors.default_code) = 500; /* } */ ONE_LINE_REASON = 3; }
`;
  const r = run(proto, both({}));
  assert.equal(r.code, 1, r.out);
  assert.match(r.out, /ONE_LINE_REASON/);
});

test('file with errors.default_code but no recognizable enum fails closed', () => {
  const r = run('import "errors/errors.proto";\noption (errors.default_code) = 500;\n', both({}));
  assert.equal(r.code, 1, r.out);
  assert.match(r.out, /no error enum was recognized/);
});

test('error enum without values fails closed', () => {
  const r = run('enum Empty {\n  option (errors.default_code) = 500;\n}\n', both({}));
  assert.equal(r.code, 1, r.out);
  assert.match(r.out, /no recognizable values/);
});
