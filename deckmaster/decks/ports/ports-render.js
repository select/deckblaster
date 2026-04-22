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
import { existsSync, readFileSync, readlinkSync } from "fs";
import { execSync } from "child_process";
import { basename, join, dirname } from "path";

// ── config ────────────────────────────────────────────────────────────────────

const STATE_FILE = "/tmp/streamdeck-ports.json";
const IMG_DIR    = "/tmp/streamdeck-ports";
const ICON_PATH  = import.meta.dir + "/assets/ports.png";
const CACHE_TTL  = 5_000; // ms
const SLOTS      = 9;     // 3×3 middle grid, column-major (slots 0-8)

// ── port scanner (inlined from port-whisperer, Linux only) ───────────────────

function _getListeningPortsRaw() {
  const entries = [];
  const portMap = new Map();
  try {
    const raw = execSync("ss -tlnp 2>/dev/null", { encoding: "utf8", timeout: 10000 });
    for (const line of raw.trim().split("\n").slice(1)) {
      const parts = line.split(/\s+/);
      if (parts.length < 5) continue;
      const portMatch = parts[3].match(/:([\d]+)$/);
      if (!portMatch) continue;
      const port = parseInt(portMatch[1], 10);
      if (portMap.has(port)) continue;
      const usersField = parts.slice(5).join(" ");
      const pidMatch  = usersField.match(/pid=(\d+)/);
      const nameMatch = usersField.match(/\("([^"]+)"/);
      if (pidMatch) {
        const pid = parseInt(pidMatch[1], 10);
        const processName = nameMatch ? nameMatch[1] : (() => {
          try { return readFileSync(`/proc/${pid}/comm`, "utf8").trim(); } catch { return "unknown"; }
        })();
        portMap.set(port, true);
        entries.push({ port, pid, processName });
      }
    }
  } catch {}
  return entries;
}

function _batchProcessInfo(pids) {
  const map = new Map();
  if (!pids.length) return map;
  try {
    const raw = execSync(`ps -p ${pids.join(",")} -o pid=,ppid=,stat=,rss=,lstart=,command= 2>/dev/null`,
      { encoding: "utf8", timeout: 5000 }).trim();
    for (const line of raw.split("\n")) {
      const m = line.trim().match(/^(\d+)\s+(\d+)\s+(\S+)\s+(\d+)\s+\w+\s+(\w+\s+\d+\s+[\d:]+\s+\d+)\s+(.*)$/);
      if (!m) continue;
      map.set(parseInt(m[1], 10), { ppid: parseInt(m[2], 10), stat: m[3], rss: parseInt(m[4], 10), lstart: m[5], command: m[6] });
    }
  } catch {}
  return map;
}

function _batchCwd(pids) {
  const map = new Map();
  for (const pid of pids) {
    try {
      const cwd = readlinkSync(`/proc/${pid}/cwd`);
      if (cwd?.startsWith("/")) map.set(pid, cwd);
    } catch {}
  }
  return map;
}

function _batchDockerInfo() {
  const map = new Map();
  try {
    const raw = execSync('docker ps --format "{{.Ports}}\t{{.Names}}\t{{.Image}}" 2>/dev/null',
      { encoding: "utf8", timeout: 5000 }).trim();
    for (const line of raw.split("\n")) {
      if (!line.trim()) continue;
      const [portsStr, name, image] = line.split("\t");
      if (!portsStr || !name) continue;
      const seen = new Set();
      for (const m of portsStr.matchAll(/(?:\d+\.\d+\.\d+\.\d+|::):(\d+)->/g)) {
        const port = parseInt(m[1], 10);
        if (!seen.has(port)) { seen.add(port); map.set(port, { name, image }); }
      }
    }
  } catch {}
  return map;
}

function _findProjectRoot(dir) {
  const markers = ["package.json", "Cargo.toml", "go.mod", "pyproject.toml", "Gemfile", "pom.xml"];
  let cur = dir, depth = 0;
  while (cur !== "/" && cur !== dirname(cur) && depth < 15) {
    if (markers.some(m => existsSync(join(cur, m)))) return cur;
    cur = dirname(cur); depth++;
  }
  return dir;
}

function _detectFramework(root) {
  const pkgPath = join(root, "package.json");
  if (existsSync(pkgPath)) {
    try {
      const { dependencies: d = {}, devDependencies: dd = {} } = JSON.parse(readFileSync(pkgPath, "utf8"));
      const all = { ...d, ...dd };
      if (all["next"]) return "Next.js";
      if (all["nuxt"] || all["nuxt3"]) return "Nuxt";
      if (all["@sveltejs/kit"]) return "SvelteKit";
      if (all["svelte"]) return "Svelte";
      if (all["@remix-run/react"] || all["remix"]) return "Remix";
      if (all["astro"]) return "Astro";
      if (all["vite"]) return "Vite";
      if (all["@angular/core"]) return "Angular";
      if (all["vue"]) return "Vue";
      if (all["react"]) return "React";
      if (all["express"]) return "Express";
      if (all["fastify"]) return "Fastify";
      if (all["hono"]) return "Hono";
      if (all["@nestjs/core"]) return "NestJS";
    } catch {}
  }
  if (existsSync(join(root, "vite.config.ts")) || existsSync(join(root, "vite.config.js"))) return "Vite";
  if (existsSync(join(root, "next.config.js")) || existsSync(join(root, "next.config.mjs"))) return "Next.js";
  if (existsSync(join(root, "Cargo.toml"))) return "Rust";
  if (existsSync(join(root, "go.mod"))) return "Go";
  if (existsSync(join(root, "manage.py"))) return "Django";
  return null;
}

function _detectFrameworkFromCommand(command, processName) {
  const cmd = (command || "").toLowerCase();
  if (cmd.includes("next")) return "Next.js";
  if (cmd.includes("vite")) return "Vite";
  if (cmd.includes("nuxt")) return "Nuxt";
  if (cmd.includes("webpack")) return "Webpack";
  if (cmd.includes("remix")) return "Remix";
  if (cmd.includes("astro")) return "Astro";
  if (cmd.includes("flask")) return "Flask";
  if (cmd.includes("django") || cmd.includes("manage.py")) return "Django";
  if (cmd.includes("uvicorn")) return "FastAPI";
  if (cmd.includes("rails")) return "Rails";
  const name = (processName || "").toLowerCase();
  if (name === "node") return "Node.js";
  if (name === "python" || name === "python3") return "Python";
  if (name === "ruby") return "Ruby";
  if (name === "java") return "Java";
  if (name === "go") return "Go";
  return null;
}

function _detectFrameworkFromImage(image) {
  if (!image) return "Docker";
  const img = image.toLowerCase();
  if (img.includes("postgres")) return "PostgreSQL";
  if (img.includes("redis")) return "Redis";
  if (img.includes("mysql") || img.includes("mariadb")) return "MySQL";
  if (img.includes("mongo")) return "MongoDB";
  if (img.includes("nginx")) return "nginx";
  if (img.includes("rabbitmq")) return "RabbitMQ";
  return "Docker";
}

function _formatUptime(ms) {
  const s = Math.floor(ms / 1000), m = Math.floor(s / 60), h = Math.floor(m / 60), d = Math.floor(h / 24);
  if (d > 0) return `${d}d ${h % 24}h`;
  if (h > 0) return `${h}h ${m % 60}m`;
  if (m > 0) return `${m}m ${s % 60}s`;
  return `${s}s`;
}

function _formatMemory(rssKB) {
  if (rssKB > 1048576) return `${(rssKB / 1048576).toFixed(1)} GB`;
  if (rssKB > 1024)    return `${(rssKB / 1024).toFixed(1)} MB`;
  return `${rssKB} KB`;
}

async function getListeningPorts() {
  const entries   = _getListeningPortsRaw();
  const pids      = [...new Set(entries.map(e => e.pid))];
  const psMap     = _batchProcessInfo(pids);
  const cwdMap    = _batchCwd(pids);
  const hasDocker = entries.some(e => e.processName.startsWith("com.docke") || e.processName === "docker");
  const dockerMap = hasDocker ? _batchDockerInfo() : new Map();

  return entries.map(({ port, pid, processName }) => {
    const ps  = psMap.get(pid);
    const cwd = cwdMap.get(pid);
    const info = {
      port, pid, processName,
      command: ps?.command ?? "",
      cwd: null, projectName: null, framework: null,
      uptime: null, startTime: null, status: "healthy", memory: null,
    };
    if (ps) {
      if (ps.stat.includes("Z")) info.status = "zombie";
      else if (ps.ppid === 1 && isDevProcess(processName, ps.command)) info.status = "orphaned";
      if (ps.rss > 0) info.memory = _formatMemory(ps.rss);
      if (ps.lstart) {
        const t = new Date(ps.lstart);
        if (!isNaN(t.getTime())) info.uptime = _formatUptime(Date.now() - t.getTime());
      }
      info.framework = _detectFrameworkFromCommand(ps.command, processName);
    }
    const docker = dockerMap.get(port);
    if (docker) {
      info.projectName = docker.name;
      info.framework   = _detectFrameworkFromImage(docker.image);
      info.processName = "docker";
    } else if (cwd) {
      const root = _findProjectRoot(cwd);
      info.cwd         = root;
      info.projectName = basename(root);
      info.framework   = info.framework || _detectFramework(root);
    }
    return info;
  }).sort((a, b) => a.port - b.port);
}

function isDevProcess(processName, command) {
  const name = (processName || "").toLowerCase();
  const cmd  = (command || "").toLowerCase();
  const systemApps = ["spotify","slack","discord","firefox","chrome","google","safari","figma",
    "notion","zoom","teams","iterm2","systemd","snapd","networkmanager","gdm","sshd",
    "cron","dbus-daemon","polkitd","rsyslogd","thermald","accounts-daemon"];
  if (systemApps.some(a => name.startsWith(a))) return false;
  const devNames = new Set(["node","python","python3","ruby","java","go","cargo","deno",
    "bun","php","uvicorn","gunicorn","flask","rails","npm","npx","yarn","pnpm",
    "tsc","tsx","esbuild","rollup","turbo","nx","jest","vitest","mocha",
    "pytest","cypress","playwright","rustc","dotnet","gradle","mvn","mix","elixir"]);
  if (devNames.has(name)) return true;
  if (name.startsWith("com.docke") || name === "docker") return true;
  const cmdIndicators = [/\bnode\b/,/\bnext[\s-]/,/\bvite\b/,/\bnuxt\b/,/\bwebpack\b/,
    /\bremix\b/,/\bastro\b/,/\bgulp\b/,/\bflask\b/,/\bdjango\b|manage\.py/,/\buvicorn\b/,/\brails\b/];
  return cmdIndicators.some(re => re.test(cmd));
}

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
  const fw       = esc((e.framework   || "").slice(0, 9));
  const uptime   = esc(e.uptime || "");

  // Card layout: top color badge (port number), then content below
  const PAD = 2, R = 6, CARD_BG = "#161b22";
  const cx = PAD, cy = PAD, cw = 72 - PAD * 2, ch = 72 - PAD * 2;
  const badgeX = cx + 3, badgeY = cy + 3, badgeH = 14, badgeW = cw - 6, badgeR = 4;
  const textX = cx + 5;

  // Status dot inside badge (right side)
  const statusDot = `<circle cx="${badgeX + badgeW - 6}" cy="${badgeY + badgeH / 2}" r="3" fill="${dotColor}"/>`;

  return `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <defs><clipPath id="card"><rect x="${cx}" y="${cy}" width="${cw}" height="${ch}" rx="${R}"/></clipPath></defs>
  <rect width="72" height="72" fill="${BG}"/>
  <rect x="${cx}" y="${cy}" width="${cw}" height="${ch}" rx="${R}" fill="${CARD_BG}"/>
  <rect x="${badgeX}" y="${badgeY}" width="${badgeW}" height="${badgeH}" rx="${badgeR}" fill="${fwColor}"/>
  <text x="${badgeX + 4}" y="${badgeY + badgeH - 4}" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="9" fill="#000000">${port}</text>
  ${statusDot}
  <text x="${textX}" y="32" font-family="DejaVu Sans,sans-serif" font-weight="bold" font-size="10" fill="#e6edf3">${proc}</text>
  <text x="${textX}" y="44" font-family="DejaVu Sans,sans-serif" font-size="9" fill="#8b949e">${project}</text>
  <text x="${textX}" y="55" font-family="DejaVu Sans,sans-serif" font-size="9" fill="${fwColor}">${fw}</text>
  <text x="${textX}" y="66" font-family="DejaVu Sans,sans-serif" font-size="8" fill="#8b949e">${uptime}</text>
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

function makeNoPortsSvg() {
  const PAD = 2, R = 6, CARD_BG = "#161b22";
  const cx = PAD, cy = PAD, cw = 72 - PAD * 2, ch = 72 - PAD * 2;
  return `<svg width="72" height="72" xmlns="http://www.w3.org/2000/svg">
  <rect width="72" height="72" fill="${BG}"/>
  <rect x="${cx}" y="${cy}" width="${cw}" height="${ch}" rx="${R}" fill="${CARD_BG}"/>
  <rect x="${cx + 3}" y="${cy + 3}" width="${cw - 6}" height="14" rx="4" fill="#30363d"/>
  <text x="36" y="42" font-family="DejaVu Sans,sans-serif" font-size="10" fill="#4b5563" text-anchor="middle">no dev</text>
  <text x="36" y="54" font-family="DejaVu Sans,sans-serif" font-size="10" fill="#4b5563" text-anchor="middle">ports</text>
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
