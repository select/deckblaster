#!/usr/bin/env bun
/**
 * docker-render.js — Stream Deck docker page renderer
 *
 * Usage:
 *   bun docker-render.js slot <n>        → refresh data if stale, render all slots, print path to key-<slot>.png
 *   bun docker-render.js page next|prev  → advance/retreat page
 *   bun docker-render.js toggle <slot>   → start/stop container in slot
 *   bun docker-render.js badge           → print path to main icon with running-count badge
 *   bun docker-render.js generate-assets → write nav button PNGs to assets/docker/
 *
 * State : /tmp/streamdeck-docker.json
 * Images: /tmp/streamdeck-docker/key-N.png
 */

import sharp from "sharp";
import { mkdir, writeFile, readFile } from "fs/promises";
import { existsSync } from "fs";
import { execSync, spawnSync } from "child_process";

// ── config ────────────────────────────────────────────────────────────────────

const STATE_FILE = "/tmp/streamdeck-docker.json";
const IMG_DIR    = "/tmp/streamdeck-docker";
const ASSETS_DIR = import.meta.dir + "/assets";
const CACHE_TTL  = 5_000; // ms
const SLOTS      = 9;     // 3×3 middle grid, column-major (slots 0-8)

const BG           = "#0d1117";
const DOCKER_BLUE  = "#2496ed";

// ── color helpers ─────────────────────────────────────────────────────────────

const STATE_COLORS = {
  running:    "#22c55e",
  paused:     "#f59e0b",
  restarting: "#60a5fa",
  exited:     "#ef4444",
  dead:       "#6b7280",
  created:    "#a78bfa",
};

function imageColor(image) {
  const img = image.toLowerCase();
  if (img.includes("postgres"))                        return "#336791";
  if (img.includes("redis"))                           return "#dc382d";
  if (img.includes("mysql") || img.includes("maria")) return "#00758f";
  if (img.includes("mongo"))                           return "#47a248";
  if (img.includes("nginx"))                           return "#009639";
  if (img.includes("node"))                            return "#68a063";
  if (img.includes("python") || img.includes("uvicorn")) return "#fbbf24";
  if (img.includes("localstack"))                      return "#e2e8f0";
  if (img.includes("golang") || img.includes("/go"))  return "#00acd7";
  if (img.includes("rust"))                            return "#de8a5a";
  return DOCKER_BLUE;
}

// ── SVG helpers ───────────────────────────────────────────────────────────────

function esc(s) {
  return String(s)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

/** Short image name: strip registry + org prefix, strip tag */
function shortImage(image) {
  // quay.io/apheris/hub-apps:0.49.1 → hub-apps
  // postgres:15-alpine → postgres
  const noTag  = image.split(":")[0];
  const parts  = noTag.split("/");
  return parts[parts.length - 1].slice(0, 10);
}

/** Short uptime from Status string e.g. "Up 3 hours" → "3h" / "Exited (0)" → "exited" */
function shortStatus(status) {
  if (!status) return "";
  const upMatch = status.match(/Up\s+(.+)/i);
  if (upMatch) {
    const s = upMatch[1].trim();
    return s
      .replace(" hours", "h").replace(" hour", "h")
      .replace(" minutes", "m").replace(" minute", "m")
      .replace(" days", "d").replace(" day", "d")
      .replace(" seconds", "s").replace(" second", "s")
      .slice(0, 7);
  }
  if (/exited/i.test(status)) return "exited";
  if (/paused/i.test(status)) return "paused";
  if (/restarting/i.test(status)) return "restarting";
  return status.slice(0, 7).toLowerCase();
}

/** First mapped port "8080->8080" → ":8080" */
function shortPorts(ports) {
  if (!ports) return "";
  const m = ports.match(/:(\d+)->/);
  return m ? `:${m[1]}` : "";
}

/** Wrap a container name into 1 or 2 lines, breaking at hyphens/underscores near the middle. */
function wrapName(raw) {
  const MAX = 9; // ~chars that fit at font-size 12 in 72px
  if (raw.length <= MAX) return [raw];

  // Find the word-boundary (hyphen/underscore) closest to the middle
  const mid = Math.floor(raw.length / 2);
  let breakAt = -1;
  for (let d = 0; d < raw.length; d++) {
    const lo = mid - d, hi = mid + d;
    if (lo >= 1 && (raw[lo] === "-" || raw[lo] === "_")) { breakAt = lo; break; }
    if (hi < raw.length - 1 && (raw[hi] === "-" || raw[hi] === "_")) { breakAt = hi; break; }
  }
  if (breakAt > 0) return [raw.slice(0, breakAt), raw.slice(breakAt + 1).slice(0, MAX)];
  // Hard split
  return [raw.slice(0, MAX), raw.slice(MAX, MAX * 2)];
}

function makeContainerSvg(c) {
  const dotColor = STATE_COLORS[c.State] || "#6b7280";
  const imgColor = imageColor(c.Image);
  const img      = esc(shortImage(c.Image));
  const uptime   = esc(shortStatus(c.Status));
  const port     = esc(shortPorts(c.Ports));
  const nameLines = wrapName(c.Names);

  // Card layout: top color badge (container name), then content below
  const PAD = 2, R = 6, CARD_BG = "#161b22";
  const cx = PAD, cy = PAD, cw = 72 - PAD * 2, ch = 72 - PAD * 2;
  const badgeX = cx + 3, badgeY = cy + 3, badgeH = 14, badgeW = cw - 6, badgeR = 4;
  const textX = cx + 5;

  // Status dot inside badge (right side)
  const statusDot = `<circle cx="${badgeX + badgeW - 6}" cy="${badgeY + badgeH / 2}" r="3" fill="${dotColor}"/>`;

  // Name in badge
  let badgeText;
  if (nameLines.length === 1) {
    badgeText = `<text x="${badgeX + 4}" y="${badgeY + badgeH - 4}" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="9" fill="#000000">${port || esc(nameLines[0])}</text>`;
  } else {
    badgeText = `<text x="${badgeX + 4}" y="${badgeY + badgeH - 4}" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="9" fill="#000000">${port || esc(nameLines[0])}</text>`;
  }

  // Container name lines
  const name1 = esc(nameLines[0]);
  const name2 = nameLines.length > 1 ? esc(nameLines[1]) : "";
  const nameSvg = name2
    ? `<text x="${textX}" y="32" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="9" fill="#e6edf3">${name1}</text>
  <text x="${textX}" y="42" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="9" fill="#e6edf3">${name2}</text>`
    : `<text x="${textX}" y="32" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="10" fill="#e6edf3">${name1}</text>`;
  const afterNameY = name2 ? 53 : 44;

  return `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" fill="${BG}"/>
  <rect x="${cx}" y="${cy}" width="${cw}" height="${ch}" rx="${R}" fill="${CARD_BG}"/>
  <rect x="${badgeX}" y="${badgeY}" width="${badgeW}" height="${badgeH}" rx="${badgeR}" fill="${imgColor}"/>
  ${badgeText}
  ${statusDot}
  ${nameSvg}
  <text x="${textX}" y="${afterNameY}" font-family="DejaVu Sans,sans-serif" font-size="9" fill="${imgColor}">${img}</text>
  <text x="${textX}" y="${afterNameY + 11}" font-family="DejaVu Sans,sans-serif" font-size="8" fill="#8b949e">${uptime}</text>
</svg>`;
}

function makeEmptySvg() {
  const PAD = 2, R = 6, CARD_BG = "#161b22";
  const cx = PAD, cy = PAD, cw = 72 - PAD * 2, ch = 72 - PAD * 2;
  return `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" fill="${BG}"/>
  <rect x="${cx}" y="${cy}" width="${cw}" height="${ch}" rx="${R}" fill="${CARD_BG}"/>
</svg>`;
}

function makeNoContainersSvg() {
  const PAD = 2, R = 6, CARD_BG = "#161b22";
  const cx = PAD, cy = PAD, cw = 72 - PAD * 2, ch = 72 - PAD * 2;
  return `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" fill="${BG}"/>
  <rect x="${cx}" y="${cy}" width="${cw}" height="${ch}" rx="${R}" fill="${CARD_BG}"/>
  <rect x="${cx + 3}" y="${cy + 3}" width="${cw - 6}" height="14" rx="4" fill="#30363d"/>
  <text x="36" y="42" font-family="DejaVu Sans,sans-serif" font-size="10" fill="#4b5563" text-anchor="middle">no</text>
  <text x="36" y="54" font-family="DejaVu Sans,sans-serif" font-size="10" fill="#4b5563" text-anchor="middle">containers</text>
</svg>`;
}

// ── data ──────────────────────────────────────────────────────────────────────

function fetchContainers() {
  try {
    const out = execSync("docker ps -a --format '{{json .}}'", {
      encoding: "utf8",
      timeout: 5000,
      stdio: ["pipe", "pipe", "pipe"],
    });
    return out
      .trim()
      .split("\n")
      .filter(Boolean)
      .map((line) => JSON.parse(line))
      .sort((a, b) => {
        // running first, then by name
        const stateOrder = { running: 0, restarting: 1, paused: 2, created: 3, exited: 4, dead: 5 };
        const sa = stateOrder[a.State] ?? 9;
        const sb = stateOrder[b.State] ?? 9;
        if (sa !== sb) return sa - sb;
        return a.Names.localeCompare(b.Names);
      });
  } catch {
    return [];
  }
}

// ── state ─────────────────────────────────────────────────────────────────────

async function loadState() {
  try {
    return JSON.parse(await readFile(STATE_FILE, "utf8"));
  } catch {
    return { page: 0, fetched: 0, containers: [] };
  }
}

async function saveState(state) {
  await writeFile(STATE_FILE, JSON.stringify(state));
}

// ── render ────────────────────────────────────────────────────────────────────

async function renderAll(state) {
  await mkdir(IMG_DIR, { recursive: true });
  const containers = state.containers ?? [];
  const totalPages = Math.max(1, Math.ceil(containers.length / SLOTS));
  const page       = Math.min(state.page ?? 0, totalPages - 1);
  const offset     = page * SLOTS;

  const renders = [];
  for (let slot = 0; slot < SLOTS; slot++) {
    const entry = containers[offset + slot];
    let svg;
    if (containers.length === 0 && slot === 0) {
      svg = makeNoContainersSvg();
    } else if (entry) {
      svg = makeContainerSvg(entry);
    } else {
      svg = makeEmptySvg();
    }
    renders.push(
      sharp(Buffer.from(svg)).png().toFile(`${IMG_DIR}/key-${slot}.png`)
    );
  }
  await Promise.all(renders);
}

// ── main icon with badge ──────────────────────────────────────────────────────

async function generateBadgeIcon(runningCount) {
  const badgeR   = 10;
  const badgeX   = 72 - badgeR - 2;
  const badgeY   = badgeR + 2;
  const fontSize = runningCount >= 10 ? 10 : 13;
  const badge    = runningCount > 0
    ? `<circle cx="${badgeX}" cy="${badgeY}" r="${badgeR}" fill="#22c55e"/>
       <text x="${badgeX}" y="${badgeY + Math.floor(fontSize / 3)}" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="${fontSize}" fill="white" text-anchor="middle">${runningCount}</text>`
    : "";

  // Docker whale-inspired icon
  const svg = `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" rx="10" fill="${BG}"/>
  <!-- whale body -->
  <rect x="8"  y="38" width="56" height="14" rx="4" fill="${DOCKER_BLUE}"/>
  <!-- container stack on whale -->
  <rect x="10" y="26" width="12" height="10" rx="2" fill="${DOCKER_BLUE}" opacity="0.9"/>
  <rect x="24" y="26" width="12" height="10" rx="2" fill="${DOCKER_BLUE}" opacity="0.9"/>
  <rect x="38" y="26" width="12" height="10" rx="2" fill="${DOCKER_BLUE}" opacity="0.9"/>
  <rect x="24" y="15" width="12" height="10" rx="2" fill="${DOCKER_BLUE}" opacity="0.7"/>
  <!-- spout -->
  <rect x="56" y="32" width="4"  height="6"  rx="2" fill="${DOCKER_BLUE}"/>
  <rect x="54" y="28" width="4"  height="4"  rx="2" fill="${DOCKER_BLUE}"/>
  <!-- label -->
  <text x="36" y="66" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="9" fill="#6b7280" text-anchor="middle">DOCKER</text>
  ${badge}
</svg>`;

  const outPath = `${IMG_DIR}/main-icon.png`;
  await mkdir(IMG_DIR, { recursive: true });
  await sharp(Buffer.from(svg)).png().toFile(outPath);
  return outPath;
}

// ── nav assets ────────────────────────────────────────────────────────────────

async function generateAssets() {
  await mkdir(ASSETS_DIR, { recursive: true });

  const navSvg = (label, arrow) => `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" rx="8" fill="#0d1829"/>
  <text x="36" y="42" font-family="DejaVu Sans,sans-serif" font-size="28" fill="${DOCKER_BLUE}" text-anchor="middle">${arrow}</text>
  <text x="36" y="62" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="10" fill="#4b5563" text-anchor="middle">${label}</text>
</svg>`;

  const exitSvg = `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" rx="8" fill="#1a0d0d"/>
  <text x="36" y="46" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="16" fill="#ef4444" text-anchor="middle">EXIT</text>
</svg>`;

  await Promise.all([
    sharp(Buffer.from(navSvg("PREV", "◀"))).png().toFile(`${ASSETS_DIR}/prev.png`),
    sharp(Buffer.from(navSvg("NEXT", "▶"))).png().toFile(`${ASSETS_DIR}/next.png`),
    sharp(Buffer.from(exitSvg)).png().toFile(`${ASSETS_DIR}/exit.png`),
  ]);
  console.error("Docker assets generated.");
}

// ── toggle ────────────────────────────────────────────────────────────────────

async function toggleContainer(slot) {
  const state      = await loadState();
  const containers = state.containers ?? [];
  const totalPages = Math.max(1, Math.ceil(containers.length / SLOTS));
  const page       = Math.min(state.page ?? 0, totalPages - 1);
  const entry      = containers[page * SLOTS + slot];
  if (!entry) return;

  const cmd = entry.State === "running" ? "stop" : "start";
  spawnSync("docker", [cmd, entry.ID], { stdio: "ignore" });
  // Bust cache so next poll fetches fresh data
  state.fetched = 0;
  await saveState(state);
}

// ── main ──────────────────────────────────────────────────────────────────────

const [, , arg, arg2] = process.argv;

if (arg === "generate-assets") {
  await generateAssets();

} else if (arg === "badge") {
  let state = await loadState();
  if (Date.now() - (state.fetched ?? 0) > CACHE_TTL) {
    state.containers = fetchContainers();
    state.fetched    = Date.now();
    await saveState(state);
  }
  const running = (state.containers ?? []).filter(c => c.State === "running").length;
  console.log(await generateBadgeIcon(running));

} else if (arg === "page") {
  const state = await loadState();
  const total = Math.max(1, Math.ceil((state.containers?.length ?? 0) / SLOTS));
  const dir   = arg2 === "prev" ? -1 : 1;
  state.page  = ((state.page ?? 0) + dir + total) % total;
  state.fetched = 0;
  await saveState(state);

} else if (arg === "toggle") {
  const slot = parseInt(arg2, 10);
  if (!isNaN(slot)) await toggleContainer(slot);

} else if (arg === "slot") {
  const slot = parseInt(arg2, 10);
  if (isNaN(slot)) process.exit(1);

  let state = await loadState();
  if (Date.now() - (state.fetched ?? 0) > CACHE_TTL) {
    state.containers = fetchContainers();
    state.fetched    = Date.now();
    await saveState(state);
  }

  await renderAll(state);
  console.log(`${IMG_DIR}/key-${slot}.png`);

} else {
  process.exit(1);
}
