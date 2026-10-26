import { readdir, readFile, writeFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { icons } from '@iconify-json/ri';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const names = new Set();
async function scan(dir) {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = resolve(dir, entry.name);
    if (entry.isDirectory()) await scan(path);
    else if (/\.(vue|ts|jsx|js|json)$/.test(entry.name) && entry.name !== 'icons.json') {
      const source = await readFile(path, 'utf8');
      for (const match of source.matchAll(/ri:([a-z0-9-]+)/g)) names.add(match[1]);
    }
  }
}
await scan(resolve(root, 'src'));
const selected = Object.fromEntries([...names].sort().map((name) => {
  if (!icons.icons[name]) throw new Error(`Unknown Remix icon: ${name}`);
  return [name, icons.icons[name]];
}));
await writeFile(resolve(root, 'src/plugins/icons.json'), JSON.stringify({ prefix: icons.prefix, width: icons.width, height: icons.height, icons: selected }) + '\n');
console.log(`Bundled ${names.size} local framework icons.`);
