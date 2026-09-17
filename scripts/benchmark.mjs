import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, readdirSync, writeFileSync, rmSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const binary = path.resolve(process.argv[2] || path.join('dist', readdirSync('dist').find(name => name.startsWith('commit-guard_'))));
const samples = Number(process.env.COMMIT_GUARD_BENCH_SAMPLES || 20);
if (!Number.isInteger(samples) || samples < 3 || samples > 1000) throw new Error('Samples must be an integer from 3 to 1000');
const tempBase = path.resolve(os.tmpdir());
const root = mkdtempSync(path.join(tempBase, 'commit-guard-benchmark-'));
const config = path.join(root, '.commit-guard.json');
writeFileSync(config, '{}');
const env = { ...process.env, GIT_CONFIG_NOSYSTEM: '1', GIT_CONFIG_GLOBAL: process.platform === 'win32' ? 'NUL' : '/dev/null' };

function execute(command, args, input) {
  const started = process.hrtime.bigint();
  const result = spawnSync(command, args, { cwd: root, input, encoding: 'utf8', env, windowsHide: true });
  if (result.error || result.status !== 0) throw new Error(`${command}: ${result.error || result.stderr}`);
  return Number(process.hrtime.bigint() - started) / 1e6;
}

function measure(command, args, input) {
  execute(command, args, input);
  const values = Array.from({ length: samples }, () => execute(command, args, input)).sort((a, b) => a - b);
  return { median_ms: Number(values[Math.floor(values.length / 2)].toFixed(3)), p95_ms: Number(values[Math.ceil(values.length * 0.95) - 1].toFixed(3)) };
}

try {
  const one = JSON.stringify([{ sha: 'benchmark', message: 'feat: Benchmark startup.' }]);
  const hundred = JSON.stringify(Array.from({ length: 100 }, (_, i) => ({ sha: String(i), message: 'feat: Benchmark batch.' })));
  const args = ['check', '--batch', '--config', config, '--json'];
  const result = { platform: `${process.platform}/${process.arch}`, samples, single: measure(binary, args, one), batch100: measure(binary, args, hundred) };
  execute('git', ['init', '-q']);
  execute('git', ['config', 'user.name', 'Benchmark']);
  execute('git', ['config', 'user.email', 'benchmark@example.invalid']);
  execute('git', ['config', 'commit.gpgsign', 'false']);
  execute(binary, ['install']);
  result.gitCommitBaseline = measure('git', ['-c', 'core.hooksPath=', 'commit', '--allow-empty', '-qm', 'feat: Benchmark baseline.']);
  result.gitCommitWithHook = measure('git', ['commit', '--allow-empty', '-qm', 'feat: Benchmark hook.']);
  result.estimatedMedianHookOverhead_ms = Number((result.gitCommitWithHook.median_ms - result.gitCommitBaseline.median_ms).toFixed(3));
  console.log(JSON.stringify(result, null, 2));
} finally {
  // The benchmark owns this uniquely created temporary directory only.
  if (path.dirname(root) === tempBase && path.basename(root).startsWith('commit-guard-benchmark-')) {
    rmSync(root, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 });
  }
}
