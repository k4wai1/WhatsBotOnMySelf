// addons/fetch.js
const { execSync } = require("child_process");
const { createCanvas, registerFont } = require("canvas");
const path = require("path");
const fs = require("fs");

// 1. REGISTRAR FUENTE NERD FONT
const fontPath = path.join(__dirname, "../assets/fonts/HackNerdFont-Regular.ttf");

if (fs.existsSync(fontPath)) {
    registerFont(fontPath, { family: "Hack Nerd Font" });
} else {
    console.warn("⚠️ No se encontró HackNerdFont. Los iconos no se verán bien.");
}

const PALETTE = {
  bg: '#15141b', fg: '#edecee',
  colors: [
    '#110f18', '#ff6767', '#61ffca', '#ffca85', '#a277ff', '#f694ff', '#61ffca', '#edecee',
    '#4d4d4d', '#ff6767', '#61ffca', '#ffca85', '#a277ff', '#f694ff', '#61ffca', '#edecee'
  ]
};

function generateFetchImage() {
    let stdout;
    try {
        stdout = execSync("fastfetch --pipe false", {
            env: { ...process.env, TERM: 'xterm-256color' },
            timeout: 10000,
            encoding: 'utf8'
        });
    } catch (e) {
        stdout = e.stdout ? e.stdout.toString() : "Error ejecutando fastfetch";
    }

    const cleanStdout = stdout.replace(/\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)/g, '');
    const tokens = [];
    const regex = /(\x1b\[[0-9;?]*[a-zA-Z])|(\n)|(\r)|([^\x1b\r\n]+)/g;
    let match;
    while ((match = regex.exec(cleanStdout)) !== null) {
        if (match[1]) tokens.push({ type: 'ansi', code: match[1] });
        else if (match[2]) tokens.push({ type: 'newline' });
        else if (match[3]) tokens.push({ type: 'cr' });
        else if (match[4]) tokens.push({ type: 'text', text: match[4] });
    }

    const grid = [];
    let cx = 0, cy = 0, maxCx = 0, maxCy = 0;
    let currentFg = PALETTE.fg, bold = false;
    let savedCx = 0, savedCy = 0;

    tokens.forEach(token => {
        if (token.type === 'newline') {
            cy++; cx = 0;
            if (cy > maxCy) maxCy = cy;
        } else if (token.type === 'cr') {
            cx = 0;
        } else if (token.type === 'text') {
            if (!grid[cy]) grid[cy] = [];
            for (let i = 0; i < token.text.length; i++) {
                grid[cy][cx] = { char: token.text[i], color: currentFg };
                cx++;
                if (cx > maxCx) maxCx = cx;
            }
        } else if (token.type === 'ansi') {
            const ansiMatch = token.code.match(/\x1b\[([0-9;?]*)(\w)/);
            if (!ansiMatch) return;
            const paramsStr = ansiMatch[1];
            const cmd = ansiMatch[2];
            const params = paramsStr ? paramsStr.split(';').map(Number) : [0];

            if (cmd === 'm') { 
                for (let i = 0; i < params.length; i++) {
                    const c = params[i];
                    if (c === 0) { currentFg = PALETTE.fg; bold = false; }
                    else if (c === 1) bold = true;
                    else if (c === 22) bold = false;
                    else if (c >= 30 && c <= 37) currentFg = PALETTE.colors[c - 30 + (bold ? 8 : 0)];
                    else if (c >= 90 && c <= 97) currentFg = PALETTE.colors[c - 90 + 8];
                    else if (c === 38 && params[i+1] === 5) { currentFg = PALETTE.colors[params[i+2]] || '#61ffca'; i+=2; }
                    else if (c === 38 && params[i+1] === 2) { currentFg = `rgb(${params[i+2]},${params[i+3]},${params[i+4]})`; i+=4; }
                }
            } else if (cmd === 'A') { cy = Math.max(0, cy - (paramsStr ? parseInt(paramsStr, 10) : 1));
            } else if (cmd === 'B') { cy += (paramsStr ? parseInt(paramsStr, 10) : 1); if (cy > maxCy) maxCy = cy;
            } else if (cmd === 'C') { cx += (paramsStr ? parseInt(paramsStr, 10) : 1); if (cx > maxCx) maxCx = cx;
            } else if (cmd === 'D') { cx = Math.max(0, cx - (paramsStr ? parseInt(paramsStr, 10) : 1));
            } else if (cmd === 'H' || cmd === 'f') { 
                cy = Math.max(0, (params[0] || 1) - 1); cx = Math.max(0, (params[1] || 1) - 1);
                if (cy > maxCy) maxCy = cy; if (cx > maxCx) maxCx = cx;
            } else if (cmd === 's') { savedCx = cx; savedCy = cy;
            } else if (cmd === 'u') { cx = savedCx; cy = savedCy;
            } else if (cmd === 'K') { 
                const n = paramsStr ? parseInt(paramsStr, 10) : 0;
                if (!grid[cy]) grid[cy] = [];
                if (n === 0) { for(let x = cx; x <= maxCx; x++) delete grid[cy][x]; }
                else if (n === 1) { for(let x = 0; x <= cx; x++) delete grid[cy][x]; }
                else if (n === 2) { grid[cy] = []; }
            }
        }
    });

    while (maxCy >= 0 && (!grid[maxCy] || grid[maxCy].length === 0 || grid[maxCy].every(c => !c))) { maxCy--; }

    const fontSize = 15;
    const lineHeight = Math.floor(fontSize * 1.2);
    const padding = 25;
    const fontFamily = fs.existsSync(fontPath) ? '"Hack Nerd Font"' : '"DejaVu Sans Mono", monospace';

    const tempCanvas = createCanvas(1, 1);
    const tempCtx = tempCanvas.getContext('2d');
    tempCtx.font = `${fontSize}px ${fontFamily}`;
    const charWidth = tempCtx.measureText('M').width; 

    const width = Math.ceil((maxCx + 1) * charWidth + padding * 2);
    const height = Math.ceil((maxCy + 1) * lineHeight + padding * 2);

    const canvas = createCanvas(width, height);
    const ctx = canvas.getContext('2d');

    ctx.fillStyle = PALETTE.bg;
    ctx.fillRect(0, 0, width, height);
    ctx.font = `${fontSize}px ${fontFamily}`;
    ctx.textBaseline = 'top';

    for (let y = 0; y <= maxCy; y++) {
        const row = grid[y] || [];
        let currentText = '';
        let currentColor = PALETTE.fg;
        let startX = 0;

        for (let x = 0; x <= maxCx + 1; x++) {
            const cell = row[x];
            const nextColor = cell ? cell.color : null;

            if (cell && nextColor === currentColor) {
                currentText += cell.char;
            } else {
                if (currentText.length > 0) {
                    ctx.fillStyle = currentColor;
                    ctx.fillText(currentText, padding + startX * charWidth, padding + y * lineHeight);
                }
                if (cell) {
                    currentColor = nextColor;
                    startX = x;
                    currentText = cell.char;
                } else {
                    currentText = '';
                }
            }
        }
    }

    return canvas.toBuffer('image/png');
}

// NUEVA ESTRUCTURA DEL ROUTER
module.exports = {
  commands: ['fetch'],
  handler: async (sock, msg, args) => {
    const jid = msg.key.remoteJid;

    try {
      await sock.sendMessage(jid, { react: { text: "⏳", key: msg.key } });

      const imgBuf = generateFetchImage();
      
      await sock.sendMessage(jid, { 
          image: imgBuf, 
          mimetype: "image/png",
          caption: "🖥️" // Caption minimalista
      }, { quoted: msg });

      await sock.sendMessage(jid, { react: { text: "✅", key: msg.key } });

    } catch (err) {
      console.error("Error en addon .fetch:", err.message);
      await sock.sendMessage(jid, { react: { text: "❌", key: msg.key } });
    }
  }
};
