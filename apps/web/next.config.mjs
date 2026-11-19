import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.dirname(fileURLToPath(import.meta.url));
/** @type {import('next').NextConfig} */
const nextConfig = {
  // Export estático: el panel se embebe en el binario de bdr-control
  // (apps/server/internal/webui). Sin Node en runtime.
  output: "export",
  poweredByHeader: false,
};

export default nextConfig;
