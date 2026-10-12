/** Each opening is a new editor, including two successive creations without an ID. */
export function createEditorSession() {
  let generation = 0;
  let active = false;
  return {
    reset(open: boolean) {
      generation++;
      active = open;
    },
    close() {
      generation++;
      active = false;
    },
    capture(): () => boolean {
      const captured = generation;
      return () => active && captured === generation;
    },
  };
}
