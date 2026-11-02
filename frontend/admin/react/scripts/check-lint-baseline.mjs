// 只在 lint 的 error / warning 总数超过 docs/plans/react-lint-baseline.tsv 的条数时失败。
// 用法（在 frontend/admin/react 下）：node scripts/check-lint-baseline.mjs
import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';

const baselinePath = new URL('../../../../docs/plans/react-lint-baseline.tsv', import.meta.url);
const base = { error: 0, warning: 0 };
for (const line of readFileSync(baselinePath, 'utf8').split('\n')) {
  if (!line || line.startsWith('#')) continue;
  const severity = line.split('\t')[0];
  if (severity in base) base[severity]++;
}

let out;
try {
  out = execFileSync('npx', ['eslint', '.', '-f', 'json'], { encoding: 'utf8', maxBuffer: 1 << 30 });
} catch (err) {
  // eslint 有问题时退出码为 1，JSON 仍在 stdout；没有 stdout 说明是 eslint 自己崩了。
  if (!err.stdout) throw err;
  out = err.stdout;
}
const cur = { error: 0, warning: 0 };
for (const f of JSON.parse(out)) {
  cur.error += f.errorCount;
  cur.warning += f.warningCount;
}
console.log(`lint: error ${cur.error}/${base.error}, warning ${cur.warning}/${base.warning} (当前/基线)`);
if (cur.error > base.error || cur.warning > base.warning) {
  console.error('lint 问题数超过基线，请修复新增问题。');
  process.exit(1);
}
