#!/usr/bin/env bun
/**
 * github-prs.js — Stream Deck GitHub open-PR plugin
 *
 * Commands:
 *   bun github-prs.js badge      → main.deck nav button icon path
 *   bun github-prs.js header     → github.deck key 0 icon path
 *   bun github-prs.js icon <n>   → PR card icon for slot n (0-indexed, up to 11)
 *   bun github-prs.js url <n>    → open PR n in browser (snooze polling)
 *
 * Smart-polling TTL:
 *   CI running (any PR)         → 30 s
 *   No running CI + snoozed     → 60 s for 1 h after last press
 *   No running CI, not snoozed  → 5 min
 *
 * State/cache : /tmp/streamdeck-github-prs.json
 * CI history  : /tmp/streamdeck-github-ci-history.json
 * Images      : /tmp/streamdeck-github/
 */

import sharp from "sharp";
import {
  readFileSync, writeFileSync, statSync,
  openSync, closeSync, unlinkSync, existsSync,
} from "fs";
import { readFile, writeFile, mkdir } from "fs/promises";
import { spawnSync, spawn } from "child_process";

// ── paths ──────────────────────────────────────────────────────────────────────
const CACHE_FILE      = "/tmp/streamdeck-github-prs.json";
const LOCK_FILE       = "/tmp/streamdeck-github.lock";
const SNOOZE_FILE     = "/tmp/streamdeck-github-snoozed";
const CI_HISTORY_FILE = "/tmp/streamdeck-github-ci-history.json";
const OUT_DIR         = "/tmp/streamdeck-github";

// ── tuning ─────────────────────────────────────────────────────────────────────
const MAX_PR_SLOTS    = 12;
const TTL_FAST        = 30_000;       // ms — any PR has running CI
const TTL_SLOW        = 60_000;       // ms — 1 min when all CIs resolved
const TTL_SNOOZED     = 60_000;       // ms — 1 min after a PR button press
const SNOOZE_DURATION = 3_600_000;    // ms — snooze lasts 1 h
const CI_HISTORY_TTL  = 86_400_000;   // ms — re-sample CI avg once per day

// ── palette ────────────────────────────────────────────────────────────────────
const BG      = "#0d1117";
const CARD_BG = "#161b22";
const BORDER  = "#30363d";
const C_WHITE = "#e6edf3";
const C_GRAY  = "#8b949e";
const C_GREEN = "#3fb950";
const C_RED   = "#f85149";
const C_ORANGE= "#d18a2a";
const C_YELLOW= "#d29922";

const CI_COLOR = { success: C_GREEN, failure: C_RED,    pending: C_YELLOW, none: C_GRAY };
const CI_LABEL = { success: "CI+",   failure: "CI!",    pending: "CI~",    none: ""     };
const RV_COLOR = {
  approved: C_GREEN, changes_requested: C_RED,
  waiting:  C_YELLOW, draft: C_GRAY, none: C_GRAY,
};
const RV_LABEL = {
  approved: "APR", changes_requested: "REQ",
  waiting:  "WAIT", draft: "DRFT", none: "",
};

// ── svg helpers ────────────────────────────────────────────────────────────────
function esc(s) {
  return String(s)
    .replace(/&/g, "&amp;").replace(/</g, "&lt;")
    .replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

/** Wrap text into at most maxLines lines of maxChars characters. */
function wrapText(text, maxChars, maxLines) {
  const words = text.split(/\s+/);
  const lines = [];
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

// ── github graphql ─────────────────────────────────────────────────────────────
const GH_QUERY = `{
  search(query: "is:pr is:open author:@me", type: ISSUE, first: 12) {
    nodes {
      ... on PullRequest {
        number title isDraft url
        repository { nameWithOwner isArchived }
        reviewDecision
        comments { totalCount }
        commits(last: 1) {
          nodes {
            commit {
              statusCheckRollup {
                state
                contexts(last: 30) {
                  nodes { ... on CheckRun { status startedAt } }
                }
              }
            }
          }
        }
      }
    }
  }
}`;

const CI_STATE_MAP = {
  success: "success", failure: "failure", error: "failure",
  pending: "pending", expected: "pending",
};
const RV_MAP = {
  APPROVED: "approved", CHANGES_REQUESTED: "changes_requested",
  REVIEW_REQUIRED: "waiting",
};

function fetchFromGitHub() {
  try {
    const r = spawnSync("gh", ["api", "graphql", "-f", `query=${GH_QUERY}`],
      { encoding: "utf8", timeout: 15_000 });
    if (r.status !== 0) return null;
    const nodes = JSON.parse(r.stdout)?.data?.search?.nodes ?? [];
    return nodes
      .filter(n => n && !n.repository?.isArchived)
      .map(n => {
        const commits  = n.commits?.nodes ?? [];
        let ci = "none", ciStartedAt = null;
        const rollup = commits[0]?.commit?.statusCheckRollup;
        if (rollup) {
          ci = CI_STATE_MAP[rollup.state?.toLowerCase()] ?? "none";
          const running = (rollup.contexts?.nodes ?? [])
            .filter(c => c?.status === "IN_PROGRESS" && c?.startedAt)
            .map(c => c.startedAt).sort();
          if (running.length) {
            ciStartedAt = running[0];
            // Still have running checks → treat as pending, not failure
            if (ci === "failure") ci = "pending";
          }
        }
        return {
          number:         n.number,
          title:          n.title ?? "",
          isDraft:        n.isDraft ?? false,
          url:            n.url ?? "",
          repo:           n.repository?.nameWithOwner ?? "",
          reviewDecision: RV_MAP[n.reviewDecision ?? ""] ?? "none",
          comments:       n.comments?.totalCount ?? 0,
          ciState:        ci,
          ciStartedAt,
        };
      });
  } catch { return null; }
}

// ── ci avg history ─────────────────────────────────────────────────────────────
function loadCiHistory() {
  try { return JSON.parse(readFileSync(CI_HISTORY_FILE, "utf8")); }
  catch { return {}; }
}

function fetchRepoCiAvg(repo) {
  const [owner, name] = repo.split("/", 2);
  const query = `{
  repository(owner: "${owner}", name: "${name}") {
    pullRequests(last: 20, states: MERGED, orderBy: {field: UPDATED_AT, direction: DESC}) {
      nodes {
        commits(last: 1) {
          nodes {
            commit {
              statusCheckRollup {
                contexts(last: 30) {
                  nodes {
                    ... on CheckRun { startedAt completedAt conclusion }
                  }
                }
              }
            }
          }
        }
      }
    }
  }
}`;
  try {
    const r = spawnSync("gh", ["api", "graphql", "-f", `query=${query}`],
      { encoding: "utf8", timeout: 15_000 });
    if (r.status !== 0) return null;
    const prNodes = JSON.parse(r.stdout)?.data?.repository?.pullRequests?.nodes ?? [];
    const durations = [];
    for (const pr of prNodes) {
      const contexts = pr?.commits?.nodes?.[0]?.commit?.statusCheckRollup?.contexts?.nodes ?? [];
      const starts = [], ends = [];
      for (const c of contexts) {
        if (!c || !["SUCCESS", "SKIPPED", "NEUTRAL"].includes(c.conclusion)) continue;
        if (c.startedAt)   starts.push(c.startedAt);
        if (c.completedAt) ends.push(c.completedAt);
      }
      if (!starts.length || !ends.length) continue;
      const t0 = new Date(starts.sort()[0]).getTime();
      const t1 = new Date(ends.sort().at(-1)).getTime();
      const d  = (t1 - t0) / 1000;
      if (d > 10 && d < 7200) durations.push(d);
    }
    return durations.length >= 3
      ? durations.reduce((a, b) => a + b, 0) / durations.length
      : null;
  } catch { return null; }
}

function getCiAvg(repo) {
  const history = loadCiHistory();
  const now     = Date.now();
  const entry   = history[repo] ?? {};
  if (entry.ts && now - entry.ts < CI_HISTORY_TTL) return entry.avg ?? null;
  const avg = fetchRepoCiAvg(repo);
  if (avg !== null) {
    history[repo] = { ts: now, avg };
    try { writeFileSync(CI_HISTORY_FILE, JSON.stringify(history)); } catch {}
  }
  return avg ?? entry.avg ?? null;
}

// ── snooze ─────────────────────────────────────────────────────────────────────
function isSnoozed() {
  try {
    return Date.now() - parseFloat(readFileSync(SNOOZE_FILE, "utf8")) < SNOOZE_DURATION;
  } catch { return false; }
}
function snooze() {
  try { writeFileSync(SNOOZE_FILE, String(Date.now())); } catch {}
}

// ── lock ───────────────────────────────────────────────────────────────────────
async function withLock(fn) {
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
async function getPrs() {
  return withLock(async () => {
    const now = Date.now();
    try {
      const cached    = JSON.parse(await readFile(CACHE_FILE, "utf8"));
      const hasPending = cached.has_pending_ci ?? false;
      const ttl = hasPending ? TTL_FAST : isSnoozed() ? TTL_SNOOZED : TTL_SLOW;
      // ts may be seconds (from legacy Python writer) or ms — normalise to ms
      let ts = cached.ts ?? 0;
      if (ts < 1e12) ts *= 1000;
      if (now - ts < ttl) return cached.prs;
    } catch {}

    const prs = fetchFromGitHub();
    if (prs === null) {
      try { return JSON.parse(await readFile(CACHE_FILE, "utf8")).prs; } catch { return []; }
    }
    const hasPending = prs.some(p => p.ciState === "pending");
    await writeFile(CACHE_FILE, JSON.stringify({ ts: now, prs, has_pending_ci: hasPending }));
    await renderAll(prs);
    return prs;
  });
}

// ── svg renderers ──────────────────────────────────────────────────────────────

/** PR card — docker/ports visual style with calendar-style header bar */
function makePrSvg(pr, slotIdx) {
  const PAD = 2, R = 6;
  const cx = PAD, cy = PAD, cw = 72 - PAD * 2, ch = 72 - PAD * 2;
  // Header bar (calendar-style, 16 px tall)
  const bx = cx + 3, by = cy + 3, bh = 16, bw = cw - 6, br = 4;

  const ci    = pr.ciState;
  const rv    = pr.isDraft ? "draft" : pr.reviewDecision;
  const ciCol = CI_COLOR[ci] ?? C_GRAY;
  const rvCol = RV_COLOR[rv] ?? C_GRAY;
  const rvLbl = RV_LABEL[rv] ?? "";
  const ciLbl = CI_LABEL[ci] ?? "";
  const ciAvg = ci === "pending" ? getCiAvg(pr.repo) : null;

  // ── header bar (solid or progress) ──────────────────────────────────────────
  const clipId = `bc${slotIdx}`;
  let barSvg;
  if (ci === "pending" && pr.ciStartedAt) {
    const elapsed = (Date.now() - new Date(pr.ciStartedAt).getTime()) / 1000;
    const fillCol = (ciAvg && elapsed >= ciAvg) ? C_ORANGE : C_YELLOW;
    if (ciAvg && ciAvg > 0) {
      const fillW = Math.max(6, Math.round(Math.min(elapsed / ciAvg, 1.0) * bw));
      barSvg = `
  <defs><clipPath id="${clipId}"><rect x="${bx}" y="${by}" width="${bw}" height="${bh}" rx="${br}"/></clipPath></defs>
  <rect x="${bx}" y="${by}" width="${bw}" height="${bh}" rx="${br}" fill="${BORDER}"/>
  <rect x="${bx}" y="${by}" width="${fillW}" height="${bh}" clip-path="url(#${clipId})" fill="${fillCol}"/>`;
    } else {
      barSvg = `<rect x="${bx}" y="${by}" width="${bw}" height="${bh}" rx="${br}" fill="${ciCol}"/>`;
    }
  } else {
    barSvg = `<rect x="${bx}" y="${by}" width="${bw}" height="${bh}" rx="${br}" fill="${ciCol}"/>`;
  }

  // ── repo name (left) + PR # (right) in black, measured to avoid overlap ──────
  // Bold 8 px DejaVu ≈ 5.5 px/char
  const prStr     = `#${pr.number}`;
  const prW       = prStr.length * 5.5 + 4;
  const maxRepoCh = Math.max(1, Math.floor((bw - prW - 14) / 5.5));
  let repoName    = pr.repo.split("/").pop();
  if (repoName.length > maxRepoCh)
    repoName = repoName.slice(0, maxRepoCh - 1) + "…";
  // y for text centred in badge (baseline = centre + ~35% of font size)
  const barTextY = by + bh - 5;  // matches docker/ports convention

  // ── title: vertically centred between bar and status bar ────────────────────
  const lines      = wrapText(pr.title, 13, 3);
  const contentTop = by + bh + 2;   // 23
  const contentBot = 72 - 16;       // 56
  const textH      = lines.length * 10;
  const firstLineY = contentTop + Math.floor((contentBot - contentTop - textH) / 2) + 7; // +7 = baseline offset for 8px font
  const titleSvg   = lines.map((l, i) =>
    `<text x="${cx + 4}" y="${firstLineY + i * 10}" font-family="DejaVu Sans,sans-serif" font-size="8" fill="${C_WHITE}">${esc(l)}</text>`
  ).join("\n  ");

  // ── status bar (bottom 14 px) ────────────────────────────────────────────────
  const sbY  = 72 - 16;
  const sbTY = sbY + 10;  // baseline for 7 px font centred in 14 px bar

  const ciLblSvg = ciLbl
    ? `<text x="${cx + 5}" y="${sbTY}" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="7" fill="${ciCol}">${ciLbl}</text>`
    : "";

  let centerSvg = "";
  if (ci === "pending" && ciAvg) {
    const mins   = Math.floor(ciAvg / 60);
    const avgStr = mins < 60
      ? `${mins}m`
      : `${Math.floor(mins / 60)}h${String(mins % 60).padStart(2, "0")}m`;
    centerSvg = `<text x="36" y="${sbTY}" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="7" fill="${C_YELLOW}" text-anchor="middle">${avgStr}</text>`;
  } else if (pr.comments > 0) {
    const nc    = pr.comments;
    const ncStr = String(nc);
    const ncW   = ncStr.length * 4.5;
    const totalW= 9 + 3 + ncW;
    const iconX = Math.round(36 - totalW / 2);
    const iconY = sbY + 4;
    centerSvg = `
  <rect x="${iconX}" y="${iconY}" width="9" height="7" rx="1" fill="${C_GRAY}"/>
  <polygon points="${iconX + 1},${iconY + 7} ${iconX + 3},${iconY + 9} ${iconX + 5},${iconY + 7}" fill="${C_GRAY}"/>
  <text x="${iconX + 12}" y="${sbTY}" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="7" fill="${C_GRAY}">${nc}</text>`;
  }

  const rvLblSvg = rvLbl
    ? `<text x="${72 - cx - 4}" y="${sbTY}" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="7" fill="${rvCol}" text-anchor="end">${rvLbl}</text>`
    : "";

  return `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" fill="${BG}"/>
  <rect x="${cx}" y="${cy}" width="${cw}" height="${ch}" rx="${R}" fill="${CARD_BG}"/>
  ${barSvg}
  <text x="${bx + 4}" y="${barTextY}" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="8" fill="#000000">${esc(repoName)}</text>
  <text x="${bx + bw - 3}" y="${barTextY}" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="8" fill="#000000" text-anchor="end">${esc(prStr)}</text>
  ${titleSvg}
  <rect x="${cx + 1}" y="${sbY}" width="${cw - 2}" height="14" rx="3" fill="${BORDER}"/>
  ${ciLblSvg}
  ${centerSvg}
  ${rvLblSvg}
</svg>`;
}

function makeEmptySvg() {
  const PAD = 2, R = 6;
  return `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" fill="${BG}"/>
  <rect x="${PAD}" y="${PAD}" width="${72 - PAD * 2}" height="${72 - PAD * 2}" rx="${R}" fill="${CARD_BG}"/>
</svg>`;
}

/** Header card (key 0 in github.deck) — octocat + counts */
function makeHeaderSvg(prs) {
  const n       = prs.length;
  const nPend   = prs.filter(p => p.ciState === "pending").length;
  const nFail   = prs.filter(p => p.ciState === "failure").length;
  const nReview = prs.filter(p => p.reviewDecision === "changes_requested").length;
  const hasAlert = nFail > 0 || nReview > 0;
  const hasPend  = nPend > 0;

  const glow = hasAlert
    ? `<rect width="72" height="72" rx="6" fill="${C_RED}" opacity="0.15"/>`
    : hasPend
    ? `<rect width="72" height="72" rx="6" fill="${C_YELLOW}" opacity="0.12"/>`
    : "";

  const cntColor = hasAlert ? C_RED : hasPend ? C_YELLOW : n > 0 ? C_GREEN : C_GRAY;

  const parts = [];
  if (nReview) parts.push(`!${nReview}rev`);
  if (nFail)   parts.push(`x${nFail}CI`);
  if (nPend)   parts.push(`~${nPend}`);
  const status      = parts.join(" ") || (n > 0 ? "all good" : "");
  const statusColor = hasAlert ? C_RED : hasPend ? C_YELLOW : C_GREEN;

  // Official GitHub mark (16×16 viewBox), scaled to 28px centred, top of card
  const ghMark = `<g transform="translate(22,2) scale(1.75)"><path fill="#f0f6fc" d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"/></g>`;

  return `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" fill="${BG}"/>
  ${glow}
  ${ghMark}
  <text x="36" y="46" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="13" fill="${cntColor}" text-anchor="middle">${n}</text>
  <text x="36" y="57" font-family="DejaVu Sans,sans-serif" font-size="7" fill="${C_GRAY}" text-anchor="middle">open PRs</text>
  ${status ? `<text x="36" y="68" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="7" fill="${statusColor}" text-anchor="middle">${esc(status)}</text>` : ""}
</svg>`;
}

/** Badge for main.deck nav button — 4 quadrant circles:
 *   Top-left:     approved PRs with no CI error (green)
 *   Top-right:    waiting PRs without CI running (yellow)
 *   Bottom-left:  PRs with CI running (yellow/pending)
 *   Bottom-right: PRs with CI fail or changes requested (red)
 */
function makeBadgeSvg(prs) {
  // Top-left: approved + CI not failure
  const nApproved = prs.filter(p => p.reviewDecision === "approved" && p.ciState !== "failure").length;
  // Top-right: waiting for review + CI not running
  const nWaiting  = prs.filter(p => p.reviewDecision === "waiting" && p.ciState !== "pending").length;
  // Bottom-left: CI running/pending
  const nRunning  = prs.filter(p => p.ciState === "pending").length;
  // Bottom-right: CI fail OR changes requested
  const nProblems = prs.filter(p => p.ciState === "failure" || p.reviewDecision === "changes_requested").length;

  function badge(cx, cy, count, color) {
    if (count === 0) return `<circle cx="${cx}" cy="${cy}" r="9" fill="${BORDER}" opacity="0.5"/>`;
    const fs = count >= 10 ? 9 : 12;
    const ty = cy + (fs === 9 ? 3 : 4);
    return `<circle cx="${cx}" cy="${cy}" r="9" fill="${color}"/>
  <text x="${cx}" y="${ty}" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="${fs}" fill="white" text-anchor="middle">${count}</text>`;
  }

  const tl = badge(12, 12, nApproved, C_GREEN);
  const tr = badge(60, 12, nWaiting, C_YELLOW);
  const bl = badge(12, 60, nRunning, C_YELLOW);
  const br = badge(60, 60, nProblems, C_RED);

  // Official GitHub mark (16×16 viewBox), scaled to 28px centred
  const ghMark = `<g transform="translate(22,22) scale(1.75)"><path fill="#f0f6fc" d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"/></g>`;

  return `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" rx="10" fill="${BG}"/>
  ${ghMark}
  ${tl}
  ${tr}
  ${bl}
  ${br}
</svg>`;
}

// ── render all slots ───────────────────────────────────────────────────────────
async function renderAll(prs) {
  await mkdir(OUT_DIR, { recursive: true });
  const jobs = [];
  for (let i = 0; i < MAX_PR_SLOTS; i++) {
    const svg = i < prs.length ? makePrSvg(prs[i], i) : makeEmptySvg();
    jobs.push(sharp(Buffer.from(svg)).png().toFile(`${OUT_DIR}/pr-${i}.png`));
  }
  jobs.push(sharp(Buffer.from(makeHeaderSvg(prs))).png().toFile(`${OUT_DIR}/header.png`));
  jobs.push(sharp(Buffer.from(makeBadgeSvg(prs))).png().toFile(`${OUT_DIR}/badge.png`));
  await Promise.all(jobs);
}

// ── main ───────────────────────────────────────────────────────────────────────
const [,, cmd, arg] = process.argv;

if (cmd === "update-cache-bg") {
  await withLock(async () => {
    const prs = fetchFromGitHub();
    if (prs !== null) {
      const hasPending = prs.some(p => p.ciState === "pending");
      await writeFile(CACHE_FILE, JSON.stringify({ ts: Date.now(), prs, has_pending_ci: hasPending }));
      await renderAll(prs);
    }
  });
  process.exit(0);
}

if (cmd === "badge" || cmd === "header" || cmd === "icon") {
  let path = "";
  if (cmd === "badge") {
    path = `${OUT_DIR}/badge.png`;
  } else if (cmd === "header") {
    path = `${OUT_DIR}/header.png`;
  } else {
    const n = parseInt(arg, 10);
    path = `${OUT_DIR}/pr-${n}.png`;
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
      const hasPending = cached.has_pending_ci ?? false;
      const ttl = hasPending ? TTL_FAST : isSnoozed() ? TTL_SNOOZED : TTL_SLOW;
      let ts = cached.ts ?? 0;
      if (ts < 1e12) ts *= 1000;
      if (Date.now() - ts >= ttl) {
        shouldFetch = true;
      }
    } catch {
      shouldFetch = true;
    }
  }

  if (shouldFetch) {
    // Spawn background process to fetch and update everything if not already locked
    if (!existsSync(LOCK_FILE)) {
      spawn("bun", [process.argv[1], "update-cache-bg"], {
        detached: true,
        stdio: "ignore",
      }).unref();
    }
  }
  process.exit(0);
}

let prs;
try { prs = await getPrs(); }
catch (e) { console.error("error fetching PRs:", e); prs = []; }

let cacheTs = 0;
try {
  let ts = JSON.parse(readFileSync(CACHE_FILE, "utf8")).ts ?? 0;
  if (ts < 1e12) ts *= 1000;  // normalise seconds → ms
  cacheTs = ts;
} catch {}

if (cmd === "url") {
  const n = parseInt(arg, 10);
  try {
    const url = JSON.parse(readFileSync(CACHE_FILE, "utf8")).prs?.[n]?.url;
    if (url) {
      snooze();
      spawn("xdg-open", [url], {
        detached: true, stdio: "ignore",
        env: { ...process.env, DISPLAY: process.env.DISPLAY || ":1" },
      }).unref();
    }
  } catch (e) {
    console.error("error opening PR:", e);
    process.exit(1);
  }

} else {
  console.error("unknown command:", cmd);
  process.exit(1);
}
