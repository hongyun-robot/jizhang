import { collect, disposeBrowser, launch } from './collector.mjs';

if (process.env.WEALTH_LIVE_SMOKE !== '1') throw new Error('Set WEALTH_LIVE_SMOKE=1 for the public read-only smoke');
const browser = await launch();
try {
  const result = await collect(browser, 'J07241');
  const rows = [...result.response.data].sort((a, b) => b.cfm_date.localeCompare(a.cfm_date));
  process.stdout.write(JSON.stringify({
    code: result.code, registration_code: result.registration_code, source_url: result.source_url,
    fetched_at: result.fetched_at, count: rows.length,
    latest: { date: rows[0].cfm_date, unit_nav: rows[0].nav, total_nav: rows[0].tot_nav },
    source_codes: [...new Set(rows.map((r) => r.prd_code))],
  }, null, 2) + '\n');
} catch (error) {
  process.stdout.write(JSON.stringify({
    code: 'J07241', fetched_at: new Date().toISOString(), status: 'failed',
    error: typeof error.code === 'string' ? error.code : 'collection_failed',
    http_status: Number.isInteger(error.httpStatus) ? error.httpStatus : null,
    browser_version: browser.version(), platform: process.platform,
  }, null, 2) + '\n');
  process.exitCode = 1;
} finally { await disposeBrowser(browser); }
