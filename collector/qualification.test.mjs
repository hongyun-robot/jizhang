import { test } from 'node:test';
import assert from 'node:assert/strict';
import { plan, validate, assess } from './qualification.mjs';
import { PRODUCTS } from './collector.mjs';

const now = new Date('2026-10-16T01:30:00Z');
const envelope = () => ({ ...PRODUCTS.J07241, source_url: PRODUCTS.J07241.page_url, request_fund_code: 'J07241',
  fetched_at: now.toISOString(), response: { code: '0000', data: [{ cfm_date: '2026-10-15', nav: '1.02142', tot_nav: '1.02142', prd_code: 'Y0408Ux' }] } });

test('qualification uses twelve real slots across three complete Shanghai days including Monday', () => {
  const slots = plan(now);
  assert.equal(slots.length, 12);
  assert.equal(slots[0].due_at, '2026-10-17T00:00:00.000Z');
  assert.equal(slots.at(-1).due_at, '2026-10-19T14:00:00.000Z');
  assert.equal(plan(new Date('2026-10-16T17:00:00Z'))[0].id, '2026-10-18/8');
});

test('schema validates identity, dates, precision, duplicates and latest source date', () => {
  assert.equal(validate(envelope(), now).latest.date, '2026-10-15');
  for (const patch of [ { cfm_date: '2026-10-17' }, { cfm_date: '2026-02-30' }, { nav: 'NaN' }, { nav: '0' }, { nav: '1.123456789' }, { prd_code: 'other' } ]) {
    const value = envelope(); Object.assign(value.response.data[0], patch);
    assert.throws(() => validate(value, now));
  }
  const value = envelope(); value.response.data.push({ ...value.response.data[0], nav: '2' });
  assert.throws(() => validate(value, now), /conflicting_nav/);
  assert.throws(() => validate({ ...envelope(), registration_code: 'other' }, now));
  assert.throws(() => validate({ ...envelope(), fetched_at: 'invalid' }, now));
});

test('admission never passes missing, crashed, failed slots or missing disclosure proof', () => {
  const state = { slots: plan(now), proofs: {} };
  assert.equal(assess(state), 'running');
  state.slots[0].state = 'interrupted'; assert.equal(assess(state), 'failed');
  state.slots.forEach((slot) => { slot.state = 'succeeded'; });
  assert.equal(assess(state), 'incomplete');
  state.proofs = { cold_start: true, process_restart: true, fresh_anonymous_history: true };
  assert.equal(assess(state), 'needs_revision_validation');
  state.proofs.new_disclosure_or_revision = { dates: ['2026-10-16'] };
  assert.equal(assess(state), 'passed_sampling');
  state.checks = [{ id: 'restart-fresh-context', state: 'failed' }];
  assert.equal(assess(state), 'failed');
});
