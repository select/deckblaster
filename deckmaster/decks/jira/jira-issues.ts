#!/usr/bin/env bun
/**
 * jira-issues.ts — Stream Deck Jira open-issues plugin
 *
 * Commands:
 *   bun jira-issues.ts badge      → main.deck nav button icon path
 *   bun jira-issues.ts header     → jira.deck key 0 icon path
 *   bun jira-issues.ts icon <n>   → issue card icon for slot n (0-indexed, up to 11)
 *   bun jira-issues.ts url <n>    → open issue n in browser
 *   bun jira-issues.ts page-next  → advance to next page, re-render
 *   bun jira-issues.ts page-icon  → pagination button icon path
 *
 * Polling TTL: 2 minutes (Jira API is slower, no need for fast polling)
 *
 * State/cache : /tmp/streamdeck-jira-issues.json
 * Images      : /tmp/streamdeck-jira/
 * Lock        : /tmp/streamdeck-jira.lock
 */

import sharp from "sharp";
import {
  readFileSync, writeFileSync, statSync,
  openSync, closeSync, unlinkSync, existsSync,
} from "fs";
import { readFile, writeFile, mkdir } from "fs/promises";
import { spawn } from "child_process";

// ── paths ──────────────────────────────────────────────────────────────────────
const CACHE_FILE = "/tmp/streamdeck-jira-issues.json";
const LOCK_FILE  = "/tmp/streamdeck-jira.lock";
const SPAWN_LOCK = LOCK_FILE + ".spawn";
const PAGE_FILE  = "/tmp/streamdeck-jira-page.json";
const OUT_DIR    = "/tmp/streamdeck-jira";

// ── tuning ─────────────────────────────────────────────────────────────────────
const MAX_ISSUE_SLOTS = 12;
const TTL_MS          = 120_000; // 2 minutes

// ── credentials ────────────────────────────────────────────────────────────────
const JIRA_URL      = process.env.JIRA_URL ?? "";
const JIRA_USERNAME = process.env.JIRA_USERNAME ?? "";
const JIRA_API_TOKEN = process.env.JIRA_API_TOKEN ?? "";

function getAuthHeader(): string {
  return `Basic ${Buffer.from(`${JIRA_USERNAME}:${JIRA_API_TOKEN}`).toString("base64")}`;
}

// ── palette ────────────────────────────────────────────────────────────────────
const BG       = "#0d1117";
const CARD_BG  = "#161b22";
const BORDER   = "#30363d";
const C_WHITE  = "#e6edf3";
const C_GRAY   = "#8b949e";
const C_GREEN  = "#3fb950";
const C_RED    = "#f85149";
const C_YELLOW = "#d29922";
const C_BLUE   = "#58a6ff";
const C_PURPLE = "#a371f7";

// Status → color mapping
const STATUS_COLOR: Record<string, string> = {
  "To Do":       C_BLUE,
  "Open":        C_BLUE,
  "Backlog":     C_GRAY,
  "In Progress": C_YELLOW,
  "In Review":   C_PURPLE,
  "ON REVIEW":   C_PURPLE,
  "Blocked":     C_RED,
  "BLOCKED":     C_RED,
  "Done":        C_GREEN,
  "Closed":      C_GREEN,
  "Resolved":    C_GREEN,
};

// Short status labels for cards
const STATUS_LABEL: Record<string, string> = {
  "To Do":       "TODO",
  "Open":        "OPEN",
  "Backlog":     "BKLOG",
  "In Progress": "PROG",
  "In Review":   "REVW",
  "ON REVIEW":   "REVW",
  "Blocked":     "BLKD",
  "BLOCKED":     "BLKD",
  "Done":        "DONE",
  "Closed":      "DONE",
  "Resolved":    "DONE",
};

// Statuses to exclude from display
const EXCLUDED_STATUSES = new Set(["WONT DO", "Wont do", "DUPLICATE", "Won't Do"]);

// Sort order: TODO first, review last
const STATUS_ORDER: Record<string, number> = {
  "To Do":       0,
  "Open":        0,
  "Backlog":     1,
  "Blocked":     2,
  "BLOCKED":     2,
  "In Progress": 3,
  "In Review":   4,
  "ON REVIEW":   4,
};

// Priority → color
const PRIO_COLOR: Record<string, string> = {
  "Highest":  C_RED,
  "High":     C_RED,
  "Medium":   C_YELLOW,
  "Low":      C_GREEN,
  "Lowest":   C_GREEN,
};

// ── types ──────────────────────────────────────────────────────────────────────
interface JiraIssue {
  key: string;
  title: string;
  status: string;
  priority: string;
  type: string;
  assignee: string;
  url: string;
  board: string; // "EN" or "AW"
  updated: string;
}

interface CacheData {
  ts: number;
  issues: JiraIssue[];
}

// ── svg helpers ────────────────────────────────────────────────────────────────
function esc(s: string): string {
  return s
    .replace(/&/g, "&amp;").replace(/</g, "&lt;")
    .replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

function wrapText(text: string, maxChars: number, maxLines: number): string[] {
  const words = text.split(/\s+/);
  const lines: string[] = [];
  let cur = "";
  for (const w of words) {
    if (cur && (cur + " " + w).length > maxChars) {
      lines.push(cur);
      cur = w;
      if (lines.length >= maxLines) break;
    } else {
      cur = cur ? cur + " " + w : w;
    }
  }
  if (cur && lines.length < maxLines) lines.push(cur);
  return lines.slice(0, maxLines);
}

// ── Jira API ───────────────────────────────────────────────────────────────────
/** Render a red error icon with a message — shown on header/badge when auth fails */
function makeErrorSvg(msg: string): string {
  const lines = wrapText(msg, 12, 3);
  const titleSvg = lines.map((l, i) =>
    `<text x="36" y="${46 + i * 10}" font-family="DejaVu Sans,sans-serif" font-size="8" fill="${C_WHITE}" text-anchor="middle">${esc(l)}</text>`
  ).join("\n  ");
  return `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" rx="6" fill="${BG}"/>
  <rect width="72" height="72" rx="6" fill="${C_RED}" opacity="0.2"/>
  <text x="36" y="22" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="18" fill="${C_RED}" text-anchor="middle">⚠</text>
  <text x="36" y="36" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="8" fill="${C_RED}" text-anchor="middle">JIRA ERROR</text>
  ${titleSvg}
</svg>`;
}

let lastError: string | null = null;

async function fetchFromJira(): Promise<JiraIssue[] | null> {
  if (!JIRA_URL || !JIRA_USERNAME || !JIRA_API_TOKEN) {
    lastError = "No credentials";
    console.error("Missing JIRA credentials (JIRA_URL, JIRA_USERNAME, JIRA_API_TOKEN)");
    return null;
  }

  // Fetch issues assigned to me from EN and AW boards, not Done
  const jql = `assignee = currentUser() AND project in (EN, AW) AND sprint in openSprints() AND status NOT IN (Done, Closed, Resolved, "WONT DO", "Wont do", DUPLICATE) AND issuetype != Epic ORDER BY updated DESC`;

  try {
    const url = `${JIRA_URL}/rest/api/3/search/jql?jql=${encodeURIComponent(jql)}&maxResults=50&fields=summary,status,priority,issuetype,assignee,updated`;
    const response = await fetch(url, {
      headers: {
        Authorization: getAuthHeader(),
        Accept: "application/json",
      },
    });

    if (!response.ok) {
      if (response.status === 401 || response.status === 403) {
        lastError = "Token expired";
      } else {
        lastError = `HTTP ${response.status}`;
      }
      console.error(`Jira API error: ${response.status} ${response.statusText}`);
      return null;
    }
    lastError = null;

    const data = await response.json() as {
      issues: Array<{
        key: string;
        fields: {
          summary: string;
          status: { name: string };
          priority?: { name: string };
          issuetype: { name: string };
          assignee?: { displayName: string };
          updated: string;
        };
      }>;
    };

    return data.issues
      .map(issue => ({
        key: issue.key,
        title: issue.fields.summary,
        status: issue.fields.status.name,
        priority: issue.fields.priority?.name ?? "Medium",
        type: issue.fields.issuetype.name,
        assignee: issue.fields.assignee?.displayName ?? "",
        url: `${JIRA_URL}/browse/${issue.key}`,
        board: issue.key.split("-")[0],
        updated: issue.fields.updated,
      }))
      .filter(i => !EXCLUDED_STATUSES.has(i.status))
      .sort((a, b) => {
        const oa = STATUS_ORDER[a.status] ?? 5;
        const ob = STATUS_ORDER[b.status] ?? 5;
        return oa - ob;
      });
  } catch (e) {
    lastError = "Network error";
    console.error("Error fetching from Jira:", e);
    return null;
  }
}

// ── lock ───────────────────────────────────────────────────────────────────────
async function withLock<T>(fn: () => Promise<T>): Promise<T> {
  const MAX_RETRIES = 30, RETRY_MS = 200;
  let fd = -1;
  for (let i = 0; i < MAX_RETRIES; i++) {
    try {
      fd = openSync(LOCK_FILE, "wx");
      break;
    } catch {
      try {
        if (Date.now() - statSync(LOCK_FILE).mtimeMs > 30_000)
          unlinkSync(LOCK_FILE);
      } catch {}
      await new Promise(r => setTimeout(r, RETRY_MS));
    }
  }
  try { return await fn(); }
  finally {
    if (fd >= 0) try { closeSync(fd); } catch {}
    try { unlinkSync(LOCK_FILE); } catch {}
  }
}

// ── cache + fetch ──────────────────────────────────────────────────────────────
async function getIssues(): Promise<JiraIssue[]> {
  return withLock(async () => {
    const now = Date.now();
    try {
      const cached: CacheData = JSON.parse(await readFile(CACHE_FILE, "utf8"));
      let ts = cached.ts ?? 0;
      if (ts < 1e12) ts *= 1000;
      if (now - ts < TTL_MS) return cached.issues;
    } catch {}

    const issues = await fetchFromJira();
    if (issues === null) {
      // Render error icons so the deck shows the problem
      if (lastError) await renderError(lastError);
      try { return (JSON.parse(await readFile(CACHE_FILE, "utf8")) as CacheData).issues; }
      catch { return []; }
    }
    await writeFile(CACHE_FILE, JSON.stringify({ ts: now, issues }));
    await renderAll(issues);
    return issues;
  });
}

// ── svg renderers ──────────────────────────────────────────────────────────────

/** Issue card — similar to GitHub PR card style with calendar-style header */
function makeIssueSvg(issue: JiraIssue, slotIdx: number): string {
  const PAD = 2, R = 6;
  const cx = PAD, cy = PAD, cw = 72 - PAD * 2, ch = 72 - PAD * 2;
  const bx = cx + 3, by = cy + 3, bh = 16, bw = cw - 6, br = 4;

  const statusCol = STATUS_COLOR[issue.status] ?? C_GRAY;
  const statusLbl = STATUS_LABEL[issue.status] ?? issue.status.slice(0, 4).toUpperCase();
  const prioCol   = PRIO_COLOR[issue.priority] ?? C_YELLOW;

  // Header bar with status color
  const barSvg = `<rect x="${bx}" y="${by}" width="${bw}" height="${bh}" rx="${br}" fill="${statusCol}"/>`;

  // Board + key in header
  const keyStr     = issue.key;
  const boardStr   = issue.board;
  const barTextY   = by + bh - 5;

  // Title: vertically centred between header and status bar
  const lines      = wrapText(issue.title, 13, 3);
  const contentTop = by + bh + 2;
  const contentBot = 72 - 16;
  const textH      = lines.length * 10;
  const firstLineY = contentTop + Math.floor((contentBot - contentTop - textH) / 2) + 7;
  const titleSvg   = lines.map((l, i) =>
    `<text x="${cx + 4}" y="${firstLineY + i * 10}" font-family="DejaVu Sans,sans-serif" font-size="8" fill="${C_WHITE}">${esc(l)}</text>`
  ).join("\n  ");

  // Status bar (bottom 14 px)
  const sbY  = 72 - 16;
  const sbTY = sbY + 10;

  // Priority dot + status label
  const prioDot = `<circle cx="${cx + 8}" cy="${sbY + 7}" r="3" fill="${prioCol}"/>`;
  const statusSvg = `<text x="${72 - cx - 4}" y="${sbTY}" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="7" fill="${statusCol}" text-anchor="end">${statusLbl}</text>`;

  // Type indicator (left of status bar)
  const typeChar = issue.type === "Bug" ? "🐛" : issue.type === "Story" ? "📖" : issue.type === "Task" ? "✓" : "•";
  const typeSvg = `<text x="${cx + 15}" y="${sbTY}" font-family="DejaVu Sans,sans-serif" font-size="7" fill="${C_GRAY}">${esc(issue.type.slice(0, 4))}</text>`;

  return `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" fill="${BG}"/>
  <rect x="${cx}" y="${cy}" width="${cw}" height="${ch}" rx="${R}" fill="${CARD_BG}"/>
  ${barSvg}
  <text x="${bx + 4}" y="${barTextY}" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="8" fill="#000000">${esc(boardStr)}</text>
  <text x="${bx + bw - 3}" y="${barTextY}" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="8" fill="#000000" text-anchor="end">${esc(keyStr.split("-")[1])}</text>
  ${titleSvg}
  <rect x="${cx + 1}" y="${sbY}" width="${cw - 2}" height="14" rx="3" fill="${BORDER}"/>
  ${prioDot}
  ${typeSvg}
  ${statusSvg}
</svg>`;
}

function makeEmptySvg(): string {
  const PAD = 2, R = 6;
  return `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" fill="${BG}"/>
  <rect x="${PAD}" y="${PAD}" width="${72 - PAD * 2}" height="${72 - PAD * 2}" rx="${R}" fill="${CARD_BG}"/>
</svg>`;
}

/** Header card (key 0 in jira.deck) — Jira logo + counts */
function makeHeaderSvg(issues: JiraIssue[]): string {
  const n       = issues.length;
  const nBlocked = issues.filter(i => i.status === "Blocked").length;
  const nProg    = issues.filter(i => i.status === "In Progress" || i.status === "In Review").length;
  const nOpen    = issues.filter(i => i.status === "To Do" || i.status === "Open" || i.status === "Backlog").length;
  const hasAlert = nBlocked > 0;

  const glow = hasAlert
    ? `<rect width="72" height="72" rx="6" fill="${C_RED}" opacity="0.15"/>`
    : nProg > 0
    ? `<rect width="72" height="72" rx="6" fill="${C_YELLOW}" opacity="0.12"/>`
    : "";

  const cntColor = hasAlert ? C_RED : nProg > 0 ? C_YELLOW : n > 0 ? C_BLUE : C_GRAY;

  const parts: string[] = [];
  if (nBlocked) parts.push(`${nBlocked} blkd`);
  if (nProg)    parts.push(`${nProg} prog`);
  if (nOpen)    parts.push(`${nOpen} open`);
  const status      = parts.join(" · ") || (n > 0 ? "all good" : "none");
  const statusColor = hasAlert ? C_RED : nProg > 0 ? C_YELLOW : C_BLUE;

  // Jira diamond logo (simplified)
  const jiraLogo = `<g transform="translate(22,2) scale(0.22)">
    <path d="M108.023 16H61.805c0 11.52 9.324 20.848 20.847 20.848h8.5v8.226c0 11.52 9.328 20.848 20.848 20.848V19.977A3.98 3.98 0 00108.023 16z" fill="#2684ff"/>
    <path d="M85.121 39.04H38.902c0 11.519 9.325 20.847 20.844 20.847h8.504v8.226c0 11.52 9.328 20.848 20.848 20.848V43.016a3.983 3.983 0 00-3.977-3.977z" fill="#2684ff"/>
    <path d="M62.219 62.078H16c0 11.524 9.324 20.848 20.848 20.848h8.5v8.23c0 11.52 9.328 20.844 20.847 20.844V66.059a3.984 3.984 0 00-3.976-3.98z" fill="#2684ff"/>
  </g>`;

  return `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" fill="${BG}"/>
  ${glow}
  ${jiraLogo}
  <text x="36" y="46" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="13" fill="${cntColor}" text-anchor="middle">${n}</text>
  <text x="36" y="57" font-family="DejaVu Sans,sans-serif" font-size="7" fill="${C_GRAY}" text-anchor="middle">issues</text>
  ${status ? `<text x="36" y="68" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="7" fill="${statusColor}" text-anchor="middle">${esc(status)}</text>` : ""}
</svg>`;
}

/** Badge for main.deck nav button — 4 corner badges:
 *   Top-left:     open/todo (blue)
 *   Top-right:    blocked (red)
 *   Bottom-left:  in progress (yellow)
 *   Bottom-right: done today (green) — not shown since we filter done out; use total instead
 */
function makeBadgeSvg(issues: JiraIssue[]): string {
  const nOpen    = issues.filter(i => i.status === "To Do" || i.status === "Open" || i.status === "Backlog").length;
  const nBlocked = issues.filter(i => i.status === "Blocked").length;
  const nProg    = issues.filter(i => i.status === "In Progress" || i.status === "In Review").length;
  const nTotal   = issues.length;

  function badge(cx: number, cy: number, count: number, color: string): string {
    if (count === 0) return `<circle cx="${cx}" cy="${cy}" r="9" fill="${BORDER}" opacity="0.5"/>`;
    const fs = count >= 10 ? 9 : 12;
    const ty = cy + (fs === 9 ? 3 : 4);
    return `<circle cx="${cx}" cy="${cy}" r="9" fill="${color}"/>
  <text x="${cx}" y="${ty}" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="${fs}" fill="white" text-anchor="middle">${count}</text>`;
  }

  const tl = badge(12, 12, nOpen, C_BLUE);       // open
  const tr = badge(60, 12, nBlocked, C_RED);      // blocked
  const bl = badge(12, 60, nProg, C_YELLOW);      // in progress
  const br = badge(60, 60, nTotal, C_GREEN);      // total

  // Jira diamond mark centred
  const jiraLogo = `<g transform="translate(22,22) scale(0.22)">
    <path d="M108.023 16H61.805c0 11.52 9.324 20.848 20.847 20.848h8.5v8.226c0 11.52 9.328 20.848 20.848 20.848V19.977A3.98 3.98 0 00108.023 16z" fill="#2684ff"/>
    <path d="M85.121 39.04H38.902c0 11.519 9.325 20.847 20.844 20.847h8.504v8.226c0 11.52 9.328 20.848 20.848 20.848V43.016a3.983 3.983 0 00-3.977-3.977z" fill="#2684ff"/>
    <path d="M62.219 62.078H16c0 11.524 9.324 20.848 20.848 20.848h8.5v8.23c0 11.52 9.328 20.844 20.847 20.844V66.059a3.984 3.984 0 00-3.976-3.98z" fill="#2684ff"/>
  </g>`;

  return `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" rx="10" fill="${BG}"/>
  ${jiraLogo}
  ${tl}
  ${tr}
  ${bl}
  ${br}
</svg>`;
}

// ── pagination ─────────────────────────────────────────────────────────────────
function getPage(): number {
  try { return JSON.parse(readFileSync(PAGE_FILE, "utf8")).page ?? 0; }
  catch { return 0; }
}
function setPage(page: number): void {
  writeFileSync(PAGE_FILE, JSON.stringify({ page }));
}
function getPageIssues(issues: JiraIssue[], page: number): JiraIssue[] {
  const start = page * MAX_ISSUE_SLOTS;
  return issues.slice(start, start + MAX_ISSUE_SLOTS);
}
function getTotalPages(issues: JiraIssue[]): number {
  return Math.max(1, Math.ceil(issues.length / MAX_ISSUE_SLOTS));
}

/** Pagination button icon — shows "page/total" with arrow */
function makePageSvg(page: number, totalPages: number): string {
  const label = `${page + 1}/${totalPages}`;
  const hasNext = page < totalPages - 1;
  const arrowCol = hasNext ? C_WHITE : C_GRAY;
  return `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" rx="10" fill="${BG}"/>
  <text x="36" y="30" font-family="DejaVu Sans,sans-serif" font-size="8" fill="${C_GRAY}" text-anchor="middle">PAGE</text>
  <text x="36" y="48" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="14" fill="${C_WHITE}" text-anchor="middle">${label}</text>
  <polygon points="28,58 36,66 44,58" fill="${arrowCol}"/>
</svg>`;
}

// ── render all slots (page-aware) ──────────────────────────────────────────────
async function renderAll(issues: JiraIssue[]): Promise<void> {
  await mkdir(OUT_DIR, { recursive: true });
  const page = getPage();
  const pageIssues = getPageIssues(issues, page);
  const totalPages = getTotalPages(issues);
  const jobs: Promise<unknown>[] = [];
  for (let i = 0; i < MAX_ISSUE_SLOTS; i++) {
    const svg = i < pageIssues.length ? makeIssueSvg(pageIssues[i], i) : makeEmptySvg();
    jobs.push(sharp(Buffer.from(svg)).png().toFile(`${OUT_DIR}/issue-${i}.png`));
  }
  jobs.push(sharp(Buffer.from(makeHeaderSvg(issues))).png().toFile(`${OUT_DIR}/header.png`));
  jobs.push(sharp(Buffer.from(makeBadgeSvg(issues))).png().toFile(`${OUT_DIR}/badge.png`));
  jobs.push(sharp(Buffer.from(makePageSvg(page, totalPages))).png().toFile(`${OUT_DIR}/page.png`));
  await Promise.all(jobs);
}

/** Render error state — header+badge show error, issue slots show empty */
async function renderError(msg: string): Promise<void> {
  await mkdir(OUT_DIR, { recursive: true });
  const errSvg = makeErrorSvg(msg);
  const jobs: Promise<unknown>[] = [];
  jobs.push(sharp(Buffer.from(errSvg)).png().toFile(`${OUT_DIR}/header.png`));
  jobs.push(sharp(Buffer.from(errSvg)).png().toFile(`${OUT_DIR}/badge.png`));
  for (let i = 0; i < MAX_ISSUE_SLOTS; i++) {
    jobs.push(sharp(Buffer.from(makeEmptySvg())).png().toFile(`${OUT_DIR}/issue-${i}.png`));
  }
  await Promise.all(jobs);
}

// ── main ───────────────────────────────────────────────────────────────────────
const [,, cmd, arg] = process.argv;

if (cmd === "update-cache-bg") {
  try {
    await withLock(async () => {
      const issues = await fetchFromJira();
      if (issues !== null) {
        await writeFile(CACHE_FILE, JSON.stringify({ ts: Date.now(), issues }));
        await renderAll(issues);
        
        // Trigger immediate deck reload so cards update without waiting for poll
        try { await fetch("http://localhost:9990/reload", { method: "POST" }); } catch {}
      } else if (lastError) {
        await renderError(lastError);
      }
    });
  } finally {
    try { unlinkSync(SPAWN_LOCK); } catch {}
  }
  process.exit(0);
}

if (cmd === "badge" || cmd === "header" || cmd === "icon" || cmd === "page-icon") {
  let path = "";
  if (cmd === "badge") {
    path = `${OUT_DIR}/badge.png`;
  } else if (cmd === "header") {
    path = `${OUT_DIR}/header.png`;
  } else if (cmd === "page-icon") {
    path = `${OUT_DIR}/page.png`;
  } else {
    const n = parseInt(arg, 10);
    path = `${OUT_DIR}/issue-${n}.png`;
  }

  // Print the path to stdout immediately so deckmaster gets it without delay
  console.log(path);

  // Check if cache has expired or if the image doesn't exist
  let shouldFetch = false;
  if (!existsSync(path)) {
    shouldFetch = true;
  } else {
    try {
      const cached = JSON.parse(readFileSync(CACHE_FILE, "utf8"));
      let ts = cached.ts ?? 0;
      if (ts < 1e12) ts *= 1000;
      if (Date.now() - ts >= TTL_MS) {
        shouldFetch = true;
      }
    } catch {
      shouldFetch = true;
    }
  }

  if (shouldFetch) {
    // Spawn background process to fetch and update everything if not already locked
    if (!existsSync(SPAWN_LOCK) && !existsSync(LOCK_FILE)) {
      try { closeSync(openSync(SPAWN_LOCK, "wx")); } catch {}
      spawn("bun", [process.argv[1], "update-cache-bg"], {
        detached: true,
        stdio: "ignore",
      }).unref();
    }
  }
  process.exit(0);
}

let issues: JiraIssue[];
try { issues = await getIssues(); }
catch (e) { console.error("error fetching issues:", e); issues = []; }

let cacheTs = 0;
try {
  let ts = (JSON.parse(readFileSync(CACHE_FILE, "utf8")) as CacheData).ts ?? 0;
  if (ts < 1e12) ts *= 1000;
  cacheTs = ts;
} catch {}

await mkdir(OUT_DIR, { recursive: true });

if (cmd === "url") {
  const n = parseInt(arg, 10);
  const page = getPage();
  try {
    const cached: CacheData = JSON.parse(readFileSync(CACHE_FILE, "utf8"));
    const pageIssues = getPageIssues(cached.issues, page);
    const url = pageIssues?.[n]?.url;
    if (url) {
      spawn("xdg-open", [url], {
        detached: true, stdio: "ignore",
        env: { ...process.env, DISPLAY: process.env.DISPLAY || ":1" },
      }).unref();
    }
  } catch (e) {
    console.error("error opening issue:", e);
    process.exit(1);
  }

} else if (cmd === "page-next") {
  const totalPages = getTotalPages(issues);
  const curPage = getPage();
  const nextPage = (curPage + 1) % totalPages; // wrap around
  setPage(nextPage);
  await renderAll(issues);
  // Trigger immediate deck reload so cards update without waiting for poll
  try { await fetch("http://localhost:9990/reload", { method: "POST" }); } catch {}
  console.log(`${OUT_DIR}/page.png`);

} else if (cmd === "page-icon") {
  const path = `${OUT_DIR}/page.png`;
  if (!existsSync(path) || statSync(path).mtimeMs < cacheTs) {
    const page = getPage();
    const totalPages = getTotalPages(issues);
    await sharp(Buffer.from(makePageSvg(page, totalPages))).png().toFile(path);
  }
  console.log(path);

} else {
  console.error("unknown command:", cmd);
  process.exit(1);
}
