#!/usr/bin/env bun
/**
 * ports-render.js — Stream Deck ports page renderer
 *
 * Usage:
 *   bun ports-render.js <slot>       → refresh data if stale, render all slots, print path to key-<slot>.png
 *   bun ports-render.js page next    → advance page, reset cache
 *   bun ports-render.js page prev    → go back page, reset cache
 *   bun ports-render.js icon         → print path to the static app-launcher icon
 *
 * State : /tmp/streamdeck-ports.json
 * Images: /tmp/streamdeck-ports/key-N.png
 */

import sharp from "sharp";
import { mkdir, writeFile, readFile } from "fs/promises";
import { existsSync } from "fs";
import {
  getListeningPorts,
  isDevProcess,
} from "port-whisperer/src/scanner.js";

// ── config ────────────────────────────────────────────────────────────────────

const STATE_FILE = "/tmp/streamdeck-ports.json";
const IMG_DIR    = "/tmp/streamdeck-ports";
const ICON_PATH  = import.meta.dir + "/assets/ports.png";
const CACHE_TTL  = 5_000; // ms
const SLOTS      = 9;     // 3×3 middle grid, column-major (slots 0-8)

const SCANNER_PATH =
  "port-whisperer/src/scanner.js";

// ── colors ────────────────────────────────────────────────────────────────────

const BG = "#0d1117";

const STATUS_COLORS = {
  healthy:  "#22c55e",
  orphaned: "#f59e0b",
  zombie:   "#ef4444",
};

const FW_COLORS = {
  "Next.js":    "#ffffff",
  "Nuxt":       "#00dc82",
  "Vite":       "#a78bfa",
  "React":      "#61dafb",
  "Vue":        "#42b883",
  "Angular":    "#dd0031",
  "Svelte":     "#ff6600",
  "SvelteKit":  "#ff6600",
  "Express":    "#aaaaaa",
  "Fastify":    "#dddddd",
  "NestJS":     "#e0234e",
  "Remix":      "#60a5fa",
  "Astro":      "#c026d3",
  "Django":     "#6ee7b7",
  "Flask":      "#e2e8f0",
  "FastAPI":    "#009688",
  "Python":     "#fbbf24",
  "Go":         "#00acd7",
  "Rust":       "#de8a5a",
  "Node.js":    "#68a063",
  "Ruby":       "#cc342d",
  "Java":       "#e76f00",
  "Hono":       "#ff6600",
  "Webpack":    "#8dd3f7",
  "esbuild":    "#ffcf00",
  "Parcel":     "#e0b24d",
  "Docker":     "#2496ed",
  "PostgreSQL": "#336791",
  "Redis":      "#dc382d",
  "MySQL":      "#00758f",
  "MongoDB":    "#47a248",
  "nginx":      "#009639",
  "LocalStack": "#e2e8f0",
};

// ── state ─────────────────────────────────────────────────────────────────────

async function loadState() {
  try {
    return JSON.parse(await readFile(STATE_FILE, "utf8"));
  } catch {
    return { page: 0, fetched: 0, ports: [] };
  }
}

async function saveState(state) {
  await writeFile(STATE_FILE, JSON.stringify(state));
}

// ── data ──────────────────────────────────────────────────────────────────────

async function fetchPorts() {
  const all = await getListeningPorts();
  return all.filter((p) => isDevProcess(p.processName, p.command));
}

// ── SVG helpers ───────────────────────────────────────────────────────────────

function esc(s) {
  return String(s)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function makePortSvg(e) {
  const fwColor  = FW_COLORS[e.framework] || "#94a3b8";
  const dotColor = STATUS_COLORS[e.status] || "#22c55e";
  const port     = esc(`:${e.port}`);
  const proc     = esc((e.processName || "?").slice(0, 11));
  const project  = esc((e.projectName || "").slice(0, 9));
  const fw       = esc((e.framework   || "").slice(0, 8));
  const uptime   = esc(e.uptime || "");
  const dot      = project && fw ? " · " : "";

  // Status dot — tiny circle top-right corner
  const statusDot = `<circle cx="66" cy="6" r="4" fill="${dotColor}"/>`;

  return `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" fill="${BG}"/>
  ${statusDot}
  <text x="33" y="18" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="18" fill="white" text-anchor="middle">${port}</text>
  <text x="36" y="33" font-family="DejaVu Sans,sans-serif" font-size="12" fill="#7dd3fc" text-anchor="middle">${proc}</text>
  <text x="36" y="48" font-family="DejaVu Sans,sans-serif" font-size="10" text-anchor="middle"><tspan fill="#94a3b8">${project}</tspan><tspan fill="#4b5563">${dot}</tspan><tspan fill="${fwColor}">${fw}</tspan></text>
  <text x="36" y="62" font-family="DejaVu Sans,sans-serif" font-size="10" fill="#4b5563" text-anchor="middle">${uptime}</text>
</svg>`;
}

function makeEmptySvg() {
  return `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" fill="black"/>
</svg>`;
}

function makeNoPortsSvg() {
  return `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" fill="${BG}"/>
  <text x="36" y="32" font-family="DejaVu Sans,sans-serif" font-size="10" fill="#4b5563" text-anchor="middle">no dev</text>
  <text x="36" y="46" font-family="DejaVu Sans,sans-serif" font-size="10" fill="#4b5563" text-anchor="middle">ports</text>
</svg>`;
}

// ── launcher icon ─────────────────────────────────────────────────────────────

async function generateBadgeIcon(count) {
  const badgeR  = 10;
  const badgeX  = 72 - badgeR - 2;
  const badgeY  = badgeR + 2;
  const fontSize = count >= 10 ? 10 : 13;
  const badge = count > 0
    ? `<circle cx="${badgeX}" cy="${badgeY}" r="${badgeR}" fill="#ef4444"/>
       <text x="${badgeX}" y="${badgeY + Math.floor(fontSize / 3)}" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="${fontSize}" fill="white" text-anchor="middle">${count}</text>`
    : "";

  const svg = `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" rx="10" fill="#0d1117"/>
  <rect x="6"  y="12" width="30" height="8" rx="2" fill="#00acd7" opacity="0.9"/>
  <rect x="40" y="12" width="26" height="8" rx="2" fill="#4b5563"/>
  <rect x="6"  y="24" width="20" height="8" rx="2" fill="#fbbf24" opacity="0.9"/>
  <rect x="30" y="24" width="36" height="8" rx="2" fill="#4b5563"/>
  <rect x="6"  y="36" width="26" height="8" rx="2" fill="#00dc82" opacity="0.9"/>
  <rect x="36" y="36" width="30" height="8" rx="2" fill="#4b5563"/>
  <rect x="6"  y="48" width="16" height="8" rx="2" fill="#a78bfa" opacity="0.9"/>
  <rect x="26" y="48" width="40" height="8" rx="2" fill="#4b5563"/>
  <text x="36" y="68" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="9" fill="#6b7280" text-anchor="middle">PORTS</text>
  ${badge}
</svg>`;
  const outPath = `${IMG_DIR}/main-icon.png`;
  await mkdir(IMG_DIR, { recursive: true });
  await sharp(Buffer.from(svg)).png().toFile(outPath);
  return outPath;
}

async function generateAppIcon() {
  // Network / ports icon: dark BG, stacked port-style rows
  const svg = `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" rx="10" fill="#0d1117"/>
  <!-- decorative colored bars mimicking port rows -->
  <rect x="6"  y="12" width="30" height="8" rx="2" fill="#00acd7" opacity="0.9"/>
  <rect x="40" y="12" width="26" height="8" rx="2" fill="#4b5563"/>
  <rect x="6"  y="24" width="20" height="8" rx="2" fill="#fbbf24" opacity="0.9"/>
  <rect x="30" y="24" width="36" height="8" rx="2" fill="#4b5563"/>
  <rect x="6"  y="36" width="26" height="8" rx="2" fill="#00dc82" opacity="0.9"/>
  <rect x="36" y="36" width="30" height="8" rx="2" fill="#4b5563"/>
  <rect x="6"  y="48" width="16" height="8" rx="2" fill="#a78bfa" opacity="0.9"/>
  <rect x="26" y="48" width="40" height="8" rx="2" fill="#4b5563"/>
  <!-- label -->
  <text x="36" y="68" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="9" fill="#6b7280" text-anchor="middle">PORTS</text>
</svg>`;
  await sharp(Buffer.from(svg)).png().toFile(ICON_PATH);
  return ICON_PATH;
}

// ── render ────────────────────────────────────────────────────────────────────

async function renderAll(state) {
  await mkdir(IMG_DIR, { recursive: true });
  const ports      = state.ports ?? [];
  const totalPages = Math.max(1, Math.ceil(ports.length / SLOTS));
  const page       = Math.min(state.page ?? 0, totalPages - 1);
  const offset     = page * SLOTS;

  const renders = [];
  for (let slot = 0; slot < SLOTS; slot++) {
    const entry = ports[offset + slot];
    let svg;
    if (ports.length === 0 && slot === 0) {
      svg = makeNoPortsSvg();
    } else if (entry) {
      svg = makePortSvg(entry);
    } else {
      svg = makeEmptySvg();
    }
    renders.push(
      sharp(Buffer.from(svg)).png().toFile(`${IMG_DIR}/key-${slot}.png`)
    );
  }
  await Promise.all(renders);
}

// ── main ──────────────────────────────────────────────────────────────────────

const [, , arg, arg2] = process.argv;

// ─────────────────────────────────────────────────────────────────────────────

if (arg === "icon") {
  // Generate (or regenerate) the launcher icon and print its path
  console.log(await generateAppIcon());

} else if (arg === "badge") {
  // Render the launcher icon with a live port-count badge
  let state = await loadState();
  if (Date.now() - (state.fetched ?? 0) > CACHE_TTL) {
    state.ports   = await fetchPorts();
    state.fetched = Date.now();
    await saveState(state);
  }
  console.log(await generateBadgeIcon((state.ports ?? []).length));

} else if (arg === "page") {
  const state = await loadState();
  const total = Math.max(1, Math.ceil((state.ports?.length ?? 0) / SLOTS));
  const dir   = arg2 === "prev" ? -1 : 1;
  state.page  = ((state.page ?? 0) + dir + total) % total;
  state.fetched = 0; // force re-render on next poll
  await saveState(state);

} else {
  const slot = parseInt(arg, 10);
  if (isNaN(slot)) process.exit(1);

  let state = await loadState();

  if (Date.now() - (state.fetched ?? 0) > CACHE_TTL) {
    state.ports   = await fetchPorts();
    state.fetched = Date.now();
    await saveState(state);
  }

  await renderAll(state);
  console.log(`${IMG_DIR}/key-${slot}.png`);
}
