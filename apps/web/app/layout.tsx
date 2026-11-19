import type { Metadata } from "next";
import localFont from "next/font/local";
import "./globals.css";
import { Providers } from "./providers";

// Fuentes autoalojadas (apps/web/fonts): el build no depende de la red ni de
// Google Fonts (crítico dentro de docker build). Licencias OFL junto a los ficheros.
const display = localFont({
  src: [
    { path: "../fonts/barlow-condensed-latin-500.woff2", weight: "500" },
    { path: "../fonts/barlow-condensed-latin-600.woff2", weight: "600" },
    { path: "../fonts/barlow-condensed-latin-700.woff2", weight: "700" },
  ],
  variable: "--font-display",
  display: "swap",
});

const body = localFont({
  src: [
    { path: "../fonts/ibm-plex-sans-latin-var.woff2", weight: "400 600" },
  ],
  variable: "--font-body",
  display: "swap",
});

const mono = localFont({
  src: [
    { path: "../fonts/ibm-plex-mono-latin-400.woff2", weight: "400" },
    { path: "../fonts/ibm-plex-mono-latin-500.woff2", weight: "500" },
  ],
  variable: "--font-mono",
  display: "swap",
});

export const metadata: Metadata = {
  title: "BDR Console",
  description: "Consola de protección de datos y recuperación ante desastres",
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="es" className={`${display.variable} ${body.variable} ${mono.variable}`}>
      <body>
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
