import raw from './tokens.json';
import type { ResolvedTheme } from '../stores/theme';

export const UI_FONT = raw.light['--font-ui'];
export function uiTokens(mode: ResolvedTheme) {
  return raw[mode];
}
