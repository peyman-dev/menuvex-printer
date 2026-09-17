import { createHash } from 'node:crypto';
import { createReadStream } from 'node:fs';
import { copyFile, mkdir, readFile, readdir, rm, stat, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const targets = {
  'x86_64-pc-windows-msvc': {
    platform: 'windows', architecture: 'x64',
    bundles: [{ directory: 'nsis', extension: '.exe', suffix: '-setup.exe' }],
  },
  'x86_64-unknown-linux-gnu': {
    platform: 'linux', architecture: 'x64',
    bundles: [
      { directory: 'deb', extension: '.deb', suffix: '.deb' },
      { directory: 'appimage', extension: '.AppImage', suffix: '.AppImage' },
    ],
  },
  'aarch64-apple-darwin': {
    platform: 'macos', architecture: 'arm64',
    bundles: [{ directory: 'dmg', extension: '.dmg', suffix: '.dmg' }],
  },
  'x86_64-apple-darwin': {
    platform: 'macos', architecture: 'x64',
    bundles: [{ directory: 'dmg', extension: '.dmg', suffix: '.dmg' }],
  },
};

async function sha256(file) {
  const hash = createHash('sha256');
  for await (const chunk of createReadStream(file)) hash.update(chunk);
  return hash.digest('hex');
}

export async function collectInstallers(root, target) {
  const profile = targets[target];
  if (!profile) throw new Error(`Unsupported target: ${target}`);
  const config = JSON.parse(await readFile(path.join(root, 'src-tauri/tauri.conf.json'), 'utf8'));
  const version = config.version;
  if (typeof version !== 'string' || !/^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?$/.test(version)) {
    throw new Error('Invalid release version');
  }
  const source = path.join(root, 'src-tauri', 'target', target, 'release', 'bundle');
  // Validate every expected installer before staging anything. No success on a missing build.
  const inputs = [];
  for (const bundle of profile.bundles) {
    const directory = path.join(source, bundle.directory);
    const matches = (await readdir(directory, { withFileTypes: true }))
      .filter(entry => entry.isFile() && entry.name.endsWith(bundle.extension));
    if (matches.length !== 1) {
      throw new Error(`Expected exactly one ${bundle.extension} installer in ${directory}; found ${matches.length}`);
    }
    const file = path.join(directory, matches[0].name);
    if ((await stat(file)).size === 0) throw new Error(`Empty installer: ${file}`);
    inputs.push({ file, name: `MenuVex-Printer-Agent-${version}-${profile.platform}-${profile.architecture}${bundle.suffix}` });
  }
  const destination = path.join(root, 'dist', 'installers', target);
  await rm(destination, { recursive: true, force: true });
  await mkdir(destination, { recursive: true });
  const files = [];
  for (const input of inputs) {
    const output = path.join(destination, input.name);
    await copyFile(input.file, output);
    files.push({ filename: input.name, bytes: (await stat(output)).size, sha256: await sha256(output) });
  }
  await writeFile(path.join(destination, 'manifest.json'), JSON.stringify({
    version, platform: profile.platform, architecture: profile.architecture,
    channel: 'test-candidate', securityReviewRequired: true, files,
  }, null, 2) + '\n');
  await writeFile(path.join(destination, 'SHA256SUMS.txt'), files.map(f => `${f.sha256}  ${f.filename}`).join('\n') + '\n');
  await writeFile(path.join(destination, 'READ-ME-FIRST.txt'),
    'MenuVex Printer Agent — TEST CANDIDATE\n\n' +
    'These are compiled installers, not source code. No Node.js, Rust or npm is required on the customer machine.\n' +
    'Windows: run the setup.exe. macOS: open the DMG and move the app to Applications. Ubuntu/Debian: install the .deb through the system package manager.\n' +
    'Open Agent, configure your printer, and inspect a physical test print before use. USB driver/permission setup may still be required.\n' +
    'This build is for supervised testing. CI does not sign/notarize or certify hardware compatibility. Do not disable OS security protections.\n' +
    'The MenuVex website must be integrated and paired separately for automatic order printing.\n');
  return destination;
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const root = fileURLToPath(new URL('../', import.meta.url));
    console.log(await collectInstallers(root, process.argv[2]));
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
