import { describe, expect, it } from 'vitest';
import { returnTarget, activeKey } from './routes';
import { rememberSearch, clearNavigationMemory, navTarget } from './memory';

describe('session navigation', () => {
  it('keeps query and fragment and strips a deployment prefix exactly once', () => {
    expect(
      returnTarget(
        { pathname: '/records', search: '?page=2', hash: '#row' },
        'member'
      )
    ).toBe('/records?page=2#row');
    expect(
      returnTarget('/jizhang/records?page=2#row', 'member', '/jizhang')
    ).toBe('/records?page=2#row');
  });
  it('rejects external and forbidden login destinations', () => {
    for (const url of [
      'https://example.com',
      '//example.com',
      '/\\example.com',
      '/login',
      '/settings',
    ])
      expect(returnTarget(url, 'viewer')).toBe('/');
    expect(returnTarget('/accounts', 'admin')).toBe('/accounts');
    expect(returnTarget('/accounts', 'viewer')).toBe('/accounts');
    expect(returnTarget('/categories', 'member')).toBe('/categories');
    expect(activeKey('/accounts')).toBe('mine');
    expect(activeKey('/categories')).toBe('mine');
    expect(activeKey('/liabilities/1')).toBe('mine');
  });
  it('clears remembered filters and avoids adding a duplicate history item', () => {
    rememberSearch('/records', '?page=3');
    expect(navTarget('/records', '/records?page=3')).toBeNull();
    clearNavigationMemory();
    expect(navTarget('/records', '/stats')).toBe('/records');
  });
});
