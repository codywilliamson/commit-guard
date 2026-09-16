'use strict';

const crypto = require('node:crypto');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');

const PER_PAGE = 100;
const MAX_PAGES = 10000;
const REQUEST_TIMEOUT_MS = 15000;
const MAX_RETRIES = 2;
const MAX_DOWNLOAD_BYTES = 32 * 1024 * 1024;
const ZERO_SHA = /^0+$/;

class ActionError extends Error {
  constructor(message, code = 2) {
    super(message);
    this.name = 'ActionError';
    this.code = code;
  }
}

class GitHubApiError extends ActionError {
  constructor(status, message, url) {
    super(`GitHub API request failed (${status})${message ? `: ${message}` : ''}`, 2);
    this.status = status;
    this.url = url;
  }
}

function envInput(name, fallback = '') {
  // The Actions runner uppercases input ids and replaces spaces with `_`.
  // Hyphens remain part of the environment key (for example INPUT_CONFIG-FILE).
  const value = process.env[`INPUT_${name.toUpperCase().replace(/ /g, '_')}`];
  return value === undefined ? fallback : value.trim();
}

function configuredApiUrl() {
  const value = (process.env.GITHUB_API_URL || '').trim().replace(/\/+$/, '');
  if (!value) throw new ActionError('GITHUB_API_URL is not configured; refusing to send a token to an unknown host');
  return value;
}

function repositoryName(value = process.env.GITHUB_REPOSITORY) {
  const match = String(value || '').trim().match(/^([^/]+)\/([^/]+)$/);
  if (!match) throw new ActionError('GITHUB_REPOSITORY must be in owner/repository form');
  return `${match[1]}/${match[2]}`;
}

function eventPayload() {
  const eventPath = process.env.GITHUB_EVENT_PATH;
  if (!eventPath) throw new ActionError('GITHUB_EVENT_PATH is required');
  try {
    return JSON.parse(fs.readFileSync(eventPath, 'utf8'));
  } catch (error) {
    throw new ActionError(`Could not read GITHUB_EVENT_PATH: ${error.message}`);
  }
}

function eventName() {
  return (process.env.GITHUB_EVENT_NAME || '').trim();
}

function apiHeaders(token) {
  const headers = {
    Accept: 'application/vnd.github+json',
    'X-GitHub-Api-Version': '2022-11-28',
    'User-Agent': 'commit-guard-action',
  };
  if (token) headers.Authorization = `Bearer ${token}`;
  return headers;
}

async function responseBody(response) {
  if (typeof response.text === 'function') {
    const text = await response.text();
    if (!text) return {};
    try { return JSON.parse(text); } catch { return { message: text }; }
  }
  if (typeof response.json === 'function') return response.json();
  return response.body || {};
}

function retryableStatus(status) {
  return status === 408 || status === 425 || status === 429 || status >= 500;
}

async function request(fetchImpl, url, options = {}) {
  const timeoutMs = Number.isFinite(options.timeoutMs) ? options.timeoutMs : REQUEST_TIMEOUT_MS;
  const maxRetries = Number.isInteger(options.maxRetries) ? options.maxRetries : MAX_RETRIES;
  const { timeoutMs: _timeoutMs, maxRetries: _maxRetries, parse, ...fetchOptions } = options;
  let lastError;
  for (let attempt = 0; attempt <= maxRetries; attempt += 1) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    try {
      const response = await fetchImpl(url, { ...fetchOptions, signal: controller.signal });
      if (response.status >= 300 && response.status < 400 && fetchOptions.redirect !== 'follow') {
        throw new ActionError(`GitHub API request redirected unexpectedly (${response.status})`);
      }
      if (response.ok === false || (response.status !== undefined && response.status >= 400)) {
        const body = await responseBody(response);
        const message = body && typeof body === 'object' ? body.message : String(body || '');
        if (retryableStatus(response.status) && attempt < maxRetries) {
          await new Promise(resolve => setTimeout(resolve, 250 * 2 ** attempt));
          continue;
        }
        throw new GitHubApiError(response.status || 0, message, url);
      }
      return parse ? await parse(response) : response;
    } catch (error) {
      lastError = error;
      if (error instanceof ActionError) throw error;
      if (attempt < maxRetries) {
        await new Promise(resolve => setTimeout(resolve, 250 * 2 ** attempt));
      }
    } finally {
      clearTimeout(timer);
    }
  }
  throw new ActionError(`Network request failed: ${lastError ? lastError.message : 'unknown error'}`);
}

async function fetchJson(fetchImpl, url, token) {
  return request(fetchImpl, url, { headers: apiHeaders(token), redirect: 'manual', parse: responseBody });
}

function apiPath(apiUrl, route) {
  return `${apiUrl}/${route.replace(/^\/+/, '')}`;
}

function encodedRepo(repo) {
  return repo.split('/').map(encodeURIComponent).join('/');
}

function pageUrl(url, page) {
  return `${url}${url.includes('?') ? '&' : '?'}per_page=${PER_PAGE}&page=${page}`;
}

function commitFromApi(item) {
  const sha = item && typeof item.sha === 'string' ? item.sha : '';
  const message = item && item.commit && typeof item.commit.message === 'string' ? item.commit.message : null;
  if (!sha || message === null) throw new ActionError('GitHub returned a commit without sha or message');
  return { sha, message };
}

async function compareCommits({ fetchImpl, apiUrl, token, repo, before, head, headRepo = repo }) {
  // The compare endpoint needs owner-qualified refs when the head is in a
  // fork network. Keep the `...` separator and ref colons readable in the URL.
  const crossRepository = headRepo !== repo;
  const baseRef = crossRepository ? `${repo.split('/')[0]}:${before}` : before;
  const headRef = crossRepository ? `${headRepo.split('/')[0]}:${head}` : head;
  const encodedRef = ref => {
    const colon = ref.indexOf(':');
    return colon < 0 ? encodeURIComponent(ref) : `${encodeURIComponent(ref.slice(0, colon))}:${encodeURIComponent(ref.slice(colon + 1))}`;
  };
  const url = apiPath(apiUrl, `repos/${encodedRepo(repo)}/compare/${encodedRef(baseRef)}...${encodedRef(headRef)}`);
  const commits = [];
  const seen = new Set();
  let expected;
  for (let page = 1; page <= MAX_PAGES; page += 1) {
    const payload = await fetchJson(fetchImpl, pageUrl(url, page), token);
    if (!Array.isArray(payload.commits)) throw new ActionError('GitHub compare response did not contain commits');
    if (!Number.isInteger(payload.total_commits) || payload.total_commits < 0) {
      throw new ActionError('GitHub compare response did not contain a valid total_commits count');
    }
    if (expected === undefined) {
      expected = payload.total_commits;
      if (expected > MAX_PAGES * PER_PAGE) throw new ActionError('GitHub compare range exceeded the pagination safety limit');
    } else if (payload.total_commits !== expected) {
      throw new ActionError('GitHub compare response changed total_commits while being paginated');
    }
    let newCount = 0;
    for (const item of payload.commits) {
      const commit = commitFromApi(item);
      if (!seen.has(commit.sha)) { seen.add(commit.sha); commits.push(commit); newCount += 1; }
    }
    if (commits.length > expected) throw new ActionError(`GitHub compare returned more commits than its total_commits count (${expected})`);
    if (commits.length === expected) return commits;
    if (newCount === 0 && payload.commits.length === PER_PAGE) {
      throw new ActionError(`GitHub compare pagination made no progress at page ${page}`);
    }
    if (payload.commits.length === 0 || payload.commits.length < PER_PAGE) {
      throw new ActionError(`GitHub compare range is incomplete: expected ${expected} commits but received ${commits.length}`);
    }
  }
  throw new ActionError('GitHub compare range exceeded the pagination safety limit');
}

async function listInitialCommits({ fetchImpl, apiUrl, token, repo, head }) {
  const url = apiPath(apiUrl, `repos/${encodedRepo(repo)}/commits?sha=${encodeURIComponent(head)}`);
  const commits = [];
  const seen = new Set();
  for (let page = 1; page <= MAX_PAGES; page += 1) {
    const payload = await fetchJson(fetchImpl, pageUrl(url, page), token);
    if (!Array.isArray(payload)) throw new ActionError('GitHub commits response was not an array');
    let newCount = 0;
    for (const item of payload) {
      const commit = commitFromApi(item);
      if (!seen.has(commit.sha)) { seen.add(commit.sha); commits.push(commit); newCount += 1; }
    }
    if (payload.length === PER_PAGE && newCount === 0 && page > 1) {
      throw new ActionError(`GitHub commits pagination made no progress at page ${page}`);
    }
    if (payload.length < PER_PAGE) {
      if (commits.length === 0) throw new ActionError('GitHub returned no commits for the initial push');
      return commits;
    }
  }
  throw new ActionError('Initial push history exceeded the pagination safety limit');
}

function validateConfigPath(file) {
  if (!file) return null;
  if (file.startsWith('/') || file.startsWith('\\') || file.split(/[\\/]/).some(part => part === '..')) {
    throw new ActionError('config-file must be a repository-relative path');
  }
  return file.split(/[\\/]/).map(encodeURIComponent).join('/');
}

async function loadConfig({ fetchImpl, apiUrl, token, repo, head, file }) {
  const encoded = validateConfigPath(file);
  if (!encoded) return {};
  const url = apiPath(apiUrl, `repos/${encodedRepo(repo)}/contents/${encoded}?ref=${encodeURIComponent(head)}`);
  try {
    const payload = await fetchJson(fetchImpl, url, token);
    if (!payload || payload.encoding !== 'base64' || typeof payload.content !== 'string') {
      throw new ActionError('Repository config response did not contain base64 content');
    }
    if (payload.truncated === true) throw new ActionError('Repository config response was truncated');
    if (payload.sha !== undefined && !/^[a-f0-9]{40}$/i.test(String(payload.sha))) {
      throw new ActionError('Repository config response contained an invalid file SHA');
    }
    const encodedContent = payload.content.replace(/\s/g, '');
    if (encodedContent.length % 4 !== 0 || !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(encodedContent)) {
      throw new ActionError('Repository config response contained invalid base64 content');
    }
    const text = Buffer.from(encodedContent, 'base64').toString('utf8');
    const config = JSON.parse(text);
    if (!config || Array.isArray(config) || typeof config !== 'object') {
      throw new ActionError('Repository config must be a JSON object');
    }
    return config;
  } catch (error) {
    if (error instanceof GitHubApiError && error.status === 404) return {};
    throw error;
  }
}

function releaseVersion() {
  const file = path.resolve(__dirname, '..', 'VERSION');
  let value;
  try { value = fs.readFileSync(file, 'utf8').trim(); } catch (error) {
    throw new ActionError(`Could not read release VERSION: ${error.message}`);
  }
  if (!/^\d+\.\d+\.\d+$/.test(value)) throw new ActionError(`Invalid release VERSION: ${value}`);
  return value;
}

function platformTarget() {
  const platforms = { linux: 'linux', darwin: 'darwin', win32: 'windows' };
  const architectures = { x64: 'amd64', arm64: 'arm64' };
  const platform = platforms[process.platform];
  const architecture = architectures[process.arch];
  if (!platform || !architecture) throw new ActionError(`Unsupported runner platform: ${process.platform}/${process.arch}`);
  return { platform, architecture, extension: process.platform === 'win32' ? '.exe' : '' };
}

async function responseBytes(response) {
  let bytes;
  if (typeof response.arrayBuffer === 'function') bytes = Buffer.from(await response.arrayBuffer());
  else if (Buffer.isBuffer(response.body)) bytes = response.body;
  else if (typeof response.body === 'string') bytes = Buffer.from(response.body);
  else throw new ActionError('Release download response did not contain binary data');
  if (bytes.length > MAX_DOWNLOAD_BYTES) throw new ActionError(`Release download exceeded ${MAX_DOWNLOAD_BYTES} bytes`);
  return bytes;
}

function checksumFor(text, asset) {
  let found;
  for (const line of String(text).split(/\r?\n/)) {
    const match = line.match(/^([a-fA-F0-9]{64})\s+(?:\*)?(.+?)\s*$/);
    if (match && match[2] === asset) {
      if (found) throw new ActionError(`SHA256SUMS contained duplicate entries for ${asset}`);
      found = match[1].toLowerCase();
    }
  }
  if (found) return found;
  throw new ActionError(`SHA256SUMS did not contain an entry for ${asset}`);
}

async function downloadChecker({ fetchImpl, version = releaseVersion(), tempRoot = os.tmpdir() }) {
  const target = platformTarget();
  const asset = `commit-guard_${version}_${target.platform}_${target.architecture}${target.extension}`;
  const base = `https://github.com/codywilliamson/commit-guard/releases/download/v${version}`;
  const [binary, manifest] = await Promise.all([
    request(fetchImpl, `${base}/${asset}`, { headers: { 'User-Agent': 'commit-guard-action' }, redirect: 'follow', parse: responseBytes }),
    request(fetchImpl, `${base}/SHA256SUMS`, { headers: { 'User-Agent': 'commit-guard-action' }, redirect: 'follow', parse: responseBytes }),
  ]);
  const sums = manifest.toString('utf8');
  const expected = checksumFor(sums, asset);
  const actual = crypto.createHash('sha256').update(binary).digest('hex');
  if (actual !== expected) throw new ActionError(`Checksum mismatch for ${asset}`);
  const directory = fs.mkdtempSync(path.join(tempRoot, 'commit-guard-'));
  const destination = path.join(directory, asset);
  fs.writeFileSync(destination, binary, { mode: 0o700 });
  if (process.platform !== 'win32') fs.chmodSync(destination, 0o700);
  return { path: destination, cleanup: () => fs.rmSync(directory, { recursive: true, force: true }) };
}

function inputCommits(event, name, inputs = {}) {
  const repository = repositoryName();
  if (name === 'pull_request') {
    const pr = event.pull_request;
    if (!pr || !pr.base || !pr.head || !pr.base.sha || !pr.head.sha) throw new ActionError('pull_request event is missing base.sha or head.sha');
    return {
      kind: 'compare', repo: pr.base.repo && pr.base.repo.full_name ? repositoryName(pr.base.repo.full_name) : repository,
      configRepo: pr.head.repo && pr.head.repo.full_name ? repositoryName(pr.head.repo.full_name) : repository,
      headRepo: pr.head.repo && pr.head.repo.full_name ? repositoryName(pr.head.repo.full_name) : repository,
      before: pr.base.sha, head: pr.head.sha,
    };
  }
  if (name === 'push') {
    const before = event.before;
    const head = event.after;
    if (!before || !head) throw new ActionError('push event is missing before or after SHA');
    if (ZERO_SHA.test(head)) return { kind: 'none', repo: repository, configRepo: repository, head };
    if (ZERO_SHA.test(before)) return { kind: 'initial', repo: repository, configRepo: repository, head };
    return { kind: 'compare', repo: repository, configRepo: repository, before, head };
  }
  if (name === 'merge_group') {
    const group = event.merge_group || event;
    const before = group.base_sha;
    const head = group.head_sha || process.env.GITHUB_SHA;
    if (!before || !head) throw new ActionError('merge_group event is missing base_sha or head_sha');
    if (ZERO_SHA.test(head)) return { kind: 'none', repo: repository, configRepo: repository, head };
    return { kind: 'compare', repo: repository, configRepo: repository, before, head };
  }
  if (name === 'workflow_dispatch') {
    const before = inputs.from;
    const head = inputs.to;
    if (!before || !head) throw new ActionError('workflow_dispatch requires both from and to inputs');
    if (ZERO_SHA.test(head)) return { kind: 'none', repo: repository, configRepo: repository, head };
    if (ZERO_SHA.test(before)) return { kind: 'initial', repo: repository, configRepo: repository, head };
    return { kind: 'compare', repo: repository, configRepo: repository, before, head };
  }
  throw new ActionError(`Unsupported GitHub event: ${name || '(missing GITHUB_EVENT_NAME)'}`);
}

function escapedCommand(value) {
  return String(value).replace(/%/g, '%25').replace(/\r/g, '%0D').replace(/\n/g, '%0A');
}

function annotate(message, title = 'commit-guard') {
  process.stdout.write(`::error title=${escapedCommand(title)}::${escapedCommand(message)}\n`);
}

function writeSummary(text) {
  const summary = process.env.GITHUB_STEP_SUMMARY;
  if (!summary) return;
  try { fs.appendFileSync(summary, `${text}\n`); } catch (error) { annotate(`Could not write GITHUB_STEP_SUMMARY: ${error.message}`, 'commit-guard runtime'); }
}

function summarize(result, count) {
  const errors = result.results.reduce((total, item) => total + (Array.isArray(item.errors) ? item.errors.length : 0), 0);
  if (result.valid) {
    const text = `commit-guard: valid (${count} commit${count === 1 ? '' : 's'} checked)`;
    process.stdout.write(`${text}\n`);
    writeSummary(`## commit-guard\n\n${text}`);
  } else {
    const text = `commit-guard: invalid (${errors} error${errors === 1 ? '' : 's'} across ${count} commit${count === 1 ? '' : 's'})`;
    process.stdout.write(`${text}\n`);
    writeSummary(`## commit-guard\n\n${text}`);
    for (const item of result.results) {
      if (!Array.isArray(item.errors)) continue;
      for (const error of item.errors) annotate(`${item.sha}: ${error}`, 'commit-guard');
    }
  }
}

function runChecker(binary, configPath, commits, spawnImpl = spawnSync) {
  const input = JSON.stringify(commits);
  const child = spawnImpl(binary, ['check', '--batch', '--config', configPath, '--json'], {
    input, encoding: 'utf8', windowsHide: true,
  });
  if (child.error) throw new ActionError(`Could not run checker: ${child.error.message}`);
  if (child.status === 0 || child.status === 1) {
    let result;
    try { result = JSON.parse(child.stdout || ''); } catch (error) {
      throw new ActionError(`Checker returned invalid JSON: ${error.message}`);
    }
    if (!result || typeof result.valid !== 'boolean' || !Array.isArray(result.results)) {
      throw new ActionError('Checker returned an invalid batch result');
    }
    return { result, status: child.status };
  }
  const detail = child.stderr ? String(child.stderr).trim() : `exit code ${child.status}`;
  throw new ActionError(`Checker failed at runtime: ${detail}`);
}

async function run(options = {}) {
  const fetchImpl = options.fetchImpl || globalThis.fetch;
  if (typeof fetchImpl !== 'function') throw new ActionError('Node fetch is unavailable');
  const inputs = {
    mode: (options.mode || envInput('mode', 'commits')).toLowerCase(),
    token: options.token === undefined ? envInput('token') : options.token,
    configFile: options.configFile === undefined ? '.commit-guard.json' : options.configFile,
    binary: options.binary === undefined ? envInput('binary') : options.binary,
    from: options.from === undefined ? envInput('from') : options.from,
    to: options.to === undefined ? envInput('to') : options.to,
  };
  if (inputs.mode === 'smart') inputs.mode = 'commits';
  if (!['commits', 'title'].includes(inputs.mode)) throw new ActionError(`Unsupported mode: ${inputs.mode}`);
  const name = options.eventName || eventName();
  const event = options.event || eventPayload();
  let apiUrl;
  const getApiUrl = () => {
    if (!apiUrl) apiUrl = options.apiUrl || configuredApiUrl();
    return apiUrl;
  };
  const selected = inputCommits(event, name, inputs);
  let commits;
  if (inputs.mode === 'title' && name === 'pull_request') {
    if (!event.pull_request || typeof event.pull_request.title !== 'string') throw new ActionError('title mode requires a pull_request event with a title');
    commits = [{ sha: event.pull_request.head.sha, message: event.pull_request.title }];
  } else if (selected.kind === 'none') {
    commits = [];
  } else if (selected.kind === 'initial') {
    commits = await listInitialCommits({ fetchImpl, apiUrl: getApiUrl(), token: inputs.token, repo: selected.repo, head: selected.head });
  } else {
    commits = await compareCommits({ fetchImpl, apiUrl: getApiUrl(), token: inputs.token, repo: selected.repo, headRepo: selected.headRepo || selected.repo, before: selected.before, head: selected.head });
  }
  if (commits.length === 0) {
    summarize({ valid: true, results: [] }, 0);
    return 0;
  }
  const config = await loadConfig({ fetchImpl, apiUrl: getApiUrl(), token: inputs.token, repo: selected.configRepo, head: selected.head || (commits[commits.length - 1] && commits[commits.length - 1].sha), file: inputs.configFile });
  const temporary = fs.mkdtempSync(path.join(os.tmpdir(), 'commit-guard-config-'));
  const configPath = path.join(temporary, 'config.json');
  let downloaded;
  try {
    fs.writeFileSync(configPath, JSON.stringify(config));
    const checker = inputs.binary ? path.resolve(process.cwd(), inputs.binary) : (downloaded = await downloadChecker({ fetchImpl })).path;
    if (!fs.existsSync(checker)) throw new ActionError(`Checker binary does not exist: ${checker}`);
    const checked = runChecker(checker, configPath, commits, options.spawnImpl);
    summarize(checked.result, commits.length);
    return checked.status;
  } finally {
    try { fs.rmSync(temporary, { recursive: true, force: true }); } catch {}
    if (downloaded) downloaded.cleanup();
  }
}

async function main() {
  try {
    const code = await run();
    process.exitCode = code;
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    annotate(message, error && error.code === 1 ? 'commit-guard' : 'commit-guard runtime');
    process.stdout.write('commit-guard: failed; see the error annotation for details\n');
    writeSummary('## commit-guard\n\ncommit-guard: failed; see the error annotation for details');
    process.exitCode = error && Number.isInteger(error.code) ? error.code : 2;
  }
}

module.exports = {
  ActionError,
  GitHubApiError,
  annotate,
  checksumFor,
  compareCommits,
  downloadChecker,
  escapedCommand,
  fetchJson,
  inputCommits,
  listInitialCommits,
  loadConfig,
  main,
  platformTarget,
  request,
  run,
  runChecker,
};

if (require.main === module) main();
