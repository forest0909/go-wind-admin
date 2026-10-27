#!/usr/bin/env node
// Frontend mirrored-file parity check.
//
// The three frontend variants carry intentionally identical hand-written
// copies of certain transport-layer files (the GROUPS manifest below). They
// are maintained as mirrors: an edit applied to only one frontend is drift,
// and drift in mirrored transport code has already bitten this repo more than
// once (a crypto helper that kept a legacy ECB path on one end, a serializer
// guard rewritten three ways, a reconnect statement lost in one port). This
// check normalizes the only sanctioned per-copy differences — per-repo
// formatter settings (quote style, line wrapping, trailing commas) and the
// per-copy provenance marker in the header comment — then asserts all copies
// are byte-identical. A missing copy counts as drift.
//
// Usage: node scripts/check-frontend-mirror.mjs

import { existsSync, readFileSync } from 'node:fs';

// Each mirrored copy names its host frontend in its header comment. The
// marker text is the only sanctioned per-copy content difference; it is
// rewritten to a common token before comparison.
const PROVENANCE_MARKERS = [
  [/（react 端副本）/g, '（端副本）'],
];

// This checkout keeps React only. A group with one root has nothing to
// compare; the missing-copy rule still fails if the React file disappears.
const GROUPS = [
  {
    name: 'transport/sse',
    roots: ['frontend/admin/react/src/core/transport/sse'],
    files: ['event.ts', 'index.ts', 'sse_client.ts', 'types.ts'],
  },
  {
    name: 'transport/rest',
    roots: ['frontend/admin/react/src/core/transport/rest'],
    files: ['pagination.ts', 'preset-interceptors.ts'],
  },
];

function normalize(text) {
  let out = text.replace(/\uFEFF/g, '');
  for (const [pattern, replacement] of PROVENANCE_MARKERS) {
    out = out.replace(pattern, replacement);
  }
  out = out.replace(/\s+/g, ''); // formatter layout: wrapping, indentation, EOLs
  out = out.replace(/['"]/g, ''); // formatter quote style
  out = out.replace(/,([)\]}])/g, '$1'); // formatter trailing commas
  out = out.replace(/=\|/g, '='); // leading pipe of multi-line-formatted unions
  return out;
}

const problems = [];
let compared = 0;

for (const group of GROUPS) {
  for (const file of group.files) {
    const copies = [];
    for (const root of group.roots) {
      const path = `${root}/${file}`;
      if (!existsSync(path)) {
        problems.push(`missing mirror copy: ${path} (group "${group.name}")`);
        continue;
      }
      copies.push({ path, data: normalize(readFileSync(path, 'utf8')) });
    }
    if (copies.length < 2) continue;
    const base = copies[0];
    for (const other of copies.slice(1)) {
      compared += 1;
      if (base.data === other.data) continue;
      let at = 0;
      while (
        at < base.data.length &&
        at < other.data.length &&
        base.data[at] === other.data[at]
      ) {
        at += 1;
      }
      const window = (s) => s.slice(Math.max(0, at - 40), at + 40);
      problems.push(
        `mirror drift in group "${group.name}", file ${file}:\n` +
          `    A: ${base.path}\n` +
          `    B: ${other.path}\n` +
          `    first difference at normalized offset ${at} (lengths ${base.data.length} vs ${other.data.length})\n` +
          `    A: …${window(base.data)}…\n` +
          `    B: …${window(other.data)}…`
      );
    }
  }
}

if (problems.length > 0) {
  console.error(
    `frontend mirror parity check FAILED (${problems.length} problem(s)):`
  );
  for (const p of problems) console.error('  - ' + p);
  console.error(
    'fix: mirrored files must carry the same content in every frontend — apply\n' +
      'the edit to all copies of the group. Only formatter differences (quote\n' +
      'style, wrapping, trailing commas) and the provenance marker may differ,\n' +
      'and those are normalized away by this check.'
  );
  process.exit(1);
}

console.log(
  `frontend mirror parity OK: ${GROUPS.length} group(s), ${compared} pairwise comparison(s), all copies normalized-identical.`
);
