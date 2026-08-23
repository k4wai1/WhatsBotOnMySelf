// addons/ss.js
// Captura de pantalla de cualquier página web.
// .ss <url>            → PNG del sitio (viewport 1280x2400)
// Cadena: Chrome/Chromium headless → lightpanda (CDP) si no hay chrome.

const fs = require('fs-extra');
const path = require('path');
const os = require('os');
const { execFile } = require('child_process');
const { promisify } = require('util');
const execFileP = promisify(execFile);

const CHROME_CANDIDATES = [
    process.env.SS_CHROME_BIN,
    '/usr/bin/google-chrome-stable',
    '/usr/bin/google-chrome',
    '/usr/bin/chromium',
    '/usr/bin/chromium-browser',
].filter(Boolean);

const LIGHTPANDA_CANDIDATES = [
    process.env.SS_LIGHTPANDA_BIN,
    path.join(os.homedir(), '.cache/lightpanda-node/lightpanda'),
].filter(Boolean);

const WIDTH = 1280;
const HEIGHT = 2400;
const TIMEOUT_MS = 60000;

function normalizeUrl(raw) {
    let u = String(raw || '').trim();
    if (!u) return null;
    if (!/^https?:\/\//i.test(u)) {
        if (/^[a-z][a-z0-9+.-]*:/i.test(u)) return null; // file://, ftp://, etc. → bloqueado
        u = 'https://' + u;
    }
    try {
        const parsed = new URL(u);
        if (!/^https?:$/.test(parsed.protocol)) return null;
        return parsed.href;
    } catch {
        return null;
    }
}

// ─── Vía 1: Chrome/Chromium headless ───────────────────────────────────────
async function shotWithChrome(bin, url, outPath) {
    await execFileP(bin, [
        '--headless=new',
        '--no-sandbox',
        '--disable-gpu',
        '--disable-dev-shm-usage',
        '--hide-scrollbars',
        `--window-size=${WIDTH},${HEIGHT}`,
        '--virtual-time-budget=9000',
        `--screenshot=${outPath}`,
        url
    ], { timeout: TIMEOUT_MS });
    if (!fs.existsSync(outPath) || fs.statSync(outPath).size < 1000) {
        throw new Error('captura vacía');
    }
}

// ─── Vía 2: lightpanda por CDP (WebSocket) ─────────────────────────────────
function findFreePort() {
    return new Promise((resolve, reject) => {
        const net = require('net');
        const srv = net.createServer();
        srv.unref();
        srv.on('error', reject);
        srv.listen(0, '127.0.0.1', () => {
            const port = srv.address().port;
            srv.close(() => resolve(port));
        });
    });
}

function sleep(ms) { return new Promise(r => setTimeout(r, ms)); }

class CdpClient {
    constructor(ws) { this.ws = ws; this.id = 0; this.pending = new Map(); }

    static connect(url, timeoutMs = 20000) {
        const WebSocket = require('ws');
        return new Promise((resolve, reject) => {
            const ws = new WebSocket(url, { perMessageDeflate: false });
            const timer = setTimeout(() => { ws.terminate(); reject(new Error('CDP: timeout de conexión')); }, timeoutMs);
            const client = new CdpClient(ws);
            ws.on('message', (raw) => {
                try {
                    const data = JSON.parse(raw.toString());
                    if (data.id && client.pending.has(data.id)) {
                        const { resolve: res, reject: rej } = client.pending.get(data.id);
                        client.pending.delete(data.id);
                        if (data.error) rej(new Error(data.error.message || 'error CDP'));
                        else res(data.result);
                    }
                } catch {}
            });
            ws.on('open', () => { clearTimeout(timer); resolve(client); });
            ws.on('error', (e) => { clearTimeout(timer); reject(e); });
        });
    }

    send(method, params = {}) {
        const id = ++this.id;
        return new Promise((resolve, reject) => {
            const timer = setTimeout(() => { this.pending.delete(id); reject(new Error(`CDP: ${method} timeout`)); }, TIMEOUT_MS);
            this.pending.set(id, {
                resolve: (v) => { clearTimeout(timer); resolve(v); },
                reject: (e) => { clearTimeout(timer); reject(e); }
            });
            this.ws.send(JSON.stringify({ id, method, params }));
        });
    }

    close() { try { this.ws.close(); } catch {} }
}

async function shotWithLightpanda(bin, url, outPath) {
    const port = await findFreePort();
    const proc = require('child_process').spawn(bin, [
        'serve', '--host', '127.0.0.1', '--port', String(port)
    ], { stdio: 'ignore', detached: false });

    try {
        // Esperar a que el servidor CDP responda
        let ready = false;
        for (let i = 0; i < 40; i++) {
            await sleep(250);
            try {
                const v = await fetch(`http://127.0.0.1:${port}/json/version`, { signal: AbortSignal.timeout(1500) });
                if (v.ok) { ready = true; break; }
            } catch {}
        }
        if (!ready) throw new Error('lightpanda no arrancó');

        const created = await fetch(`http://127.0.0.1:${port}/json/new?${encodeURIComponent(url)}`, {
            method: 'PUT',
            signal: AbortSignal.timeout(15000)
        }).then(r => r.json());

        if (!created?.webSocketDebuggerUrl) throw new Error('no se pudo crear la pestaña');

        const client = await CdpClient.connect(created.webSocketDebuggerUrl);
        try {
            await client.send('Page.enable');
            await client.send('Emulation.setDeviceMetricsOverride', {
                width: WIDTH, height: HEIGHT, deviceScaleFactor: 1, mobile: false
            });
            await client.send('Page.navigate', { url }); // re-navegar por si /json/new no cargó
            await sleep(6000); // render + JS
            const shot = await client.send('Page.captureScreenshot', { format: 'png' });
            const buf = Buffer.from(shot.data, 'base64');
            if (buf.length < 1000) throw new Error('captura vacía');
            await fs.writeFile(outPath, buf);
        } finally {
            client.close();
        }
    } finally {
        try { proc.kill('SIGKILL'); } catch {}
    }
}

module.exports = {
    commands: ['ss', 'screenshot'],

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;
        const tmpDir = path.join(os.tmpdir(), `ss_${Date.now()}`);
        const outPath = path.join(tmpDir, 'shot.png');

        try {
            const url = normalizeUrl(args.join(' ').trim());
            if (!url) {
                await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
                await sock.sendMessage(jid, {
                    text: '📸 Captura páginas web.\n\n• `.ss google.com`\n• `.ss https://x.com/usuario`'
                }, { quoted: msg });
                return;
            }

            await sock.sendMessage(jid, { react: { text: '📸', key: msg.key } });
            await fs.ensureDir(tmpDir);

            const chromeBin = CHROME_CANDIDATES.find(c => fs.existsSync(c));
            const pandaBin = LIGHTPANDA_CANDIDATES.find(c => fs.existsSync(c));

            if (chromeBin) {
                await shotWithChrome(chromeBin, url, outPath);
            } else if (pandaBin) {
                await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });
                await shotWithLightpanda(pandaBin, url, outPath);
            } else {
                await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                await sock.sendMessage(jid, { text: '❌ No hay navegador en el servidor (chrome/chromium/lightpanda).' }, { quoted: msg });
                return;
            }

            await sock.sendMessage(jid, {
                image: await fs.readFile(outPath),
                caption: `📸 ${url}`
            }, { quoted: msg });
            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

        } catch (error) {
            console.error('❌ [ss]:', error.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
            await sock.sendMessage(jid, { text: `❌ No pude capturar esa página: ${String(error.message).slice(0, 150)}` }, { quoted: msg });
        } finally {
            await fs.remove(tmpDir).catch(() => {});
        }
    }
};
