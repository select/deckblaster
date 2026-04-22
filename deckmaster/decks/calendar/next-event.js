#!/usr/bin/env bun
/**
 * next-event.js — Stream Deck main-deck calendar widget
 *
 * Reads from the fetcher daemon's JSON, renders a single SVG→PNG icon
 * showing the next calendar event with colour-coded countdown badge.
 *
 * Usage:
 *   bun next-event.js icon   → render icon, print PNG path
 *   bun next-event.js open   → open meeting URL in browser
 *
 * Events: ~/.local/share/deckblaster/calendar-day-events.json (from fetcher)
 * Image:  /tmp/streamdeck-calendar/calendar.png
 * Alerts: /tmp/streamdeck-next-event-alerts.json
 */

import sharp from "sharp";
import { mkdir, writeFile, readFile } from "fs/promises";
import { existsSync } from "fs";
import { spawn } from "child_process";
import { join, dirname } from "path";
import { homedir } from "os";

// ── config ────────────────────────────────────────────────────────────────────

const EVENTS_FILE = join(homedir(), ".local", "share", "deckblaster", "calendar-day-events.json");
const IMG_DIR     = "/tmp/streamdeck-calendar";
const IMG_PATH    = join(IMG_DIR, "calendar.png");
const ALERT_FILE  = "/tmp/streamdeck-next-event-alerts.json";
const API_KEY     = 9;
const API_URL     = `http://localhost:9990/key/${API_KEY}`;
const DECK_ROOT   = join(dirname(import.meta.path), "..", "..");

// ── design tokens ─────────────────────────────────────────────────────────────

const W = 72, H = 72, PAD = 2;
const FONT = "DejaVu Sans,sans-serif";
const BG           = "#000000";
const COLOR_NOW    = "#f85149";
const COLOR_20MIN  = "#f0883e";
const COLOR_1HR    = "#d29922";
const COLOR_4HR    = "#58a6ff";
const COLOR_FAR    = "#3fb950";
const COLOR_PAST   = "#484f58";
const TEXT_PRIMARY = "#e6edf3";
const TEXT_SECONDARY = "#8b949e";

// alert thresholds: seconds → { duration, label, bg }
const ALERTS = {
  180: { duration: "5s", label: "3 MIN", bg: "#cc0000" },
  120: { duration: "5s", label: "2 MIN", bg: "#cc0000" },
  60:  { duration: "5s", label: "1 MIN", bg: "#cc0000" },
};

// ── helpers ───────────────────────────────────────────────────────────────────

function esc(s) {
  return String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function formatCountdown(ms) {
  const min = Math.floor(ms / 60000);
  if (min < 1) return "NOW";
  const h = Math.floor(min / 60);
  if (h >= 24) return `${Math.floor(h / 24)}D`;
  if (h > 0) return `${h}H`;
  return `${min}MIN`;
}

function formatTime(iso) {
  return new Date(iso).toLocaleTimeString("de-DE", { hour: "2-digit", minute: "2-digit", hour12: false });
}

function wrapText(text, maxChars = 10, maxLines = 3) {
  const words = text.split(/\s+/);
  const lines = [];
  let cur = "";
  for (const w of words) {
    if (cur && (cur + " " + w).length > maxChars) {
      lines.push(cur); cur = w;
    } else { cur = cur ? cur + " " + w : w; }
    if (lines.length >= maxLines) break;
  }
  if (cur && lines.length < maxLines) lines.push(cur);
  if (!lines.length) lines.push(text.slice(0, maxChars));
  const last = lines[lines.length - 1];
  if (last.length > maxChars) lines[lines.length - 1] = last.slice(0, maxChars - 1) + "…";
  return lines;
}

function getBarColor(evt) {
  const now = Date.now();
  const start = new Date(evt.dt).getTime();
  const end = new Date(evt.end_dt).getTime();
  if (end <= now) return COLOR_PAST;
  if (start <= now && now < end) return COLOR_NOW;
  const min = (start - now) / 60000;
  if (min <= 20) return COLOR_20MIN;
  if (min <= 60) return COLOR_1HR;
  if (min <= 240) return COLOR_4HR;
  return COLOR_FAR;
}

// ── find next event from fetcher JSON ─────────────────────────────────────────

async function getNextEvent() {
  let fdata;
  try { fdata = JSON.parse(await readFile(EVENTS_FILE, "utf8")); }
  catch { return null; }

  const now = Date.now();
  for (let offset = 0; offset < 7; offset++) {
    const events = fdata.days?.[String(offset)] ?? [];
    for (const e of events) {
      const start = new Date(e.dt).getTime();
      const end = new Date(e.end_dt).getTime();
      if (start <= now && now < end) {
        return { ...e, in_progress: true };
      }
      if (start > now) {
        return { ...e, in_progress: false };
      }
    }
  }
  return null;
}

// ── SVG ───────────────────────────────────────────────────────────────────────

function renderSvg(evt) {
  if (!evt) {
    return `<svg width="${W}" height="${H}" xmlns="http://www.w3.org/2000/svg">
  <rect width="${W}" height="${H}" fill="${BG}"/>
  <text x="${W / 2}" y="${H / 2 + 2}" font-family="${FONT}" font-size="10" fill="${TEXT_SECONDARY}" text-anchor="middle">no events</text>
</svg>`;
  }

  const now = Date.now();
  const start = new Date(evt.dt).getTime();
  const end = new Date(evt.end_dt).getTime();
  const bc = getBarColor(evt);

  let countdownText;
  if (evt.in_progress) {
    const remaining = end - now;
    countdownText = remaining < 60000 ? "NOW" : formatCountdown(remaining);
  } else {
    const delta = start - now;
    countdownText = delta < 60000 ? "NOW" : `IN ${formatCountdown(delta)}`;
  }

  const timeRange = `${formatTime(evt.dt)} – ${formatTime(evt.end_dt)}`;
  const lines = wrapText(evt.summary, 10, 3);

  const bx = PAD + 3, by = PAD + 3, bw = W - PAD * 2 - 6, bh = 14, br = 4;
  const tx = PAD + 5;

  const titleSvg = lines.map((l, i) =>
    `<text x="${tx}" y="${by + bh + 23 + i * 11}" font-family="${FONT}" font-size="9" fill="${TEXT_PRIMARY}">${esc(l)}</text>`
  ).join("\n  ");

  return `<svg width="${W}" height="${H}" xmlns="http://www.w3.org/2000/svg">
  <rect width="${W}" height="${H}" fill="${BG}"/>
  <rect x="${bx}" y="${by}" width="${bw}" height="${bh}" rx="${br}" fill="${bc}"/>
  <text x="${bx + bw / 2}" y="${by + bh - 4}" font-family="${FONT}" font-weight="bold" font-size="9" fill="#000000" text-anchor="middle">${esc(countdownText)}</text>
  <text x="${tx}" y="${by + bh + 11}" font-family="${FONT}" font-size="9" fill="${TEXT_SECONDARY}">${esc(timeRange)}</text>
  ${titleSvg}
</svg>`;
}

// ── alerts ────────────────────────────────────────────────────────────────────

async function loadAlertState() {
  try { return JSON.parse(await readFile(ALERT_FILE, "utf8")); }
  catch { return {}; }
}

async function saveAlertState(state) {
  await writeFile(ALERT_FILE, JSON.stringify(state));
}

async function pushAlert(label, duration, bg) {
  try {
    await fetch(API_URL, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ label, background: bg, fontsize: 18, duration }),
    });
  } catch {}
}

async function checkAlerts(evt) {
  if (!evt || evt.in_progress) return;
  const remaining = (new Date(evt.dt).getTime() - Date.now()) / 1000;
  if (remaining < 0 || remaining > 200) return;

  const state = await loadAlertState();
  const dtKey = evt.dt;
  if (state.dt !== dtKey) { state.dt = dtKey; state.fired = []; }
  const fired = state.fired || [];
  let changed = false;

  for (const [thresh, { duration, label, bg }] of Object.entries(ALERTS)) {
    const t = Number(thresh);
    if (!fired.includes(t) && remaining > 0 && remaining <= t) {
      await pushAlert(label, duration, bg);
      fired.push(t);
      changed = true;
    }
  }
  if (changed) { state.fired = fired; await saveAlertState(state); }
}

// ── main ──────────────────────────────────────────────────────────────────────

const [,, mode] = process.argv;

if (mode === "open") {
  const evt = await getNextEvent();
  if (evt) {
    const m = (evt.location || "").match(/https?:\/\/\S+/);
    if (m) {
      spawn("xdg-open", [m[0]], {
        detached: true, stdio: "ignore",
        env: { ...process.env, DISPLAY: process.env.DISPLAY || ":1" },
      }).unref();
    }
  }
} else {
  // icon mode (default)
  const evt = await getNextEvent();
  await checkAlerts(evt);

  const svg = renderSvg(evt);
  await mkdir(IMG_DIR, { recursive: true });
  await sharp(Buffer.from(svg)).png().toFile(IMG_PATH);
  console.log(IMG_PATH);
}
