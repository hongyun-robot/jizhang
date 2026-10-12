import { useLayoutEffect, useRef } from 'react';
import { createEditorSession } from '../lib/editorSession';
import { useAuth } from '../stores/auth';

export function useEditorSession(open: boolean, target: string | number) {
  const editor = useRef(createEditorSession()).current;
  const epoch = useAuth((state) => state.epoch);
  useLayoutEffect(() => {
    editor.reset(open);
    return () => editor.close();
  }, [editor, open, target, epoch]);
  return editor;
}
