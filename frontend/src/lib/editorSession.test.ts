import { describe, expect, it } from 'vitest';
import { createEditorSession } from './editorSession';

describe('editor response ownership', () => {
  it('accepts the normal save but rejects success or failure from a previous opening', () => {
    const editor = createEditorSession();
    editor.reset(true);
    const old = editor.capture();
    expect(old()).toBe(true);
    editor.close();
    editor.reset(true);
    expect(old()).toBe(false);
    expect(editor.capture()()).toBe(true);
  });
  it('rejects callbacks after a target/session change or unmount', () => {
    const editor = createEditorSession();
    editor.reset(true);
    const first = editor.capture();
    editor.reset(true);
    expect(first()).toBe(false);
    const next = editor.capture();
    editor.close();
    expect(next()).toBe(false);
  });
});
