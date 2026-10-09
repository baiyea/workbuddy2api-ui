// Build the prepared target, then give its installer the public release name.
const fs = require('node:fs');
const path = require('node:path');
const { spawnSync } = require('node:child_process');

const root = path.resolve(__dirname, '..');
const config = JSON.parse(fs.readFileSync(path.join(root, 'tauri.conf.json'), 'utf8'));
const { target } = JSON.parse(fs.readFileSync(path.join(root, 'resources/runtime/runtime-manifest.json'), 'utf8'));
const targets = {
  'aarch64-apple-darwin': { host: 'darwin', system: 'macos', arch: 'arm64', bundles: 'app,dmg', folder: 'dmg', ext: '.dmg' },
  'x86_64-apple-darwin': { host: 'darwin', system: 'macos', arch: 'x64', bundles: 'app,dmg', folder: 'dmg', ext: '.dmg' },
  'x86_64-pc-windows-msvc': { host: 'win32', system: 'windows', arch: 'x64', bundles: 'nsis', folder: 'nsis', ext: '.exe' },
};
const selected = targets[target];
if (!selected || selected.host !== process.platform) {
  throw new Error(`请在对应系统上构建安装包：${target}`);
}
if (process.argv.length > 2) throw new Error('先通过 prepare.py --target 选择目标，再运行 npm run build（无需额外参数）。');
const built = spawnSync(process.execPath, [path.join(root, 'node_modules/@tauri-apps/cli/tauri.js'),
  'build', '--target', target, '--bundles', selected.bundles, '--', '--locked'], { cwd: root, stdio: 'inherit' });
if (built.error) throw built.error;
if (built.status !== 0) process.exit(built.status || 1);

const targetDir = path.resolve(root, process.env.CARGO_TARGET_DIR || 'target');
const directory = path.join(targetDir, target, 'release/bundle', selected.folder);
const sources = fs.readdirSync(directory, { withFileTypes: true }).filter(entry => entry.isFile()
  && entry.name.startsWith(`${config.productName}_${config.version}_`) && entry.name.endsWith(selected.ext));
if (sources.length !== 1) throw new Error(`预期一个新安装包，找到 ${sources.length} 个：${directory}`);
const destination = path.join(directory, `${config.productName}-${selected.system}-${selected.arch}-${config.version}${selected.ext}`);
fs.renameSync(path.join(directory, sources[0].name), destination);
console.log(`安装包：${destination}`);
