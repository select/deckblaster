#!/usr/bin/env bun
import { existsSync, rmSync } from "fs";
import { join } from "path";

const PID_FILE = "/tmp/ambilight-sync.pid";

async function isRunning(pid: number): Promise<boolean> {
  try {
    process.kill(pid, 0);
  } catch {
    return false;
  }

  // Double check cmdline
  const cmdlinePath = `/proc/${pid}/cmdline`;
  try {
    const cmd = await Bun.file(cmdlinePath).text();
    if (cmd.includes("rust_ambilight")) {
      return true;
    }
  } catch {
    // ignore
  }
  return false;
}

async function stopAmbilight() {
  if (existsSync(PID_FILE)) {
    try {
      const pidStr = (await Bun.file(PID_FILE).text()).trim();
      const pid = parseInt(pidStr, 10);
      if (pid && (await isRunning(pid))) {
        console.log(`Stopping Ambilight process (PID ${pid})...`);
        process.kill(pid, "SIGTERM");
        
        // Wait up to 2 seconds for process to exit
        for (let i = 0; i < 20; i++) {
          try {
            process.kill(pid, 0);
            await Bun.sleep(100);
          } catch {
            break;
          }
        }
      } else {
        console.log("Ambilight process was not running, but PID file existed.");
      }
    } catch (e) {
      console.error(`Error stopping: ${e}`);
    } finally {
      if (existsSync(PID_FILE)) {
        rmSync(PID_FILE);
      }
    }
  } else {
    console.log("Ambilight is already stopped.");
  }
}

async function startAmbilight(extraArgs: string[]) {
  if (existsSync(PID_FILE)) {
    try {
      const pidStr = (await Bun.file(PID_FILE).text()).trim();
      const pid = parseInt(pidStr, 10);
      if (pid && (await isRunning(pid))) {
        console.log(`Ambilight is already running with PID ${pid}.`);
        return;
      }
    } catch {
      // ignore
    }
  }

  const dirPath = import.meta.dir;
  const binaryPath = join(dirPath, "rust_ambilight", "target", "release", "rust_ambilight");

  if (!existsSync(binaryPath)) {
    console.error(`Error: Compiled Rust binary not found at ${binaryPath}. Please build it first.`);
    process.exit(1);
  }

  console.log("Starting Ambilight (GPU Rust) process in the background...");
  
  try {
    const cmd = [binaryPath, ...extraArgs];
    const proc = Bun.spawn(cmd, {
      stdout: "ignore",
      stderr: "ignore",
      detached: true,
      cwd: dirPath,
    });
    
    // Unref child process so Bun can exit without waiting for it
    proc.unref();

    await Bun.write(PID_FILE, proc.pid.toString());
    console.log(`Started Ambilight successfully with PID ${proc.pid}.`);
  } catch (e) {
    console.error(`Failed to start Ambilight: ${e}`);
  }
}

async function main() {
  const args = process.argv.slice(2);
  const action = args[0]?.toLowerCase();

  if (action === "start") {
    await startAmbilight(args.slice(1));
  } else if (action === "stop") {
    await stopAmbilight();
  } else if (action === "status") {
    if (existsSync(PID_FILE)) {
      try {
        const pidStr = (await Bun.file(PID_FILE).text()).trim();
        const pid = parseInt(pidStr, 10);
        if (pid && (await isRunning(pid))) {
          console.log("running");
          process.exit(0);
        }
      } catch {
        // ignore
      }
    }
    console.log("stopped");
    process.exit(1);
  } else if (action === "icon") {
    if (existsSync(PID_FILE)) {
      try {
        const pidStr = (await Bun.file(PID_FILE).text()).trim();
        const pid = parseInt(pidStr, 10);
        if (pid && (await isRunning(pid))) {
          console.log("decks/ambilight/assets/ambilight-on.png");
          process.exit(0);
        }
      } catch {
        // ignore
      }
    }
    console.log("decks/ambilight/assets/ambilight-off.png");
    process.exit(0);
  } else {
    // Toggle mode
    let wasRunning = false;
    if (existsSync(PID_FILE)) {
      try {
        const pidStr = (await Bun.file(PID_FILE).text()).trim();
        const pid = parseInt(pidStr, 10);
        if (pid && (await isRunning(pid))) {
          wasRunning = true;
          await stopAmbilight();
        }
      } catch {
        // ignore
      }
    }
    if (!wasRunning) {
      await startAmbilight(args);
    }
  }
}

main().catch(console.error);
