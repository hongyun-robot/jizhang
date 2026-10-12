export function createKeyboardObserver(environment: {
  height: () => number;
  viewport: {
    height: number;
    offsetTop: number;
    addEventListener: (type: string, listener: () => void) => void;
    removeEventListener: (type: string, listener: () => void) => void;
  } | null;
  write: (value: number | null) => void;
  frame: (callback: () => void) => number;
  cancel: (id: number) => void;
}) {
  let users = 0;
  let frame = 0;
  const apply = () => {
    frame = 0;
    if (environment.viewport)
      environment.write(
        Math.max(
          0,
          Math.round(
            environment.height() -
              environment.viewport.height -
              environment.viewport.offsetTop
          )
        )
      );
  };
  const schedule = () => {
    if (!frame) frame = environment.frame(apply);
  };
  return {
    acquire(): () => void {
      if (++users === 1) {
        environment.viewport?.addEventListener('resize', schedule);
        environment.viewport?.addEventListener('scroll', schedule);
        apply();
      }
      let released = false;
      return () => {
        if (released) return;
        released = true;
        if (--users > 0) return;
        if (frame) environment.cancel(frame);
        frame = 0;
        environment.viewport?.removeEventListener('resize', schedule);
        environment.viewport?.removeEventListener('scroll', schedule);
        environment.write(null);
      };
    },
  };
}
