import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

const read = path => readFile(new URL(`../${path}`, import.meta.url), 'utf8');

test('only the bundled launcher can invoke the three native commands', async () => {
  const capability = JSON.parse(await read('src-tauri/capabilities/launcher.json'));
  assert.equal(capability.local, true);
  assert.equal(capability.remote, undefined);
  assert.deepEqual(capability.webviews, ['main']);
  assert.deepEqual(capability.permissions.sort(), ['allow-connect-local', 'allow-connect-remote', 'allow-connection-info']);
  const build = await read('src-tauri/build.rs');
  for (const command of ['connection_info', 'connect_remote', 'connect_local']) assert.ok(build.includes(`"${command}"`));
  assert.ok(build.includes('.app_manifest('), 'custom commands must opt into the Tauri ACL');
  const config = JSON.parse(await read('src-tauri/tauri.conf.json'));
  assert.deepEqual(config.app.security.capabilities, ['launcher']);
  assert.ok(config.app.security.csp.includes("script-src 'self'"));
  assert.ok(!config.app.security.csp.includes('unsafe-eval'));
});

test('mobile packages cannot accidentally include a desktop sidecar', async () => {
  const base = JSON.parse(await read('src-tauri/tauri.conf.json'));
  assert.equal(base.bundle.externalBin, undefined);
  for (const platform of ['macos', 'windows']) {
    const config = JSON.parse(await read(`src-tauri/tauri.${platform}.conf.json`));
    assert.deepEqual(config.bundle.externalBin, ['binaries/gitchat-server']);
  }
});

test('Apple transport policy permits local networking without disabling HTTPS protection', async () => {
  for (const name of ['Info.plist', 'Info.ios.plist']) {
    const plist = await read(`src-tauri/${name}`);
    assert.ok(plist.includes('<key>NSAllowsLocalNetworking</key>'));
    assert.ok(!plist.includes('NSAllowsArbitraryLoads'));
  }
});
