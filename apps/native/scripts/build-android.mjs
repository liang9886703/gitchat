import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

// Keep an installable debug APK without shipping hundreds of MB of Rust DWARF.
// Developers can opt back into symbols through the usual Cargo environment.
const cli = fileURLToPath(new URL('../node_modules/@tauri-apps/cli/tauri.js', import.meta.url));
const result = spawnSync(process.execPath, [cli, 'android', 'build', '--debug', '--apk', ...process.argv.slice(2)], {
  cwd: fileURLToPath(new URL('../', import.meta.url)),
  env: {
    ...process.env,
    CARGO_PROFILE_DEV_DEBUG: process.env.CARGO_PROFILE_DEV_DEBUG ?? '0',
    CARGO_PROFILE_DEV_STRIP: process.env.CARGO_PROFILE_DEV_STRIP ?? 'debuginfo',
  },
  stdio: 'inherit',
});
if (result.error) throw result.error;
process.exit(result.status ?? 1);
