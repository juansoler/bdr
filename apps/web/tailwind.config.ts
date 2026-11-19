import type { Config } from "tailwindcss";

const config: Config = {
  content: [
    "./app/**/*.{ts,tsx}",
    "./components/**/*.{ts,tsx}",
    "./lib/**/*.{ts,tsx}",
  ],
  theme: {
    extend: {
      fontFamily: {
        display: ["var(--font-display)", "sans-serif"],
        sans: ["var(--font-body)", "sans-serif"],
        mono: ["var(--font-mono)", "monospace"],
      },
      colors: {
        ink: {
          950: "#06090c",
          900: "#0a0f14",
          850: "#0e151b",
          800: "#121b22",
          700: "#1a2731",
          600: "#253743",
        },
        signal: "#c8ff4d",
        alert: "#ff8a4c",
        danger: "#ff5c5c",
        ice: "#8fd6ff",
      },
      boxShadow: {
        panel: "0 24px 60px -30px rgba(0,0,0,.9)",
        signal: "0 0 24px -6px rgba(200,255,77,.55)",
      },
      letterSpacing: {
        widest2: ".22em",
      },
    },
  },
  plugins: [],
};

export default config;
