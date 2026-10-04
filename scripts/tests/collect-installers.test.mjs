import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, writeFile, readFile, readdir, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { collectInstallers } from '../collect-installers.mjs';

const windows = 'x86_64-pc-windows-msvc';
async function fixture(t) {
  const root = await mkdtemp(path.join(tmpdir(), 'menuvex-packaging-test-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  await mkdir(path.join(root, 'src-tauri'), { recursive: true });
  await writeFile(path.join(root, 'src-tauri/tauri.conf.json'), '{"version":"1.0.0"}');
  return root;
}
async function add(root, target, folder, name) {
  const directory = path.join(root, 'src-tauri/target', target, 'release/bundle', folder);
  await mkdir(directory, { recursive: true });
  await writeFile(path.join(directory, name), 'test fixture bytes; not an executable');
}

test('stages installer-only output with deterministic name and checksum', async t => {
  const root = await fixture(t);
  await add(root, windows, 'nsis', 'original-setup.exe');
  await add(root, windows, 'nsis', 'not-an-installer.txt');
  const output = await collectInstallers(root, windows);
  const name = 'MenuVex-Printer-Agent-1.0.0-windows-x64-setup.exe';
  assert.deepEqual((await readdir(output)).sort(), [name, 'READ-ME-FIRST.txt', 'SHA256SUMS.txt', 'manifest.json'].sort());
  const manifest = JSON.parse(await readFile(path.join(output, 'manifest.json')));
  assert.equal(manifest.channel, 'test-candidate');
  assert.equal(manifest.files[0].filename, name);
  assert.equal(manifest.files[0].sha256, createHash('sha256').update(await readFile(path.join(output, name))).digest('hex'));
});

test('rejects missing builds instead of publishing source or an empty artifact', async t => {
  await assert.rejects(collectInstallers(await fixture(t), windows));
});

test('rejects ambiguous installers from multiple stale builds', async t => {
  const root = await fixture(t);
  await add(root, windows, 'nsis', 'first.exe');
  await add(root, windows, 'nsis', 'second.exe');
  await assert.rejects(collectInstallers(root, windows), /exactly one/);
});

test('requires both Linux packages', async t => {
  const root = await fixture(t);
  const target = 'x86_64-unknown-linux-gnu';
  await add(root, target, 'deb', 'app.deb');
  await assert.rejects(collectInstallers(root, target));
  await add(root, target, 'appimage', 'app.AppImage');
  const output = await collectInstallers(root, target);
  const manifest = JSON.parse(await readFile(path.join(output, 'manifest.json')));
  assert.equal(manifest.files.length, 2);
});

test('rejects unrecognized targets', async t => {
  await assert.rejects(collectInstallers(await fixture(t), '../other'), /Unsupported target/);
});
