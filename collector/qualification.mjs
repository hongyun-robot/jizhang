// Real-time, isolated admission run. This never writes the application database.
import { readFile, writeFile, rename, mkdir } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { pathToFileURL } from 'node:url';
import { collect, launch, disposeBrowser, productFor, CollectionError } from './collector.mjs';

const DAY = 86400000;
export const shanghaiDate = (date) => new Date(date.getTime() + 8 * 3600000).toISOString().slice(0, 10);
export function plan(start) {
  const first = Date.parse(`${shanghaiDate(start)}T00:00:00+08:00`) + DAY;
  return Array.from({ length: 3 }, (_, day) => [8, 12, 18, 22].map((hour) => {
    const time = first + day * DAY + hour * 3600000;
    return { id: `${shanghaiDate(new Date(time))}/${hour}`, due_at: new Date(time).toISOString(), state: 'pending' };
  })).flat();
}

export function validate(envelope, now = new Date()) {
  const p = productFor('J07241');
  for (const field of ['code', 'name', 'registration_code', 'currency', 'source_code']) {
    if (envelope[field] !== p[field]) throw new CollectionError('envelope_identity_mismatch');
  }
  if (envelope.request_fund_code !== p.code || envelope.source_url !== p.page_url ||
      !Number.isFinite(Date.parse(envelope.fetched_at)) || Math.abs(Date.parse(envelope.fetched_at) - now.getTime()) > 300000 ||
      envelope.response?.code !== '0000' || !Array.isArray(envelope.response.data) ||
      !envelope.response.data.length || envelope.response.data.length > 9999) throw new CollectionError('invalid_business_response');
  const rows = new Map();
  const positive = (v) => typeof v === 'string' && /^\d+(\.\d{1,8})?$/.test(v) && Number(v) > 0 && Number(v) < 1e12;
  for (const row of envelope.response.data) {
    const date = row.cfm_date;
    if (typeof date !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(date) ||
        !Number.isFinite(Date.parse(date)) || new Date(date).toISOString().slice(0, 10) !== date ||
        date < '2025-08-29' || date > shanghaiDate(now) || row.prd_code !== p.source_code ||
        !positive(row.nav) || (row.tot_nav != null && row.tot_nav !== '' && !positive(row.tot_nav))) {
      throw new CollectionError('invalid_nav');
    }
    const value = { date, nav: row.nav, total_nav: row.tot_nav || null };
    if (rows.has(date) && JSON.stringify(rows.get(date)) !== JSON.stringify(value)) throw new CollectionError('conflicting_nav');
    rows.set(date, value);
  }
  const data = [...rows.values()].sort((a, b) => a.date.localeCompare(b.date));
  return { rows: data, count: data.length, earliest: data[0].date, latest: data.at(-1),
    sha256: createHash('sha256').update(JSON.stringify(data)).digest('hex') };
}

export function assess(state) {
  if (state.checks?.some((check) => check.state === 'failed')) return 'failed';
  if (state.slots.some((slot) => ['failed', 'missed', 'interrupted'].includes(slot.state))) return 'failed';
  if (!state.slots.every((slot) => slot.state === 'succeeded')) return 'running';
  if (!state.proofs.cold_start || !state.proofs.process_restart || !state.proofs.fresh_anonymous_history) return 'incomplete';
  return state.proofs.new_disclosure_or_revision ? 'passed_sampling' : 'needs_revision_validation';
}

export async function main() {
  if (process.env.WEALTH_QUALIFICATION !== '1') throw new Error('explicit qualification opt-in required');
  const directory = process.env.WEALTH_EVIDENCE_DIR || '/evidence';
  await mkdir(directory, { recursive: true });
  const statePath = `${directory}/state.json`;
  let state;
  try { state = JSON.parse(await readFile(statePath, 'utf8')); }
  catch (error) {
    if (error.code !== 'ENOENT') throw error;
    const start = new Date();
    state = { format_version: 1, product: 'J07241', timezone: 'Asia/Shanghai', started_at: start.toISOString(),
      source_version: process.env.WEALTH_SOURCE_VERSION, boots: [], status: 'running', slots: plan(start), proofs: {}, checks: [], baseline: null };
  }
  state.boots.push(new Date().toISOString());
  // A crashed in-flight scheduled attempt is evidence of failure, never silently
  // retried as if the original slot had succeeded on time.
  for (const slot of state.slots) if (slot.state === 'running') slot.state = 'interrupted';
  const save = async () => {
    state.status = assess(state); state.updated_at = new Date().toISOString();
    await writeFile(`${statePath}.tmp`, JSON.stringify(state, null, 2) + '\n', { mode: 0o600 });
    await rename(`${statePath}.tmp`, statePath);
  };
  await save();
  const check = async (id, slot) => {
    const entry = { id, started_at: new Date().toISOString(), state: 'running', attempts: [] };
    state.checks.push(entry);
    if (slot) { slot.state = 'running'; slot.started_at = entry.started_at; }
    await save();
    let envelope;
    for (let attempt = 0; attempt < 2; attempt++) {
      let browser;
      try {
        browser = await launch();
        envelope = await collect(browser, 'J07241');
        const result = validate(envelope);
        entry.attempts.push({ result: 'valid', finished_at: new Date().toISOString() });
        Object.assign(entry, { state: 'succeeded', count: result.count, earliest: result.earliest, latest: result.latest, sha256: result.sha256 });
        if (!state.baseline) state.baseline = result.rows;
        else {
          const old = new Map(state.baseline.map((row) => [row.date, row]));
          const changes = result.rows.filter((row) => !old.has(row.date) || JSON.stringify(old.get(row.date)) !== JSON.stringify(row));
          if (changes.length) state.proofs.new_disclosure_or_revision = { check: id, observed_at: new Date().toISOString(), dates: changes.map((row) => row.date) };
        }
        const name = `public-${String(state.checks.length).padStart(3, '0')}.json`;
        await writeFile(`${directory}/${name}`, JSON.stringify(envelope) + '\n', { mode: 0o600 });
        entry.public_envelope = name;
        state.proofs.fresh_anonymous_history = true;
        break;
      } catch (error) {
        entry.attempts.push({ result: 'failed', code: error instanceof CollectionError ? error.code : 'browser_or_storage_error',
          http_status: Number.isInteger(error.httpStatus) ? error.httpStatus : null, finished_at: new Date().toISOString() });
        entry.state = 'failed';
      } finally { if (browser) await disposeBrowser(browser).catch(() => {}); }
    }
    entry.finished_at = new Date().toISOString();
    if (slot) { slot.state = entry.state; slot.finished_at = entry.finished_at; slot.check_id = id; }
    await save();
    process.stdout.write(JSON.stringify({ event: 'qualification_check', id, state: entry.state, latest: entry.latest?.date, count: entry.count }) + '\n');
    return entry.state === 'succeeded';
  };
  if (!state.proofs.cold_start && !state.checks.some((check) => check.id === 'cold-start')) {
    state.proofs.cold_start = await check('cold-start');
    await save();
    // Intentional first process exit exercises Docker restart + evidence resume.
    process.exitCode = 75; return;
  }
  if (!state.proofs.process_restart && state.boots.length >= 2 && !state.checks.some((check) => check.id === 'restart-fresh-context')) {
    const last = Date.parse(state.checks.at(-1)?.started_at || state.started_at);
    await new Promise((resolve) => setTimeout(resolve, Math.max(0, 61000 - (Date.now() - last))));
    state.proofs.process_restart = await check('restart-fresh-context');
    await save();
  }
  if (state.status === 'failed') {
    process.stdout.write(JSON.stringify({ event: 'qualification_precheck_failed' }) + '\n');
    setInterval(() => {}, 3600000); return;
  }
  while (state.slots.some((slot) => slot.state === 'pending')) {
    const slot = state.slots.find((item) => item.state === 'pending');
    const remaining = Date.parse(slot.due_at) - Date.now();
    if (remaining > 0) { await new Promise((resolve) => setTimeout(resolve, Math.min(remaining, 30000))); continue; }
    if (remaining < -5 * 60000) { slot.state = 'missed'; slot.detected_at = new Date().toISOString(); await save(); continue; }
    await check(slot.id, slot);
  }
  state.completed_at = new Date().toISOString(); await save();
  process.stdout.write(JSON.stringify({ event: 'qualification_finished', status: state.status }) + '\n');
  // Retain a quiet health-readable evidence container; no more source requests.
  setInterval(() => {}, 3600000);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch(() => { process.stderr.write('qualification worker failed; inspect durable evidence\n'); process.exitCode = 1; });
}
