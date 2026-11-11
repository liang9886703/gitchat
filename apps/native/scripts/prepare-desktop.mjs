import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { dirname, join, resolve } from 'node:path';
import { mkdirSync, copyFileSync } from 'node:fs';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const hostResult = spawnSync('rustc', ['-vV'], { encoding: 'utf8' });
if (hostResult.status !== 0) throw new Error('Rust toolchain is required');
const target = process.env.GITCHAT_NATIVE_TARGET || /host: (.+)/.exec(hostResult.stdout)?.[1];
const platforms = {
  'aarch64-apple-darwin': ['darwin', 'arm64'],
  'x86_64-apple-darwin': ['darwin', 'amd64'],
  'x86_64-pc-windows-msvc': ['windows', 'amd64'],
  'aarch64-pc-windows-msvc': ['windows', 'arm64'],
  'x86_64-unknown-linux-gnu': ['linux', 'amd64'],
};
if (!platforms[target]) throw new Error(`Unsupported desktop target: ${target}`);
const [goos, goarch] = platforms[target];
const directory = join(root, 'src-tauri', 'binaries');
mkdirSync(directory, { recursive: true });
const binary = join(directory, `gitchat-server-${target}${goos === 'windows' ? '.exe' : ''}`);
const result = spawnSync('go', ['build', '-trimpath', '-ldflags=-s -w', '-o', binary, '.'], {
  cwd: resolve(root, '../..'),
  env: { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: '0' },
  stdio: 'inherit',
});
if (result.status !== 0) process.exit(result.status || 1);
// The CLI copies externalBin alongside the packaged application. Keep the raw
// cargo run path usable as well, without modifying global PATH or installing Go.
const debugDir = join(root, 'src-tauri', 'target', 'debug');
mkdirSync(debugDir, { recursive: true });
copyFileSync(binary, join(debugDir, `gitchat-server${goos === 'windows' ? '.exe' : ''}`));
console.log(`Prepared ${target}: ${binary}`);
