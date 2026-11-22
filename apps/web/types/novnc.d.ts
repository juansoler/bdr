// Declaraciones de tipo para @novnc/novnc (el paquete no trae tipos).
// Solo la superficie que usa el visor de pantalla del panel.
declare module "@novnc/novnc" {
  export default class RFB {
    constructor(
      target: HTMLElement,
      webSocketOrURL: WebSocket | string,
      options?: {
        shared?: boolean;
        credentials?: { password?: string };
      },
    );
    disconnect(): void;
    sendCtrlAltDel(): void;
    get focus(): boolean;
    set focus(value: boolean);
    get scaleViewport(): boolean;
    set scaleViewport(value: boolean);
    get resizeSession(): boolean;
    set resizeSession(value: boolean);
    get qualityLevel(): number; // 0-9
    set qualityLevel(value: number);
    get compressionLevel(): number; // 0-9
    set compressionLevel(value: number);
    get showDotCursor(): boolean;
    set showDotCursor(value: boolean);
    get background(): string;
    set background(value: string);
    addEventListener(
      type:
        | "connect"
        | "disconnect"
        | "credentialsrequired"
        | "clipboard"
        | "desktopname"
        | "securityfailure",
      listener: (ev: CustomEvent) => void,
    ): void;
    removeEventListener(type: string, listener: (ev: CustomEvent) => void): void;
  }
}
