'use strict';

const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const { spawnSync } = require('node:child_process');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const action = require('./index.js');

process.env.GITHUB_REPOSITORY = 'owner/project';

function jsonResponse(value, status = 200) {
  return {
    ok: status < 400,
    status,
    text: async () => JSON.stringify(value),
  };
}

function binaryResponse(value, status = 200) {
  const bytes = Buffer.from(value);
  return {
    ok: status < 400,
    status,
    arrayBuffer: async () => bytes,
  };
}

function apiEvent() {
  return {
    pull_request: {
      title: 'feat: edited title',
      base: { sha: 'base-sha', repo: { full_name: 'owner/project' } },
      head: { sha: 'head-sha', repo: { full_name: 'owner/project' } },
    },
  };
}

test('compare pagination checks every page beyond the 250 commit API window', async () => {
  const calls = [];
  const items = Array.from({ length: 260 }, (_, i) => ({ sha: `sha-${i}`, commit: { message: `feat: item ${i}` } }));
  const fetchImpl = async url => {
    calls.push(url);
    const page = Number(new URL(url).searchParams.get('page'));
    return jsonResponse({ total_commits: 260, commits: items.slice((page - 1) * 100, page * 100) });
  };
  const result = await action.compareCommits({
    fetchImpl, apiUrl: 'https://api.github.test', token: 'secret', repo: 'owner/project', before: 'base', head: 'head',
  });
  assert.equal(result.length, 260);
  assert.equal(calls.length, 3);
  assert.match(calls[0], /compare\/base\.\.\.head\?per_page=100&page=1$/);
});

test('compare qualifies refs for a pull request head in a fork network', async () => {
  let requested;
  const result = await action.compareCommits({
    fetchImpl: async url => {
      requested = url;
      return jsonResponse({ total_commits: 1, commits: [{ sha: 'head', commit: { message: 'feat: fork' } }] });
    },
    apiUrl: 'https://api.github.test', token: 'secret', repo: 'owner/project', headRepo: 'contributor/project', before: 'base', head: 'head',
  });
  assert.equal(result.length, 1);
  assert.match(requested, /compare\/owner:base\.\.\.contributor:head\?per_page=100&page=1$/);
});

test('title mode uses current pull request metadata, including edited titles', async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'commit-guard-test-'));
  const binary = path.join(root, 'checker');
  fs.writeFileSync(binary, 'fixture');
  const calls = [];
  let checkerInput;
  try {
    const result = await action.run({
      eventName: 'pull_request', event: apiEvent(), mode: 'title', binary, apiUrl: 'https://api.github.test', configFile: '',
      fetchImpl: async url => { calls.push(url); throw new Error(`unexpected API call: ${url}`); },
      spawnImpl: (_file, args, options) => {
        assert.deepEqual(args.slice(0, 3), ['check', '--batch', '--config']);
        checkerInput = JSON.parse(options.input);
        return { status: 0, stdout: JSON.stringify({ valid: true, results: [{ sha: 'head-sha', errors: [] }] }), stderr: '' };
      },
    });
    assert.equal(result, 0);
    assert.deepEqual(checkerInput, [{ sha: 'head-sha', message: 'feat: edited title' }]);
    assert.equal(calls.length, 0);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test('title mode falls back to commit ranges on pushes', async () => {
  let checked;
  const result = await action.run({
    eventName: 'push', event: { before: 'base', after: 'head' }, mode: 'title', apiUrl: 'https://api.github.test', configFile: '',
    fetchImpl: async url => jsonResponse({ total_commits: 1, commits: [{ sha: 'head', commit: { message: 'feat: pushed' } }] }),
    binary: __filename,
    spawnImpl: (_file, _args, options) => {
      checked = JSON.parse(options.input);
      return { status: 0, stdout: JSON.stringify({ valid: true, results: [{ sha: 'head', errors: [] }] }), stderr: '' };
    },
  });
  assert.equal(result, 0);
  assert.deepEqual(checked, [{ sha: 'head', message: 'feat: pushed' }]);
});

test('initial pushes list complete ancestry, while deleted refs produce an explicit empty result', async () => {
  const pages = [];
  const fetchImpl = async url => {
    pages.push(url);
    const page = Number(new URL(url).searchParams.get('page'));
    const values = page === 1 ? Array.from({ length: 100 }, (_, i) => ({ sha: `sha-${i}`, commit: { message: 'feat: first' } })) : [{ sha: 'sha-100', commit: { message: 'feat: last' } }];
    return jsonResponse(values);
  };
  const initial = await action.listInitialCommits({ fetchImpl, apiUrl: 'https://api.github.test', token: '', repo: 'owner/project', head: 'head' });
  assert.equal(initial.length, 101);
  assert.equal(pages.length, 2);
  assert.deepEqual(action.inputCommits({ before: '0'.repeat(40), after: 'head' }, 'push'), { kind: 'initial', repo: 'owner/project', configRepo: 'owner/project', head: 'head' });
  assert.deepEqual(action.inputCommits({ before: 'base', after: '0'.repeat(40) }, 'push'), { kind: 'none', repo: 'owner/project', configRepo: 'owner/project', head: '0000000000000000000000000000000000000000' });
});

test('deleted push performs no network work when there are no commits', async () => {
  let called = false;
  const result = await action.run({
    eventName: 'push', event: { before: 'base', after: '0'.repeat(40) }, apiUrl: 'https://api.github.test',
    fetchImpl: async () => { called = true; throw new Error('network should not be used'); },
  });
  assert.equal(result, 0);
  assert.equal(called, false);
});

test('pull request input reports a missing base and preserves fork repositories', () => {
  assert.throws(() => action.inputCommits({ pull_request: { head: { sha: 'head' } } }, 'pull_request'), /missing base\.sha or head\.sha/);
  const selected = action.inputCommits({
    pull_request: {
      base: { sha: 'base', repo: { full_name: 'owner/project' } },
      head: { sha: 'fork-head', repo: { full_name: 'contributor/project' } },
    },
  }, 'pull_request');
  assert.equal(selected.repo, 'owner/project');
  assert.equal(selected.configRepo, 'contributor/project');
  assert.equal(selected.head, 'fork-head');
});

test('merge group reads immutable SHAs from the nested event payload', () => {
  const selected = action.inputCommits({ merge_group: { base_sha: 'base', head_sha: 'head' } }, 'merge_group');
  assert.deepEqual(selected, { kind: 'compare', repo: 'owner/project', configRepo: 'owner/project', before: 'base', head: 'head' });
});

test('contents config is decoded as strict JSON and 404 means default policy', async () => {
  const config = { types: ['feat', 'fix'], maxHeaderLength: 120 };
  const encoded = Buffer.from(JSON.stringify(config)).toString('base64');
  const loaded = await action.loadConfig({
    fetchImpl: async () => jsonResponse({ encoding: 'base64', content: encoded }),
    apiUrl: 'https://api.github.test', token: 'token', repo: 'owner/project', head: 'head', file: '.commit-guard.json',
  });
  assert.deepEqual(loaded, config);
  const absent = await action.loadConfig({
    fetchImpl: async () => jsonResponse({ message: 'Not Found' }, 404),
    apiUrl: 'https://api.github.test', token: 'token', repo: 'owner/project', head: 'head', file: '.commit-guard.json',
  });
  assert.deepEqual(absent, {});
  await assert.rejects(() => action.loadConfig({
    fetchImpl: async () => jsonResponse({ encoding: 'base64', content: Buffer.from('{bad').toString('base64') }),
    apiUrl: 'https://api.github.test', token: '', repo: 'owner/project', head: 'head', file: '.commit-guard.json',
  }), /JSON/);
  await assert.rejects(() => action.loadConfig({
    fetchImpl: async () => jsonResponse({ encoding: 'base64', content: encoded, truncated: true }),
    apiUrl: 'https://api.github.test', token: '', repo: 'owner/project', head: 'head', file: '.commit-guard.json',
  }), /truncated/);
  await assert.rejects(() => action.loadConfig({
    fetchImpl: async () => jsonResponse({ encoding: 'base64', content: '%%%=' }),
    apiUrl: 'https://api.github.test', token: '', repo: 'owner/project', head: 'head', file: '.commit-guard.json',
  }), /base64/);
});

test('error annotations escape GitHub command delimiters', () => {
  const originalWrite = process.stdout.write;
  let output = '';
  process.stdout.write = value => { output += value; return true; };
  try {
    action.annotate('sha%\r\n: bad%\r\nmessage');
    assert.match(output, /sha%25%0D%0A: bad%25%0D%0Amessage/);
  } finally {
    process.stdout.write = originalWrite;
  }
});

test('release checksum mismatch fails before writing an executable', async () => {
  const target = action.platformTarget();
  const asset = `commit-guard_0.3.0_${target.platform}_${target.architecture}${target.extension}`;
  const calls = [];
  await assert.rejects(() => action.downloadChecker({
    version: '0.3.0', tempRoot: os.tmpdir(),
    fetchImpl: async url => {
      calls.push(url);
      return url.endsWith('SHA256SUMS') ? binaryResponse(`${'0'.repeat(64)}  ${asset}\n`) : binaryResponse('checker');
    },
  }), /Checksum mismatch/);
  assert.equal(calls.length, 2);
  assert.equal(crypto.createHash('sha256').update('checker').digest('hex') === '0'.repeat(64), false);
});

test('checksum manifests reject duplicate asset names and release requests carry no token', async () => {
  const target = action.platformTarget();
  const asset = `commit-guard_0.3.0_${target.platform}_${target.architecture}${target.extension}`;
  const bytes = Buffer.from('checker');
  const digest = crypto.createHash('sha256').update(bytes).digest('hex');
  let releaseOptions;
  await assert.rejects(() => action.downloadChecker({
    version: '0.3.0', fetchImpl: async (url, options) => {
      releaseOptions = options;
      return url.endsWith('SHA256SUMS') ? binaryResponse(`${digest}  ${asset}\n${digest} *${asset}\n`) : binaryResponse(bytes);
    },
  }), /duplicate/);
  assert.equal(releaseOptions.headers.Authorization, undefined);
  assert.equal(releaseOptions.redirect, 'follow');
});

test('API request timeout aborts a hanging response within the configured bound', async () => {
  let signal;
  const pending = action.request((_url, options) => Promise.resolve({ ok: true, status: 200, text: () => new Promise((_resolve, reject) => {
    signal = options.signal;
    signal.addEventListener('abort', () => reject(new Error('aborted')));
  }) }), 'https://api.github.test/slow', { timeoutMs: 5, maxRetries: 0, parse: response => response.text() });
  await assert.rejects(pending, /Network request failed: aborted/);
  assert.equal(signal.aborted, true);
});

test('malicious commit errors stay in escaped annotations and do not become summary Markdown', async () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'commit-guard-summary-'));
  const binary = path.join(root, 'checker');
  const summary = path.join(root, 'summary.md');
  fs.writeFileSync(binary, 'fixture');
  const originalWrite = process.stdout.write;
  let output = '';
  process.stdout.write = value => { output += value; return true; };
  const previousSummary = process.env.GITHUB_STEP_SUMMARY;
  process.env.GITHUB_STEP_SUMMARY = summary;
  try {
    const status = await action.run({
      eventName: 'pull_request', event: { pull_request: { title: 'feat: [untrusted](https://evil)', base: { sha: 'base', repo: { full_name: 'owner/project' } }, head: { sha: 'head', repo: { full_name: 'owner/project' } } } },
      mode: 'title', binary, apiUrl: 'https://api.github.test', configFile: '',
      fetchImpl: async () => { throw new Error('network should not be used'); },
      spawnImpl: () => ({ status: 1, stdout: JSON.stringify({ valid: false, results: [{ sha: 'head', errors: ['bad [link](https://evil)'] }] }), stderr: '' }),
    });
    assert.equal(status, 1);
    assert.match(output, /bad \[link\]\(https:\/\/evil\)/);
    assert.doesNotMatch(fs.readFileSync(summary, 'utf8'), /evil/);
  } finally {
    process.stdout.write = originalWrite;
    if (previousSummary === undefined) delete process.env.GITHUB_STEP_SUMMARY;
    else process.env.GITHUB_STEP_SUMMARY = previousSummary;
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test('workflow dispatch requires explicit from and to SHAs', () => {
  assert.throws(() => action.inputCommits({}, 'workflow_dispatch', { from: '', to: '' }), /requires both from and to/);
  assert.deepEqual(action.inputCommits({ before: 'ignored', after: 'ignored' }, 'workflow_dispatch', { from: 'base', to: 'head' }), { kind: 'compare', repo: 'owner/project', configRepo: 'owner/project', before: 'base', head: 'head' });
});

test('compiled checker batch protocol matches the action contract', { skip: !process.env.CG_TEST_BINARY }, () => {
  const binary = path.resolve(process.env.CG_TEST_BINARY);
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'commit-guard-real-'));
  const config = path.join(root, 'config.json');
  const runBatch = (messages, configText = '{}') => {
    fs.writeFileSync(config, configText);
    return spawnSync(binary, ['check', '--batch', '--config', config, '--json'], { input: JSON.stringify(messages), encoding: 'utf8', windowsHide: true });
  };
  try {
    const valid = runBatch([{ sha: 'a', message: 'fEaT(scope)!: punctuation, works!' }]);
    assert.equal(valid.status, 0);
    assert.deepEqual(JSON.parse(valid.stdout), { valid: true, results: [{ sha: 'a', errors: [] }] });

    const long = runBatch([{ sha: 'b', message: `feat: ${'x'.repeat(40)}` }], '{"maxHeaderLength":20}');
    assert.equal(long.status, 1);
    assert.match(JSON.parse(long.stdout).results[0].errors.join(' '), /maximum length/);

    const malformed = runBatch([{ sha: 'c', message: 'feat: okay' }], '{"unknown":true}');
    assert.equal(malformed.status, 2);
    assert.match(malformed.stderr, /unknown field|unknown/i);

    const empty = runBatch([]);
    assert.equal(empty.status, 0);
    assert.deepEqual(JSON.parse(empty.stdout), { valid: true, results: [] });

    const missing = spawnSync(binary, ['check', '--batch', '--config', config, '--json'], { input: '', encoding: 'utf8', windowsHide: true });
    assert.equal(missing.status, 2);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});
