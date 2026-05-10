#!/usr/bin/env bun
/**
 * calendar-day.js — Stream Deck calendar day-view renderer
 *
 * Pure renderer: reads events from calendar-day-events.json (written by
 * calendar-fetch.py poll daemon) and renders SVG→PNG icons.
 *
 * Usage:
 *   bun calendar-day.js <slot>       → print path to key-<slot>.png (render if needed)
 *   bun calendar-day.js page next    → advance day, re-render all
 *   bun calendar-day.js page prev    → go back day, re-render all
 *   bun calendar-day.js header       → print path to header.png
 *   bun calendar-day.js open <n>     → open meeting URL for event n
 *   bun calendar-day.js generate-assets → write nav button PNGs
 *
 * State : ~/.local/share/deckblaster/calendar-day.json       (dayOffset only)
 * Events: ~/.local/share/deckblaster/calendar-day-events.json (from fetcher daemon)
 * Images: ~/.local/share/deckblaster/calendar-day/
 */

import sharp from "sharp";
import { mkdir, writeFile, readFile, stat } from "fs/promises";
import { existsSync } from "fs";
import { spawn } from "child_process";
import { join, dirname } from "path";
import { homedir } from "os";

// ── config ────────────────────────────────────────────────────────────────────

const CACHE_DIR    = join(homedir(), ".local", "share", "deckblaster");
const STATE_FILE   = join(CACHE_DIR, "calendar-day.json");
const EVENTS_FILE  = join(CACHE_DIR, "calendar-day-events.json");
const IMG_DIR      = join(CACHE_DIR, "calendar-day");
const SCRIPT_DIR   = dirname(import.meta.path);
const ASSETS_DIR   = join(SCRIPT_DIR, "assets");
const SLOTS        = 9;

// ── design tokens ─────────────────────────────────────────────────────────────

const W = 72, H = 72;
const BG   = "#000000";
const PAD  = 2;
const FONT = "DejaVu Sans,sans-serif";

const COLOR_NOW    = "#f85149";
const COLOR_20MIN  = "#f0883e";
const COLOR_1HR    = "#d29922";
const COLOR_4HR    = "#58a6ff";
const COLOR_FAR    = "#3fb950";
const COLOR_PAST   = "#484f58";

const TEXT_PRIMARY   = "#e6edf3";
const TEXT_SECONDARY = "#8b949e";

// ── helpers ───────────────────────────────────────────────────────────────────

function esc(s) {
  return String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

function getBarColor(event) {
  const now = Date.now();
  const start = new Date(event.dt).getTime();
  const end = new Date(event.end_dt).getTime();
  if (end <= now) return COLOR_PAST;
  if (start <= now && now < end) return COLOR_NOW;
  const min = (start - now) / 60000;
  if (min <= 20) return COLOR_20MIN;
  if (min <= 60) return COLOR_1HR;
  if (min <= 240) return COLOR_4HR;
  return COLOR_FAR;
}

function formatTime(iso) {
  const d = new Date(iso);
  return d.toLocaleTimeString("de-DE", { hour: "2-digit", minute: "2-digit", hour12: false });
}

function wrapText(text, maxChars = 10, maxLines = 3) {
  const words = text.split(/\s+/);
  const lines = [];
  let cur = "";
  for (const w of words) {
    if (cur && (cur + " " + w).length > maxChars) {
      lines.push(cur);
      cur = w;
    } else {
      cur = cur ? cur + " " + w : w;
    }
    if (lines.length >= maxLines) break;
  }
  if (cur && lines.length < maxLines) lines.push(cur);
  if (!lines.length) lines.push(text.slice(0, maxChars));
  const last = lines[lines.length - 1];
  if (last.length > maxChars) lines[lines.length - 1] = last.slice(0, maxChars - 1) + "…";
  return lines;
}

function dayLabel(off) {
  const d = new Date(); d.setDate(d.getDate() + off);
  if (off === 0) return "TODAY";
  if (off === 1) return "TOMORROW";
  if (off === -1) return "YESTERDAY";
  return d.toLocaleDateString("en-GB", { weekday: "short", day: "numeric", month: "short" }).toUpperCase();
}

function dateStr(off) {
  const d = new Date(); d.setDate(d.getDate() + off);
  return d.toLocaleDateString("en-GB", { day: "2-digit", month: "short", year: "numeric" });
}

// ── cache helpers ────────────────────────────────────────────────────────────

/** Returns true if path doesn't exist or was last modified on a different calendar day. */
async function isStale(filePath) {
  if (!existsSync(filePath)) return true;
  try {
    const { mtimeMs } = await stat(filePath);
    const fileDay  = new Date(mtimeMs).toDateString();
    const todayDay = new Date().toDateString();
    return fileDay !== todayDay;
  } catch {
    return true;
  }
}

// ── state ─────────────────────────────────────────────────────────────────────

async function loadState() {
  try { return JSON.parse(await readFile(STATE_FILE, "utf8")); }
  catch { return { dayOffset: 0 }; }
}

async function saveState(state) {
  await mkdir(CACHE_DIR, { recursive: true });
  await writeFile(STATE_FILE, JSON.stringify(state));
}

async function loadEvents() {
  try { return JSON.parse(await readFile(EVENTS_FILE, "utf8")); }
  catch { return { days: {} }; }
}

function getDayEvents(eventsData, dayOffset) {
  return eventsData.days?.[String(dayOffset)] ?? [];
}

// ── SVG ───────────────────────────────────────────────────────────────────────

function eventSvg(event) {
  const bc = getBarColor(event);
  const tr = `${formatTime(event.dt)}–${formatTime(event.end_dt)}`;
  const lines = wrapText(event.summary, 10, 3);
  const bx = PAD + 3, by = PAD + 3, bw = W - PAD * 2 - 6, bh = 14, br = 4;
  const tx = PAD + 5;
  const title = lines.map((l, i) =>
    `<text x="${tx}" y="${by + bh + 12 + i * 11}" font-family="${FONT}" font-size="9" fill="${TEXT_PRIMARY}">${esc(l)}</text>`
  ).join("\n  ");
  return `<svg width="${W}" height="${H}" xmlns="http://www.w3.org/2000/svg">
  <rect width="${W}" height="${H}" fill="${BG}"/>
  <rect x="${bx}" y="${by}" width="${bw}" height="${bh}" rx="${br}" fill="${bc}"/>
  <text x="${bx + bw / 2}" y="${by + bh - 4}" font-family="${FONT}" font-weight="bold" font-size="8" fill="#000000" text-anchor="middle">${esc(tr)}</text>
  ${title}
</svg>`;
}

function emptySvg() {
  return `<svg width="${W}" height="${H}" xmlns="http://www.w3.org/2000/svg"><rect width="${W}" height="${H}" fill="${BG}"/></svg>`;
}

function noEventsSvg() {
  return `<svg width="${W}" height="${H}" xmlns="http://www.w3.org/2000/svg">
  <rect width="${W}" height="${H}" fill="${BG}"/>
  <text x="${W / 2}" y="${H / 2 + 4}" font-family="${FONT}" font-size="10" fill="${TEXT_SECONDARY}" text-anchor="middle">no events</text>
</svg>`;
}

function headerSvg(dayOffset, count) {
  const label = dayLabel(dayOffset);
  const date = dateStr(dayOffset);
  const bc = dayOffset === 0 ? "#58a6ff" : "#484f58";
  const ls = label.length <= 5 ? 11 : label.length <= 8 ? 9 : 8;
  const bx = 2, by = 2, bw = W - 4, bh = 18, br = 5;
  return `<svg width="${W}" height="${H}" xmlns="http://www.w3.org/2000/svg">
  <rect width="${W}" height="${H}" fill="${BG}"/>
  <rect x="${bx}" y="${by}" width="${bw}" height="${bh}" rx="${br}" fill="${bc}"/>
  <text x="${W / 2}" y="${by + bh - 5}" font-family="${FONT}" font-weight="bold" font-size="${ls}" fill="white" text-anchor="middle">${esc(label)}</text>
  <text x="${W / 2}" y="40" font-family="${FONT}" font-size="10" fill="${TEXT_PRIMARY}" text-anchor="middle">${esc(date)}</text>
  <text x="${W / 2}" y="56" font-family="${FONT}" font-weight="bold" font-size="11" fill="${TEXT_PRIMARY}" text-anchor="middle">${count} event${count !== 1 ? "s" : ""}</text>
</svg>`;
}

// ── render ────────────────────────────────────────────────────────────────────

async function renderAll(state, eventsData) {
  await mkdir(IMG_DIR, { recursive: true });
  const off = state.dayOffset ?? 0;
  const events = getDayEvents(eventsData, off);
  const renders = [];

  renders.push(sharp(Buffer.from(headerSvg(off, events.length))).png().toFile(join(IMG_DIR, "header.png")));

  for (let i = 0; i < SLOTS; i++) {
    let svg;
    if (!events.length && i === 0) svg = noEventsSvg();
    else if (i < events.length) svg = eventSvg(events[i]);
    else svg = emptySvg();
    renders.push(sharp(Buffer.from(svg)).png().toFile(join(IMG_DIR, `key-${i}.png`)));
  }

  await Promise.all(renders);
}

// ── generate nav assets ──────────────────────────────────────────────────────

async function generateAssets() {
  await mkdir(ASSETS_DIR, { recursive: true });
  const assets = {
    "prev.png": `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" rx="8" fill="#0d1829"/>
  <text x="36" y="42" font-family="${FONT}" font-size="28" fill="#58a6ff" text-anchor="middle">◀</text>
  <text x="36" y="62" font-family="${FONT}" font-weight="bold" font-size="10" fill="#4b5563" text-anchor="middle">PREV</text>
</svg>`,
    "next.png": `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" rx="8" fill="#0d1829"/>
  <text x="36" y="42" font-family="${FONT}" font-size="28" fill="#58a6ff" text-anchor="middle">▶</text>
  <text x="36" y="62" font-family="${FONT}" font-weight="bold" font-size="10" fill="#4b5563" text-anchor="middle">NEXT</text>
</svg>`,
    "exit.png": `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" rx="8" fill="#1a0d0d"/>
  <text x="36" y="46" font-family="${FONT}" font-weight="bold" font-size="16" fill="#ef4444" text-anchor="middle">EXIT</text>
</svg>`,
  };
  await Promise.all(
    Object.entries(assets).map(([name, svg]) =>
      sharp(Buffer.from(svg)).png().toFile(join(ASSETS_DIR, name))
    )
  );
}

// ── main ──────────────────────────────────────────────────────────────────────

const [,, arg, arg2] = process.argv;

if (arg === "generate-assets") {
  await generateAssets();

} else if (arg === "header") {
  const path = join(IMG_DIR, "header.png");
  if (await isStale(path)) {
    const state = await loadState();
    const eventsData = await loadEvents();
    await renderAll(state, eventsData);
  }
  console.log(path);

} else if (arg === "page") {
  const state = await loadState();
  state.dayOffset = (state.dayOffset ?? 0) + (arg2 === "prev" ? -1 : 1);
  await saveState(state);
  const eventsData = await loadEvents();
  await renderAll(state, eventsData);

} else if (arg === "open") {
  const slot = parseInt(arg2, 10) || 0;
  const state = await loadState();
  const eventsData = await loadEvents();
  const events = getDayEvents(eventsData, state.dayOffset ?? 0);
  if (slot < events.length) {
    const m = (events[slot].location || "").match(/https?:\/\/\S+/);
    if (m) {
      spawn("xdg-open", [m[0]], {
        detached: true, stdio: "ignore",
        env: { ...process.env, DISPLAY: process.env.DISPLAY || ":1" },
      }).unref();
    }
  }

} else if (arg === "init") {
  const state = await loadState();
  state.dayOffset = 0;
  await saveState(state);
  const eventsData = await loadEvents();
  await renderAll(state, eventsData);

} else {
  // slot <n> or bare <n> — just return cached path
  const raw = arg === "slot" ? arg2 : arg;
  const slot = parseInt(raw, 10);
  if (isNaN(slot)) process.exit(1);
  const path = join(IMG_DIR, `key-${slot}.png`);
  if (await isStale(path)) {
    const state = await loadState();
    const eventsData = await loadEvents();
    await renderAll(state, eventsData);
  }
  console.log(path);
}
