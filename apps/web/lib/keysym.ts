// keysym.ts — mapeo de KeyboardEvent del navegador a keysyms X11 (los que
// hablan RFB, XTEST y el protocolo de tiles del agente).
//
// Printables: Latin-1 va por código directo (0x20-0xFF); otro Unicode usa el
// rango de keysyms unicode (0x01000000 + codepoint), que Windows inyecta con
// KEYEVENTF_UNICODE y X11 suele resolver vía XKB.

const specialKeys: Record<string, number> = {
  Enter: 0xff0d,
  Backspace: 0xff08,
  Tab: 0xff09,
  Escape: 0xff1b,
  ArrowLeft: 0xff51,
  ArrowUp: 0xff52,
  ArrowRight: 0xff53,
  ArrowDown: 0xff54,
  Home: 0xff50,
  End: 0xff57,
  PageUp: 0xff55,
  PageDown: 0xff56,
  Insert: 0xff63,
  Delete: 0xffff,
  F1: 0xffbe,
  F2: 0xffbf,
  F3: 0xffc0,
  F4: 0xffc1,
  F5: 0xffc2,
  F6: 0xffc3,
  F7: 0xffc4,
  F8: 0xffc5,
  F9: 0xffc6,
  F10: 0xffc7,
  F11: 0xffc8,
  F12: 0xffc9,
  ShiftLeft: 0xffe1,
  ShiftRight: 0xffe2,
  ControlLeft: 0xffe3,
  ControlRight: 0xffe4,
  AltLeft: 0xffe9,
  AltRight: 0xffea,
  MetaLeft: 0xffeb,
  MetaRight: 0xffec,
  CapsLock: 0xffe5,
  NumLock: 0xff7f,
  ScrollLock: 0xff14,
  Pause: 0xff13,
  PrintScreen: 0xff61,
  ContextMenu: 0xff67,
};

// keysymFromEvent devuelve el keysym de la tecla, o null si no se mapea
// (teclas muertas, IME, claves de navegador…).
export function keysymFromEvent(ev: KeyboardEvent): number | null {
  const mapped = specialKeys[ev.key];
  if (mapped !== undefined) return mapped;

  if (ev.key.length === 1) {
    const code = ev.key.charCodeAt(0);
    if (code >= 0x20 && code <= 0xff) return code;
    if (code >= 0x100) return 0x01000000 + code;
  }
  return null;
}

// Keysyms de modificadores, para liberarlos al perder el foco del visor.
export const modifierKeysyms = [
  0xffe1, 0xffe2, // Shift L/R
  0xffe3, 0xffe4, // Control L/R
  0xffe9, 0xffea, // Alt L/R
  0xffeb, 0xffec, // Super L/R
];
