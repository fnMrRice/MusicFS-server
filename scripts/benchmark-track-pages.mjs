// 用同一个 SQLite 快照与内置后端测量真实 HTTP 分页。每次运行都创建独立目录。
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { createHash, randomBytes } from 'node:crypto';
import { constants } from 'node:fs';
import { copyFile, mkdir, readFile, writeFile } from 'node:fs/promises';
import { createServer } from 'node:net';
import { join, resolve } from 'node:path';
import { DatabaseSync } from 'node:sqlite';
import { setTimeout as delay } from 'node:timers/promises';

const [binary, snapshot, directory, suppliedCursor, mode = 'all'] = process.argv.slice(2);
if (!suppliedCursor || !['all', 'plain-only'].includes(mode)) {
  throw new Error('Usage: node benchmark-track-pages.mjs HOST SNAPSHOT NEW_RUN_DIR FAVORITES_CURSOR [plain-only]');
}
const run = resolve(directory);
const digest = value => createHash('sha256').update(value).digest('hex');
const median = values => [...values].sort((a, b) => a - b)[Math.floor(values.length / 2)];
const originPort = await new Promise((done, reject) => {
  const socket = createServer();
  socket.on('error', reject).listen(0, '127.0.0.1', () => {
    const port = socket.address().port;
    socket.close(() => done(port));
  });
});
const origin = 'http://127.0.0.1:' + originPort;
const source = new DatabaseSync(resolve(snapshot), { readOnly: true });
const cursor = source.prepare('SELECT owner,kind,position,revision,expires_at FROM personal_collection_cursors WHERE id=?').get(suppliedCursor);
assert.ok(cursor && cursor.owner === 'admin' && cursor.kind === 'favorites' && cursor.position % 100 === 0);
const tracks = source.prepare('SELECT count(*) AS count FROM music_tracks').get().count;
const schema = source.prepare('SELECT max(version) AS version FROM schema_migrations').get().version;
source.close();
await mkdir(run);
await mkdir(join(run, 'data'));
await copyFile(resolve(snapshot), join(run, 'data', 'musicfs.sqlite'), constants.COPYFILE_EXCL);
// 使用固定配置；显式的空插件列表关闭 Provider 自动发现。
const config = [
  'configVersion: 1', 'listen: "127.0.0.1:' + originPort + '"', 'allowLoopbackHttp: true',
  'allowedOrigins: ["' + origin + '"]', 'discovery: {enabled: false}',
  'dataRoot: "./data"', 'mediaRoot: "./media"', 'cacheRoot: "./cache"',
  'webRoot: ""', 'logging: {level: debug}', 'storage: {backend: sqlite}', 'plugins: []',
  'audioDSP: {enabled: false}', 'audioChannels: {enabled: false}', 'hashing: {auto: false}',
  'fingerprinting: {enabled: false}', 'writeBack: {enabled: false}', '',
].join('\n');
await writeFile(join(run, 'config.yaml'), config);
const token = randomBytes(32).toString('hex');
const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.toUpperCase().startsWith('MUSICFS_')));
const child = spawn(resolve(binary), ['serve', '--config', join(run, 'config.yaml')], {
  cwd: run, windowsHide: true, env: { ...env, MUSICFS_ADMIN_TOKEN: token }, stdio: ['ignore', 'ignore', 'pipe'],
});
let stderr = '';
child.stderr.on('data', b => { stderr = (stderr + b.toString()).slice(-12000); });
const closed = new Promise((done, reject) => { child.once('error', reject); child.once('close', done); });
let cookie;
const samples = [];
async function request(page, path, round) {
  const start = performance.now();
  const res = await fetch(origin + '/api/v1' + path, { headers: { Cookie: cookie }, signal: AbortSignal.timeout(60000) });
  const raw = await res.text();
  const elapsed = Math.round((performance.now() - start) * 10) / 10;
  const data = JSON.parse(raw);
  if (!res.ok) throw Object.assign(new Error('Request failed: ' + res.status + ' ' + (data.error?.code ?? data.code)), { status: res.status, code: data.error?.code ?? data.code });
  if (round >= 0) {
    const contents = { ...data };
    // 收藏游标是随机令牌；保留分页是否结束、其他所有字段及曲目顺序。
    if ('nextCursor' in contents) contents.nextCursor = contents.nextCursor !== null;
    const sample = { page, round, requestId: res.headers.get('x-request-id'), elapsed, bytes: Buffer.byteLength(raw),
      count: data.items.length, hash: digest(JSON.stringify(contents)), rawHash: digest(raw), more: !!(data.nextCursor || data.nextAfter) };
    samples.push(sample);
    console.log(page, round, elapsed + ' ms', sample.count + ' items');
  }
  return data;
}
try {
  let ready = false;
  for (let n = 0; n < 140; n++) {
    if (child.exitCode !== null) throw new Error('Server exited during startup: ' + stderr);
    try { ready = (await fetch(origin + '/api/v1/health/ready', { signal: AbortSignal.timeout(500) })).ok; } catch {}
    if (ready) break;
    await delay(150);
  }
  assert.ok(ready, 'Server did not become ready: ' + stderr);
  const login = await fetch(origin + '/api/v1/auth/session', { method: 'POST', headers: { Authorization: 'Bearer ' + token, Origin: origin } });
  assert.equal(login.status, 200, 'Isolated login failed');
  cookie = login.headers.getSetCookie().map(line => line.split(';')[0]).join('; ');
  assert.ok(cookie);
  let startCursor = suppliedCursor;
  let equivalent = false;
  const fav = c => '/me/favorites?limit=100' + (c ? '&cursor=' + encodeURIComponent(c) : '');
  async function favoritesPair(round) {
    let first;
    try { first = await request('favorites@' + cursor.position, fav(startCursor), round); }
    catch (err) {
      if (round !== 0 || err.code !== 'CURSOR_EXPIRED') throw err;
      equivalent = true;
      startCursor = null;
      for (let offset = 0; offset < cursor.position; offset += 100) {
        startCursor = (await request('', fav(startCursor), -1)).nextCursor;
        assert.ok(startCursor, 'Snapshot does not cover supplied cursor position');
      }
      first = await request('favorites@' + cursor.position, fav(startCursor), round);
    }
    assert.equal(first.revision, cursor.revision, 'Favorites revision changed');
    assert.ok(first.nextCursor);
    await request('favorites@' + (cursor.position + 100), fav(first.nextCursor), round);
  }
  async function tracksPair(prefix, tracksPath, round) {
    const trackPage = await request(prefix + '@0', tracksPath, round);
    assert.ok(trackPage.nextAfter);
    await request(prefix + '@100', tracksPath + '&after=' + encodeURIComponent(trackPage.nextAfter), round);
  }
  const tracksPath = '/library/tracks?limit=100';
  for (let round = 0; round <= 5; round++) {
    if (mode === 'all') {
      await favoritesPair(round);
      await tracksPair('tracks', tracksPath + '&sourceOnly=true', round);
    }
    await tracksPair('tracksPlain', tracksPath, round);
  }
  const logs = (await readFile(join(run, 'data', 'logs', 'server.log'), 'utf8')).split(/\r?\n/).filter(Boolean).map(line => JSON.parse(line));
  const completed = new Map(logs.filter(line => line.msg === 'HTTP request completed').map(line => [line.requestId, line.durationMs]));
  for (const sample of samples) {
    sample.serverMs = completed.get(sample.requestId);
    assert.ok(Number.isFinite(sample.serverMs));
  }
  const summary = {};
  for (const page of new Set(samples.map(s => s.page))) {
    const [first, ...warm] = samples.filter(s => s.page === page);
    assert.ok(warm.every(s => s.hash === first.hash), 'Unstable page: ' + page);
    summary[page] = { count: first.count, hash: first.hash, firstMs: first.elapsed, firstServerMs: first.serverMs,
      medianMs: median(warm.map(s => s.elapsed)), medianServerMs: median(warm.map(s => s.serverMs)) };
  }
  const pairs = {};
  for (const name of new Set(samples.map(s => s.page.split('@')[0]))) {
    const values = [];
    for (let round = 0; round <= 5; round++) {
      const [a, b] = samples.filter(s => s.page.startsWith(name + '@') && s.round === round);
      values.push({ elapsed: Math.round((a.elapsed + b.elapsed) * 10) / 10, serverMs: a.serverMs + b.serverMs,
        hash: digest(a.hash + b.hash) });
    }
    pairs[name] = { firstMs: values[0].elapsed, firstServerMs: values[0].serverMs, medianMs: median(values.slice(1).map(v => v.elapsed)),
      medianServerMs: median(values.slice(1).map(v => v.serverMs)), rounds: values };
  }
  const store = new DatabaseSync(join(run, 'data', 'musicfs.sqlite'), { readOnly: true });
  const runSchema = store.prepare('SELECT max(version) AS version FROM schema_migrations').get().version;
  store.close();
  const result = { snapshot: resolve(snapshot), host: resolve(binary), hostHash: digest(await readFile(binary)), schema, runSchema, tracks,
    equivalent, position: cursor.position, favoritesRevision: cursor.revision, summary, pairs, samples };
  await writeFile(join(run, 'result.json'), JSON.stringify(result, null, 2));
  console.log(JSON.stringify(summary, null, 2));
} finally {
  if (child.exitCode === null) child.kill();
  await closed;
}
