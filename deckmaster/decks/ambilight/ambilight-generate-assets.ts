#!/usr/bin/env bun
import sharp from "sharp";
import { mkdir } from "fs/promises";
import { join } from "path";

async function generate() {
    const assetsDir = join(import.meta.dir, "assets");
    await mkdir(assetsDir, { recursive: true });

    const width = 144;
    const height = 144;

    for (const state of ["on", "off"]) {
        const isOn = state === "on";
        
        // Define SVG filters & gradients
        const glowFilter = `
            <filter id="blur" x="-20%" y="-20%" width="140%" height="140%">
                <feGaussianBlur stdDeviation="12" />
            </filter>
        `;
        
        const gradients = isOn ? `
            <radialGradient id="glow" cx="50%" cy="50%" r="50%">
                <stop offset="0%" stop-color="#ffffff" stop-opacity="0.8" />
                <stop offset="40%" stop-color="#00f0ff" stop-opacity="0.6" />
                <stop offset="70%" stop-color="#ff00f0" stop-opacity="0.4" />
                <stop offset="100%" stop-color="#000000" stop-opacity="0" />
            </radialGradient>
        ` : `
            <radialGradient id="glow" cx="50%" cy="50%" r="50%">
                <stop offset="0%" stop-color="#ffffff" stop-opacity="0.2" />
                <stop offset="100%" stop-color="#000000" stop-opacity="0" />
            </radialGradient>
        `;

        const dotColor = isOn ? "#00ff96" : "#ff3232";
        const statusText = isOn ? "ON" : "OFF";
        const statusColor = isOn ? "#00ffc8" : "#828282";
        const textColor = isOn ? "#ffffff" : "#b4b4b4";

        const svg = `
            <svg width="${width}" height="${height}" viewBox="0 0 ${width} ${height}" xmlns="http://www.w3.org/2000/svg">
                <defs>
                    ${glowFilter}
                    ${gradients}
                </defs>
                
                <!-- Black background -->
                <rect width="${width}" height="${height}" fill="#000000" />
                
                <!-- Glow background -->
                <circle cx="72" cy="72" r="60" fill="url(#glow)" filter="url(#blur)" />
                
                <!-- TV Frame Bezel -->
                <rect x="22" y="32" width="100" height="68" rx="8" ry="8" fill="#282828" stroke="#3c3c3c" stroke-width="2" />
                
                <!-- TV Screen -->
                <rect x="28" y="38" width="88" height="56" rx="4" ry="4" fill="#121212" />
                
                <!-- Stand Neck -->
                <rect x="66" y="100" width="12" height="14" fill="#282828" />
                
                <!-- Stand Base -->
                <polygon points="47,118 97,118 87,113 57,113" fill="#323232" />
                
                <!-- Text elements -->
                <text x="72" y="52" fill="${textColor}" font-family="DejaVu Sans, sans-serif" font-weight="bold" font-size="10" text-anchor="middle">AMBILIGHT</text>
                <text x="72" y="78" fill="${statusColor}" font-family="DejaVu Sans, sans-serif" font-weight="bold" font-size="18" text-anchor="middle">${statusText}</text>
                
                <!-- Power LED Dot -->
                <circle cx="112" cy="94" r="3" fill="${dotColor}" />
            </svg>
        `;

        const destPath = join(assetsDir, `ambilight-${state}.png`);
        await sharp(Buffer.from(svg))
            .png()
            .toFile(destPath);
        
        console.log(`Generated: ${destPath}`);
    }
}

generate().catch(console.error);
