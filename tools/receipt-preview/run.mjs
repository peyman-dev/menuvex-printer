#!/usr/bin/env node
// Runs the receipt design preview with whatever Python is installed, so `npm run preview:receipt`
// works on the owner's Linux/macOS/Windows machine without remembering the interpreter name.
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const candidates = process.platform === 'win32' ? [['py', ['-3']], ['python'], ['python3']] : [['python3'], ['python']];

for (const [command, prefix = []] of candidates) {
  const probe = spawnSync(command, [...prefix, '--version'], { stdio: 'ignore' });
  if (probe.status !== 0) continue;
  const result = spawnSync(command, [...prefix, join(here, 'preview.py'), ...process.argv.slice(2)], {
    stdio: 'inherit',
  });
  if (result.error) {
    console.error(`cannot run ${command}: ${result.error.message}`);
    process.exit(2);
  }
  process.exit(result.status ?? 1);
}

console.error('no Python interpreter found; install Python 3 and the preview requirements:');
console.error('  python3 -m pip install uharfbuzz freetype-py pillow');
process.exit(2);
