import { mkdir, rm, readFile } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';

const root = path.resolve(fileURLToPath(new URL('..', import.meta.url)));
const version = (await readFile(path.join(root, 'VERSION'), 'utf8')).trim();
const current = process.argv.includes('--current');
const hostOS = process.platform === 'win32' ? 'windows' : process.platform;
const hostArch = process.arch === 'x64' ? 'amd64' : process.arch === 'ia32' ? '386' : process.arch;
const targets = current ? [[hostOS, hostArch]] : [['linux','amd64'],['linux','arm64'],['darwin','amd64'],['darwin','arm64'],['windows','amd64'],['windows','arm64']];
const go = process.env.GO ?? 'go';
const out = path.join(root, 'dist');
await rm(out, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 });
await mkdir(out, { recursive: true });
for (const [goos, goarch] of targets) {
  const suffix = goos === 'windows' ? '.exe' : '';
  const file = `commit-guard_${version}_${goos}_${goarch}${suffix}`;
  const result = spawnSync(go, ['build','-trimpath','-ldflags=-s -w', '-o', path.join(out, file), './cmd/commit-guard'], { cwd: root, env: { ...process.env, CGO_ENABLED: '0', GOOS: goos, GOARCH: goarch }, stdio: 'inherit' });
  if (result.status !== 0) process.exit(result.status ?? 1);
}
const artifacts = (await (await import('node:fs/promises')).readdir(out)).filter(name => name.startsWith('commit-guard_')).sort();
const checksums = [];
for (const name of artifacts) {
  const digest = createHash('sha256').update(await (await import('node:fs/promises')).readFile(path.join(out, name))).digest('hex');
  checksums.push(`${digest}  ${name}`);
}
await (await import('node:fs/promises')).writeFile(path.join(out, 'SHA256SUMS'), `${checksums.join('\n')}\n`);
console.log(`built ${targets.length} target(s) for v${version} in dist/`);
