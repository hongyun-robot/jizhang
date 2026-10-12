import { test } from 'node:test';
import assert from 'node:assert/strict';
import { assertFixture } from './fixture.mjs';

test('fixture guard rejects remote targets before accessing them', async () => {
  await assert.rejects(assertFixture('https://example.com'), /local isolated fixture/);
});
