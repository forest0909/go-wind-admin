#!/usr/bin/env node
// Update-mask allowlist parity check.
//
// A handful of edit forms still hand-write their update FieldMask as a
// literal — or as a named constant holding one — instead of deriving it
// with makeUpdateMask(). Each such string is an allowlist: "these fields,
// and only these fields, may be written". They drift silently in both
// directions: a form field added without updating the mask silently stops
// saving; a field added to the mask silently becomes writable; and one
// frontend can quietly diverge from the other two. The literals survive
// because each one deliberately excludes something — an immutable field,
// credentials that ride as dedicated request fields with empty-means-keep
// server semantics, or (in one case) a narrow toggle update distinct from
// the full form mask; the per-site reasons are recorded inline in the
// registry below.
//
// This check enforces two properties:
//
//   1. Pinned values: every registered site carries exactly its registered
//      mask — literals are compared per file as multisets (a missing,
//      duplicated, extra or altered literal fails), constant references as
//      sets, and named mask constants as exact values. The three
//      frontends' equivalent sites are pinned to the same canonical
//      string, so the mask CONTENT is parity-checked even where the
//      surrounding files differ.
//   2. Closed allowlist: no other updateMask literal, constant reference,
//      or `const updateMask =` binding may appear anywhere in the three
//      application source trees. New code derives masks with
//      makeUpdateMask() from transport/rest; adding a genuinely new
//      allowlist site is a conscious decision that must be registered
//      here.
//
// Sweep exclusions: api/generated/** (make-ts output whose
// `updateMask: undefined | wellKnownFieldMask` lines are type
// declarations, not assignments) and the makeUpdateMask identifier itself
// (the sanctioned generic form). This checkout scans the React tree only.
//
// Usage: node scripts/check-update-mask-allowlists.mjs

import { existsSync, readFileSync, readdirSync } from 'node:fs';

// Canonical mask values: one entry per distinct allowlist. Every site
// carrying a given allowlist — inline literal or named constant, in any of
// the three frontends — is pinned to the same canonical string, which is
// what makes the three frontends' mask content comparable.
const MASKS = {
  // Built-in channel types. Credentials stay on dedicated request fields
  // (password / webhookSecret / smsAccessSecret) and must not enter the mask.
  notificationChannel:
    'name,type,smtpHost,smtpPort,smtpUsername,smtpFrom,smtpFromName,smtpTls,webhookUrl,webhookSignStyle,webhookPayloadTemplate,smsProvider,smsAccessKeyId,smsSignName,smsTemplateCode,smsAppId,smsRegion,enabled,remark',
  // Extension channel types: the dynamic settings object replaces the
  // built-in SMTP/webhook/SMS columns.
  notificationChannelExt: 'name,type,settings,enabled,remark',
  // Notification rule form. event_type is immutable after creation.
  notificationRule: 'channel,isAsync,isEnabled,remark',
  // Notification template form. code is immutable after creation.
  notificationTemplate:
    'name,titleTemplate,contentTemplate,isEnabled,remark',
  monitorAlert:
    'name,metric,op,threshold,cooldownMinutes,channel,target,isEnabled,remark',
  aiAgent: 'name,description,enabled',
  aiAgentGraph: 'graph',
};

// Registered allowlist sites. Each entry pins, for one file, the complete
// set of updateMask usages that file may contain:
//   expectStrings — every literal mask value in the file, compared as a
//     multiset (counts included: a duplicated literal fails too)
//   expectIdents  — every constant-referenced mask in the file
//   defs          — named mask constants DEFINED in the file, pinned to
//     their exact value (usage and definition may live in different
//     files across the three frontends)
const REGISTRY = [
  {
    // Ternary: extension channels write settings, built-in channels write
    // the SMTP/webhook/SMS columns. Both branches are pinned.
    path: 'frontend/admin/react/src/pages/app/notification/channel/index.tsx',
    expectStrings: [MASKS.notificationChannelExt, MASKS.notificationChannel],
    expectIdents: [],
    defs: [],
  },
  {
    path: 'frontend/admin/react/src/pages/app/notification/rule/index.tsx',
    expectStrings: [MASKS.notificationRule],
    expectIdents: [],
    defs: [],
  },
  {
    path: 'frontend/admin/react/src/pages/app/notification/template/index.tsx',
    expectStrings: [MASKS.notificationTemplate],
    expectIdents: [],
    defs: [],
  },
  {
    path: 'frontend/admin/react/src/pages/app/system/monitor-alert/index.tsx',
    expectStrings: [MASKS.monitorAlert],
    expectIdents: [],
    defs: [],
  },
  {
    path: 'frontend/admin/react/src/pages/app/ai/agent/index.tsx',
    expectStrings: [MASKS.aiAgent],
    expectIdents: [],
    defs: [],
  },
  {
    path: 'frontend/admin/react/src/pages/app/ai/agent/agent-canvas.tsx',
    expectStrings: [MASKS.aiAgentGraph],
    expectIdents: [],
    defs: [],
  },
];

// This checkout keeps the React application tree only.
const ROOTS = ['frontend/admin/react/src'];

const SKIP_DIRS = new Set(['node_modules', 'dist', '.turbo', 'coverage']);
const SCANNABLE_RE = /\.(ts|tsx|vue|js|jsx|mjs)$/;

// updateMask usages. \s spans newlines, so newline-wrapped literals are
// caught too.
const STRING_USAGE_RE = /updateMask\s*:\s*(['"`])([^'"`\n]*)\1/g;
const TERNARY_USAGE_RE =
  /updateMask\s*:\s*[A-Za-z_$][\w$]*\([^)]*\)\s*\?\s*(['"`])([^'"`\n]*)\1\s*:\s*(['"`])([^'"`\n]*)\3/g;
const IDENT_USAGE_RE =
  /updateMask\s*:\s*([A-Za-z_$][A-Za-z0-9_$]*)\b(?!\s*\()/g;
const UPDATEMASK_BINDING_RE = /\b(?:const|let|var)\s+updateMask\s*=/g;

function defPattern(name) {
  return new RegExp(
    "(?:export\\s+)?const\\s+" + name + "\\s*=\\s*(['\"`])([^'\"`\\n]*)\\1"
  );
}

function* walk(dir) {
  let entries;
  try {
    entries = readdirSync(dir, { withFileTypes: true });
  } catch {
    return; // unreadable directory (e.g. a root that no longer exists)
  }
  for (const entry of entries) {
    const child = `${dir}/${entry.name}`;
    if (entry.isDirectory()) {
      if (SKIP_DIRS.has(entry.name)) continue;
      yield* walk(child);
    } else if (SCANNABLE_RE.test(entry.name)) {
      yield child;
    }
  }
}

function tally(items) {
  const counts = new Map();
  for (const item of items) counts.set(item, (counts.get(item) ?? 0) + 1);
  return counts;
}

function diff(expected, found) {
  const expectedCounts = tally(expected);
  const foundCounts = tally(found);
  const missing = [];
  const extra = [];
  for (const [item, count] of expectedCounts) {
    if ((foundCounts.get(item) ?? 0) < count) missing.push(item);
  }
  for (const [item, count] of foundCounts) {
    if (count > (expectedCounts.get(item) ?? 0)) extra.push(item);
  }
  return { missing, extra };
}

const problems = [];
let scanned = 0;
const registryByPath = new Map(REGISTRY.map((entry) => [entry.path, entry]));
const visited = new Set();

for (const entry of REGISTRY) {
  if (!existsSync(entry.path)) {
    problems.push(
      `registered site file missing: ${entry.path} — the file moved or was deleted; update the registry in scripts/check-update-mask-allowlists.mjs`
    );
  }
}

for (const root of ROOTS) {
  for (const file of walk(root)) {
    if (file.includes('/api/generated/')) continue;
    scanned += 1;
    let content;
    try {
      content = readFileSync(file, 'utf8');
    } catch (err) {
      problems.push(`cannot read ${file}: ${err.message}`);
      continue;
    }
    const stringHits = [
      ...[...content.matchAll(STRING_USAGE_RE)].map((m) => m[2]),
      ...[...content.matchAll(TERNARY_USAGE_RE)].flatMap((m) => [m[2], m[4]]),
    ];
    const identHits = [...content.matchAll(IDENT_USAGE_RE)]
      .map((m) => m[1])
      .filter((name) => name !== 'makeUpdateMask');
    const bindingHits = content.match(UPDATEMASK_BINDING_RE) ?? [];
    const entry = registryByPath.get(file);
    if (!entry) {
      for (const value of stringHits) {
        problems.push(
          `unregistered updateMask literal in ${file}: ${JSON.stringify(value)} — derive masks with makeUpdateMask() from transport/rest, or consciously register the site in scripts/check-update-mask-allowlists.mjs`
        );
      }
      for (const name of identHits) {
        problems.push(
          `unregistered updateMask constant reference in ${file}: ${name} — register the site or switch to makeUpdateMask()`
        );
      }
      for (const hit of bindingHits) {
        problems.push(
          `unregistered updateMask binding in ${file} (${hit}) — bind the mask through makeUpdateMask(), not a local updateMask variable`
        );
      }
      continue;
    }
    visited.add(entry.path);
    const stringDiff = diff(entry.expectStrings, stringHits);
    for (const value of stringDiff.missing) {
      problems.push(
        `registered mask site missing in ${file}: expected ${JSON.stringify(value)} was not found — the site was removed or restructured; update the registry`
      );
    }
    for (const value of stringDiff.extra) {
      problems.push(
        `mask drift in ${file}: unexpected literal ${JSON.stringify(value)} — the allowlisted field set changed; reconcile across the three frontends, then update MASKS and the registry`
      );
    }
    const identDiff = diff(entry.expectIdents, identHits);
    for (const name of identDiff.missing) {
      problems.push(
        `registered constant reference missing in ${file}: expected ${name} was not found — the site was removed or restructured; update the registry`
      );
    }
    for (const name of identDiff.extra) {
      problems.push(
        `constant-reference drift in ${file}: unexpected reference ${name} — reconcile, then update the registry`
      );
    }
    for (const def of entry.defs) {
      const match = content.match(defPattern(def.name));
      if (!match) {
        problems.push(
          `mask constant ${def.name} not found in ${file} — the definition moved or was renamed; update the registry`
        );
        continue;
      }
      if (match[2] !== def.value) {
        problems.push(
          `mask constant ${def.name} in ${file} drifted: expected ${JSON.stringify(def.value)}, found ${JSON.stringify(match[2])} — reconcile across the three frontends, then update MASKS and the registry`
        );
      }
    }
  }
}

for (const entry of REGISTRY) {
  if (!visited.has(entry.path) && existsSync(entry.path)) {
    problems.push(
      `registered site file was not reached by the sweep: ${entry.path} — the ROOTS list no longer covers it`
    );
  }
}

if (problems.length > 0) {
  console.error(
    `update-mask allowlist check FAILED (${problems.length} problem(s)):`
  );
  for (const problem of problems) console.error('  - ' + problem);
  console.error(
    'fix: registered allowlist sites must carry exactly their registered\n' +
      'mask, and the allowlist is closed — new updateMask usage must go\n' +
      'through makeUpdateMask() from transport/rest or be consciously\n' +
      'registered in scripts/check-update-mask-allowlists.mjs.'
  );
  process.exit(1);
}

const pinned = REGISTRY.reduce(
  (sum, entry) =>
    sum + entry.expectStrings.length + entry.expectIdents.length + entry.defs.length,
  0
);

console.log(
  `update-mask allowlist check OK: ${scanned} file(s) scanned across ${ROOTS.length} frontend trees, ${pinned} registered allowlist assertion(s) verified, no unregistered updateMask usage.`
);
