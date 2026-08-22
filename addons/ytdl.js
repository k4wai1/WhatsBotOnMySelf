// addons/ytdl.js — YouTube Downloader interactivo
// Dependencia externa: yt-dlp (instalado en el sistema)
// Cookies: busca en assets/www.youtube.com_cookies.json o www.youtube.com_cookies.json
//           Acepta formato JSON (arreglo de cookies) o Netscape (formato texto)
//           Si es JSON, lo convierte automáticamente a Netscape para yt-dlp
// ────────────────────────────────────────────────────────────────────────────

const { execFile } = require('child_process');
const fs = require('fs-extra');
const path = require('path');
const crypto = require('crypto');
const util = require('util');

const execFileP = util.promisify(execFile);

// ─── Constantes ──────────────────────────────────────────────────────────

// Busca el archivo de cookies en varias rutas (assets/ primero, luego raíz)
const COOKIES_CANDIDATES = [
    path.join(__dirname, '..', 'assets', 'www.youtube.com_cookies.json'),
    path.join(__dirname, '..', 'www.youtube.com_cookies.json'),
    path.join(__dirname, '..', 'assets', 'www.youtube.com_cookies.txt'),
    path.join(__dirname, '..', 'www.youtube.com_cookies.txt'),
];

const TMP_BASE = '/tmp/ytdl';

// Umbral de tamaño: si el archivo estimado supera esto, se advierte (en bytes)
const WARN_SIZE = 60 * 1024 * 1024; // 60 MB

// Resoluciones objetivo en píxeles de alto (ordenadas de mayor a menor)
const RES_TIERS = [4320, 2160, 1440, 1080, 720, 480, 360, 240, 144];

// ─── Estado de sesiones interactivas ─────────────────────────────────────
// pendingSessions: Map<senderJid -> { info, options, createdAt }>  — menú de formatos
// pendingSearch:   Map<senderJid -> { results, createdAt }>        — resultados de búsqueda
const pendingSessions = new Map();
const pendingSearch = new Map();

const SESSION_TTL_MS = 10 * 60 * 1000; // 10 minutos

// Limpieza periódica de sesiones expiradas
setInterval(() => {
    const now = Date.now();
    for (const [jid, session] of pendingSessions) {
        if (now - session.createdAt > SESSION_TTL_MS) pendingSessions.delete(jid);
    }
    for (const [jid, session] of pendingSearch) {
        if (now - session.createdAt > SESSION_TTL_MS) pendingSearch.delete(jid);
    }
}, 60_000);

// ─── Conversor de cookies: JSON_array → Netscape ────────────────────────
// La extensión "cookies_localy.txt" exporta cookies en JSON con estructura:
//   [{ domain, name, value, path, secure, httpOnly, expirationDate, ... }]
// yt-dlp exige formato Netscape:
//   domain\tTRUE\tpath\tsecure\texpiry\tname\tvalue
// Esta función convierte sobre la marcha y escribe un archivo temporal.

let COOKIES_TEMP_FILE = null; // Para limpiar al final

/**
 * Prepara el archivo de cookies para yt-dlp.
 * Si el archivo es JSON, lo convierte a Netscape en un temp file.
 * Retorna la ruta al archivo listo para usar, o null si no hay cookies.
 */
function prepareCookies() {
    // Encontrar el primer archivo de cookies que exista
    const srcPath = COOKIES_CANDIDATES.find(f => fs.existsSync(f));
    if (!srcPath) return null;

    const raw = fs.readFileSync(srcPath, 'utf-8').trim();
    if (!raw) return null;

    const ext = path.extname(srcPath).toLowerCase();

    // ── Si termina en .txt, asumimos Netscape y lo usamos directo ──
    if (ext === '.txt') {
        console.log(`📝 ytdl: usando cookies Netscape desde ${srcPath}`);
        return srcPath;
    }

    // ── Si es JSON, detectar y convertir ──
    if (raw.startsWith('[')) {
        try {
            const cookies = JSON.parse(raw);
            if (!Array.isArray(cookies) || cookies.length === 0) {
                console.warn('⚠️ ytdl: archivo JSON de cookies vacío o inválido');
                return null;
            }

            // Generar contenido Netscape
            const lines = [
                '# Netscape HTTP Cookie File',
                '# Generado automáticamente desde JSON por ytdl.js',
                '',
            ];

            for (const c of cookies) {
                const domain = c.domain || '';
                if (!domain) continue;

                // domain_flag: TRUE si el dominio empieza con "."
                const domainFlag = domain.startsWith('.') ? 'TRUE' : 'FALSE';

                const cPath = c.path || '/';

                // secure_flag: TRUE si secure es true
                const secureFlag = c.secure ? 'TRUE' : 'FALSE';

                // expiration: usar expirationDate o timestamp de 10 años
                let expiry = c.expirationDate;
                if (!expiry || typeof expiry !== 'number') {
                    expiry = Math.floor(Date.now() / 1000) + 10 * 365 * 86400;
                } else {
                    expiry = Math.floor(expiry);
                }

                const name = c.name || '';
                const value = c.value || '';

                // Saltar cookies con nombre vacío
                if (!name) continue;

                // Escapar tabs y newlines en value (Netscape no los tolera)
                const safeValue = String(value).replace(/[\t\n\r]/g, '');

                lines.push(`${domain}\t${domainFlag}\t${cPath}\t${secureFlag}\t${expiry}\t${name}\t${safeValue}`);
            }

            if (lines.length <= 3) {
                console.warn('⚠️ ytdl: no se pudieron convertir cookies (sin entradas válidas)');
                return null;
            }

            // Escribir archivo temporal
            const tmpDir = path.join(TMP_BASE, 'cookies');
            fs.ensureDirSync(tmpDir);
            COOKIES_TEMP_FILE = path.join(tmpDir, `cookies_${Date.now()}.txt`);
            fs.writeFileSync(COOKIES_TEMP_FILE, lines.join('\n') + '\n');
            console.log(`📝 ytdl: cookies convertidas JSON→Netscape (${cookies.length} entradas → ${lines.length - 3} líneas)`);
            return COOKIES_TEMP_FILE;

        } catch (parseErr) {
            console.error('❌ ytdl: error parseando JSON de cookies:', parseErr.message);
            return null;
        }
    }

    // ── Si empieza con #, asumimos que ya es Netscape ──
    if (raw.startsWith('#')) {
        console.log(`📝 ytdl: usando cookies Netscape desde ${srcPath}`);
        return srcPath;
    }

    // ── Formato desconocido, intentar pasar directo y que yt-dlp decida ──
    console.warn(`⚠️ ytdl: formato de cookies no reconocido en ${srcPath}, pasando directo`);
    return srcPath;
}

/** Limpia el archivo temporal de cookies si existe */
function cleanupCookiesTemp() {
    if (COOKIES_TEMP_FILE) {
        try { fs.unlinkSync(COOKIES_TEMP_FILE); } catch (_) {}
        COOKIES_TEMP_FILE = null;
    }
}

// ─── Helpers generales ───────────────────────────────────────────────────

/** Convierte segundos a formato mm:ss o hh:mm:ss */
function fmtDuration(secs) {
    if (!secs || isNaN(secs)) return '?:??';
    const h = Math.floor(secs / 3600);
    const m = Math.floor((secs % 3600) / 60);
    const s = Math.floor(secs % 60);
    if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
    return `${m}:${String(s).padStart(2, '0')}`;
}

/** Formatea bytes a una cadena legible */
function fmtSize(bytes) {
    if (!bytes || bytes <= 0) return '? MB';
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(0)} MB`;
}

/** Crea un directorio temporal único */
async function ensureTmpDir() {
    const dir = path.join(TMP_BASE, crypto.randomBytes(4).toString('hex'));
    await fs.ensureDir(dir);
    return dir;
}

/** Limpia un directorio temporal ignorando errores */
async function cleanupDir(dir) {
    if (dir) await fs.remove(dir).catch(() => {});
}

/**
 * Ejecuta yt-dlp y devuelve stdout.
 * Incluye automáticamente:
 *   - --js-runtimes deno para resolver el n-challenge de YouTube
 *   - --remote-components ejs:github para descargar el script solver
 *   - --cookies si el archivo existe
 * Lanza un error descriptivo si falla.
 */

// ─── Detección de Deno ────────────────────────────────────────────────
// PM2 a veces no hereda el PATH del shell, así que probamos múltiples estrategias.
let DENO_PATH = null;

function findDeno() {
    const { execFileSync } = require('child_process');
    const candidates = [
        // 1. which deno (funciona si PATH incluye ~/.deno/bin)
        () => execFileSync('which', ['deno'], { encoding: 'utf-8', timeout: 5000 }).trim(),
        // 2. Ruta común de instalación por script oficial
        () => execFileSync('test', ['-x', `${require('os').homedir()}/.deno/bin/deno`], { encoding: 'utf-8' })
            && `${require('os').homedir()}/.deno/bin/deno`,
        // 3. Buscar en /home/*/.deno/bin/deno
        () => {
            const homes = execFileSync('ls', ['-d', '/home/*/.deno/bin/deno'], { encoding: 'utf-8', shell: true }).trim().split('\n');
            return homes.find(f => f.length > 0) || null;
        },
        // 4. Deno en /usr/local/bin
        () => execFileSync('test', ['-x', '/usr/local/bin/deno'], { encoding: 'utf-8' }) && '/usr/local/bin/deno',
    ];

    for (const attempt of candidates) {
        try {
            const result = attempt();
            if (result && result.length > 0 && result !== true) {
                return result;
            }
        } catch (_) {}
    }
    return null;
}

try {
    DENO_PATH = findDeno();
    if (DENO_PATH) {
        console.log(`🦕 ytdl: Deno detectado en ${DENO_PATH}`);
    } else {
        console.warn('⚠️ ytdl: Deno no encontrado. YouTube puede no devolver formatos de video.');
        console.warn('   Instalá Deno con: curl -fsSL https://deno.land/install.sh | sh');
        console.warn('   Si ya lo instalaste, reiniciá PM2 con: pm2 restart index');
    }
} catch (_) {
    DENO_PATH = null;
}

const JS_RUNTIME_BASE = DENO_PATH ? `deno:${DENO_PATH}` : null;

async function ytdlp(args) {
    // Preparar argumentos base: siempre remote-components ejs:github
    const baseArgs = ['--remote-components', 'ejs:github'];

    // Agregar --js-runtimes si Deno está disponible
    if (JS_RUNTIME_BASE) {
        baseArgs.push('--js-runtimes', JS_RUNTIME_BASE);
    }

    // Agregar --cookies si existe el archivo
    const cookiesFile = prepareCookies();
    const allArgs = cookiesFile
        ? ['--cookies', cookiesFile, ...baseArgs, ...args]
        : [...baseArgs, ...args];

    try {
        const { stdout } = await execFileP('yt-dlp', allArgs, {
            timeout: 300_000,       // 5 min para descargas grandes
            maxBuffer: 50 * 1024 * 1024
        });
        return stdout;
    } catch (err) {
        const stderr = err.stderr || '';
        const msg = stderr.split('\n').find(l => l.includes('ERROR:')) || err.message;
        throw new Error(msg.slice(0, 400));
    } finally {
        cleanupCookiesTemp();
    }
}

/**
 * Obtiene el info JSON de un video de YouTube.
 */
async function fetchVideoInfo(url) {
    const stdout = await ytdlp([
        '--dump-json',
        '--no-warnings',
        '--no-playlist',
        url
    ]);
    const data = JSON.parse(stdout);

    return {
        id: data.id,
        title: data.title || 'Sin título',
        duration: data.duration || 0,
        channel: data.channel || data.uploader || 'Desconocido',
        thumbnail: data.thumbnail || '',
        webpage_url: data.webpage_url || url,
        formats: data.formats || []
    };
}

/**
 * Busca videos en YouTube usando yt-dlp con ytsearch.
 * Retorna un array de { id, title, url, duration, channel, thumbnail }.
 */
async function searchYoutube(query) {
    const stdout = await ytdlp([
        `ytsearch5:${query}`,
        '--dump-json',
        '--no-warnings',
        '--flat-playlist',
        '--no-playlist',
    ]);
    const lines = stdout.trim().split('\n');
    return lines.map(line => {
        const d = JSON.parse(line);
        return {
            id: d.id,
            title: d.title || 'Sin título',
            url: d.url || d.webpage_url || `https://youtu.be/${d.id}`,
            duration: d.duration || 0,
            channel: d.channel || d.uploader || 'Desconocido',
            thumbnail: d.thumbnail || ''
        };
    });
}

/**
 * Agrupa formatos por altura, eligiendo el mejor de cada grupo.
 * Devuelve un array de { height, label, formatSpec, sizeBytes } ordenado descendente.
 * Al final añade la opción "Solo audio".
 */
function buildFormatOptions(info) {
    const { formats, duration } = info;
    const usedTiers = new Set();

    // Filtrar formatos que tengan video
    const videoFormats = formats.filter(f =>
        f.vcodec && f.vcodec !== 'none' && f.height && f.height >= 144
    );

    const results = [];

    for (const tier of RES_TIERS) {
        const candidates = videoFormats.filter(f =>
            f.height <= tier && f.height > tier - 80 && !usedTiers.has(f.height)
        );
        if (candidates.length === 0) continue;

        const best = candidates.reduce((best, current) => {
            const bestScore = (best.acodec && best.acodec !== 'none' ? 1000 : 0) + (best.tbr || 0);
            const curScore = (current.acodec && current.acodec !== 'none' ? 1000 : 0) + (current.tbr || 0);
            return curScore > bestScore ? current : best;
        });
        usedTiers.add(best.height);

        // Estimar tamaño
        const audioFormats = formats.filter(f =>
            f.acodec && f.acodec !== 'none' && (!f.vcodec || f.vcodec === 'none')
        );
        const bestAudio = audioFormats.reduce((a, b) => ((a.tbr || 0) > (b.tbr || 0) ? a : b), { tbr: 0 });
        const totalBitrate = (best.tbr || 0) + (bestAudio.tbr || 0);
        const estBytes = totalBitrate > 0 && duration > 0
            ? Math.round(totalBitrate * 1000 / 8 * duration)
            : best.filesize || best.filesize_approx || 0;

        const hasAudio = best.acodec && best.acodec !== 'none';
        const fmtSpec = hasAudio
            ? `best[height<=${best.height}]`
            : `bestvideo[height<=${best.height}]+bestaudio/best[height<=${best.height}]`;

        const label = best.height >= 2160 ? `${best.height}p (4K)` :
                      best.height >= 1440 ? `${best.height}p (2K)` :
                      `${best.height}p`;

        results.push({ height: best.height, label, formatSpec: fmtSpec, sizeBytes: estBytes });
    }

    // Opción de solo audio
    const audioFormats = formats.filter(f =>
        f.acodec && f.acodec !== 'none' && (!f.vcodec || f.vcodec === 'none')
    );
    const bestAudio = audioFormats.reduce((a, b) => ((a.tbr || 0) > (b.tbr || 0) ? a : b), { tbr: 0 });
    const audioSize = bestAudio.tbr > 0 && duration > 0
        ? Math.round(bestAudio.tbr * 1000 / 8 * duration)
        : 0;
    results.push({
        height: 0,
        label: '🎵 Solo audio (MP3)',
        formatSpec: 'bestaudio/best',
        sizeBytes: audioSize,
        isAudio: true
    });

    return results;
}

/**
 * Descarga un video/audio usando yt-dlp.
 * Retorna la ruta al archivo descargado.
 */
async function downloadMedia(url, formatSpec, outputDir) {
    const outputTemplate = path.join(outputDir, '%(title)s.%(ext)s');

    await ytdlp([
        '-f', formatSpec,
        '--merge-output-format', 'mp4',
        '--no-warnings',
        '--no-playlist',
        '--no-part',
        '--no-mtime',
        '-o', outputTemplate,
        url
    ]);

    const files = await fs.readdir(outputDir);
    const videoFile = files.find(f => f !== '.' && f !== '..' && !f.startsWith('.'));
    if (!videoFile) throw new Error('No se encontró el archivo descargado.');
    return path.join(outputDir, videoFile);
}

// ─── Interfaz de texto para el menú de formatos ─────────────────────────

function buildMenuText(info, options) {
    const lines = [];
    lines.push('╭━━━━━━━━━━━━━━━━━━━━');
    lines.push(`┃ 📹 *${info.title}*`);
    lines.push(`┃ ⏱  ${fmtDuration(info.duration)}`);
    lines.push(`┃ 📺 ${info.channel}`);
    lines.push('╰━━━━━━━━━━━━━━━━━━━━');
    lines.push('');
    lines.push('*Selecciona resolución:*');
    lines.push('');

    options.forEach((opt, i) => {
        const num = i + 1;
        const size = fmtSize(opt.sizeBytes);
        if (opt.isAudio) {
            lines.push(`${num} 〉🎵 *${opt.label}*  — ${size}`);
        } else {
            const warn = opt.sizeBytes > WARN_SIZE ? ' ⚠️' : '';
            lines.push(`${num} 〉*${opt.label}*  — ${size}${warn}`);
        }
    });

    lines.push('');
    lines.push('Responde con:  `.ytdl <núm>`  (ej: `.ytdl 3`)');
    lines.push('Agrega `c` al final para comprimir en ZIP: `.ytdl 3 c`');
    lines.push('`.ytdl cancel` para salir');

    return lines.join('\n');
}

// ─── Menú de resultados de búsqueda ────────────────────────────────────

function buildSearchMenu(results) {
    const lines = [];
    lines.push('🔍 *Resultados de búsqueda:*');
    lines.push('');

    results.forEach((r, i) => {
        const num = i + 1;
        const dur = r.duration ? `⏱ ${fmtDuration(r.duration)}` : '';
        lines.push(`${num}. *${r.title}*`);
        lines.push(`   📺 ${r.channel}  ${dur}`);
    });

    lines.push('');
    lines.push('Responde con:  `.ytdl <núm>`  (ej: `.ytdl 2`)');
    lines.push('`.ytdl cancel` para salir');
    lines.push('_(Búsqueda expira en 10 min)_');

    return lines.join('\n');
}

// ─── Enviar menú con miniatura (si está disponible) ─────────────────────

async function sendMenuWithThumbnail(sock, jid, menuText, thumbnailUrl, quotedMsg) {
    if (thumbnailUrl) {
        try {
            const axios = require('axios');
            const resp = await axios.get(thumbnailUrl, { responseType: 'arraybuffer', timeout: 8000 });
            const thumbBuffer = Buffer.from(resp.data);
            await sock.sendMessage(jid, {
                image: thumbBuffer,
                caption: menuText
            }, { quoted: quotedMsg });
            return;
        } catch (_) {
            // Fallback: enviar solo texto si falla la miniatura
        }
    }
    await sock.sendMessage(jid, { text: menuText }, { quoted: quotedMsg });
}

// ─── Auto-actualización de yt-dlp ────────────────────────────────────────
// Corre al inicio (con retardo) y luego cada 24 h. No bloquea el bot.
const YT_DLP_UPDATE_INTERVAL = 24 * 60 * 60 * 1000;
let ytdlpUpdateTimer = null;

async function autoUpdateYtDlp() {
    try {
        const { stdout } = await execFileP('yt-dlp', ['-U'], { timeout: 120000 });
        const out = (stdout || '').trim();
        if (/pip or using the wheel/i.test(out)) {
            // Instalado vía pip/wheel: el self-updater no funciona
            await execFileP('python3', ['-m', 'pip', 'install', '--user', '--break-system-packages', '-U', 'yt-dlp'], { timeout: 180000 });
            console.log('⬆️ ytdl: yt-dlp actualizado vía pip.');
        } else {
            console.log(`⬆️ ytdl: ${out.split('\n').pop() || 'yt-dlp verificado'}`);
        }
    } catch (e) {
        console.warn('⚠️ ytdl: auto-actualización falló (no crítico):', e.message);
    }
}

function scheduleYtDlpUpdates() {
    if (ytdlpUpdateTimer) clearTimeout(ytdlpUpdateTimer);
    setTimeout(() => {
        autoUpdateYtDlp();
        ytdlpUpdateTimer = setInterval(autoUpdateYtDlp, YT_DLP_UPDATE_INTERVAL);
    }, 60 * 1000).unref();
}

// ─── Handler principal ───────────────────────────────────────────────────

module.exports = {
    // ytdlp agregado como alias para quienes escriben con 'p' al final
    commands: ['ytdl', 'ytdlp', 'yt', 'youtube'],

    init: () => {
        scheduleYtDlpUpdates();
    },

    handler: async (sock, msg, args, store) => {
        const jid = msg.key.remoteJid;
        const sender = msg.key.participant || jid;
        const pushName = msg.pushName || 'Usuario';

        // Extraer flag de compresión 'c' / 'zip' / 'comp' de los args
        const compressFlag = args.some(a => a === 'c' || a === 'zip' || a === 'comp');
        const cleanArgs = args.filter(a => a !== 'c' && a !== 'zip' && a !== 'comp');

        // Log de depuración: mostrar quién llamó y con qué args
        console.log(`📹 ytdl — llamado por ${pushName} (${sender}) args:`, JSON.stringify(args), compressFlag ? '[COMPRIMIR]' : '');

        try {
            // ─── CASO 1: Cancelar sesión ────────────────────────────────
            if (cleanArgs[0]?.toLowerCase() === 'cancel') {
                const hadSession = pendingSessions.delete(sender) | pendingSearch.delete(sender);
                if (hadSession) {
                    console.log(`📹 ytdl: sesión cancelada por ${pushName}`);
                    await sock.sendMessage(jid, { text: '✅ Descarga/Búsqueda cancelada.' }, { quoted: msg });
                } else {
                    await sock.sendMessage(jid, { text: '❌ No tienes ninguna descarga activa.' }, { quoted: msg });
                }
                return;
            }

            // ─── CASO 2: Selección numérica (sesión activa) ──────────────
            const selection = parseInt(cleanArgs[0], 10);
            if (!isNaN(selection) && pendingSessions.has(sender)) {
                const session = pendingSessions.get(sender);
                const { info, options } = session;

                if (selection < 1 || selection > options.length) {
                    console.log(`📹 ytdl: selección inválida ${selection} (rango: 1-${options.length})`);
                    await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
                    await sock.sendMessage(jid, {
                        text: `❌ Número inválido. Elige entre 1 y ${options.length}.\nUsa \`.ytdl cancel\` para salir.`
                    }, { quoted: msg });
                    return;
                }

                const selected = options[selection - 1];
                pendingSessions.delete(sender);

                console.log(`📹 ytdl: ${pushName} seleccionó #${selection} → ${selected.label} (${selected.formatSpec})`);
                await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

                // ── Descargar ──
                const tmpDir = await ensureTmpDir();
                try {
                    const downloadedPath = await downloadMedia(
                        info.webpage_url,
                        selected.formatSpec,
                        tmpDir
                    );
                    const stat = await fs.stat(downloadedPath);
                    const fileSize = stat.size;
                    console.log(`📹 ytdl: descarga completada → ${path.basename(downloadedPath)} (${fmtSize(fileSize)})`);

                    let sendPath = downloadedPath;
                    let isCompressed = false;

                    // ── Si el usuario pidió compresión ZIP ──
                    if (compressFlag) {
                        console.log(`📹 ytdl: comprimiendo en ZIP (máxima compresión)...`);
                        const zipPath = downloadedPath + '.zip';
                        await execFileP('zip', ['-9', '-j', zipPath, downloadedPath]);
                        const zipStat = await fs.stat(zipPath);
                        console.log(`📹 ytdl: ZIP creado → ${fmtSize(zipStat.size)} (original: ${fmtSize(fileSize)})`);
                        sendPath = zipPath;
                        isCompressed = true;
                    }

                    const fileBuffer = await fs.readFile(sendPath);
                    const ext = path.extname(sendPath).toLowerCase();

                    // ── Enviar según tipo ──
                    if (isCompressed) {
                        // ZIP → siempre como documento
                        await sock.sendMessage(jid, {
                            document: fileBuffer,
                            mimetype: 'application/zip',
                            fileName: `video_${info.id}.zip`
                        }, { quoted: msg });
                    } else if (selected.isAudio) {
                        await sock.sendMessage(jid, {
                            audio: fileBuffer,
                            mimetype: 'audio/mpeg',
                            ptt: false
                        }, { quoted: msg });
                    } else if (ext === '.mp4' || ext === '.webm' || ext === '.mkv') {
                        const actualExt = ext === '.mkv' || ext === '.webm' ? '.mp4' : ext;

                        // Si el archivo es muy grande para video → enviar como documento
                        if (fileSize > WARN_SIZE) {
                            console.log(`📹 ytdl: archivo grande (${fmtSize(fileSize)}), enviando como documento`);
                            await sock.sendMessage(jid, {
                                document: fileBuffer,
                                mimetype: `video/${actualExt.replace('.', '')}`,
                                fileName: `video_${info.id}.${actualExt.replace('.', '')}`
                            }, { quoted: msg });
                        } else {
                            await sock.sendMessage(jid, {
                                video: fileBuffer,
                                caption: `📹 ${info.title}`,
                                mimetype: `video/${actualExt.replace('.', '')}`
                            }, { quoted: msg });
                        }
                    } else {
                        await sock.sendMessage(jid, {
                            document: fileBuffer,
                            mimetype: 'application/octet-stream',
                            fileName: path.basename(sendPath)
                        }, { quoted: msg });
                    }

                    console.log(`📹 ytdl: video enviado a ${jid}`);
                    await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
                } catch (dlErr) {
                    console.error('❌ ytdl: error en descarga:', dlErr.message);
                    await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                    await sock.sendMessage(jid, {
                        text: `❌ Error descargando: ${dlErr.message.slice(0, 250)}`
                    }, { quoted: msg });
                } finally {
                    await cleanupDir(tmpDir);
                }
                return;
            }

            // ─── CASO 2b: Selección de resultado de búsqueda ─────────────
            if (!isNaN(selection) && pendingSearch.has(sender)) {
                const searchData = pendingSearch.get(sender);
                const { results } = searchData;

                if (selection < 1 || selection > results.length) {
                    console.log(`📹 ytdl: selección búsqueda inválida ${selection} (rango: 1-${results.length})`);
                    await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
                    await sock.sendMessage(jid, {
                        text: '❌ Número inválido. Elige entre 1 y ' + results.length + '.\nUsa `.ytdl cancel` para salir.'
                    }, { quoted: msg });
                    return;
                }

                const selectedResult = results[selection - 1];
                pendingSearch.delete(sender);

                console.log(`📹 ytdl: ${pushName} seleccionó búsqueda #${selection} → "${selectedResult.title}"`);
                await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

                // Obtener info del video seleccionado
                const info = await fetchVideoInfo(selectedResult.url);
                const options = buildFormatOptions(info);

                if (options.length === 0) {
                    await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                    await sock.sendMessage(jid, {
                        text: '❌ No se encontraron formatos descargables para este video.'
                    }, { quoted: msg });
                    return;
                }

                pendingSessions.set(sender, { info, options, createdAt: Date.now() });
                const menuText = buildMenuText(info, options);
                await sendMenuWithThumbnail(sock, jid, menuText, info.thumbnail, msg);
                await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
                console.log(`📹 ytdl: menú enviado a ${pushName} — esperando selección`);
                return;
            }

            // ─── CASO 3: Nueva descarga o búsqueda (primer paso) ─────────
            const url = cleanArgs.join(' ');
            if (!url) {
                console.log('📹 ytdl: uso sin args — mostrando ayuda');
                await sock.sendMessage(jid, {
                    text: [
                        '📹 *YouTube / TikTok / X (Twitter) Downloader*',
                        '',
                        'Uso:',
                        '  `!ytdl <url>` — Lista resoluciones disponibles (YouTube, TikTok, X/Twitter, etc.)',
                        '  `!ytdl <núm>` — Descarga la opción seleccionada',
                        '  `!ytdl cancel` — Cancela la descarga actual',
                        '  `!ytdl c <url>` — Descarga + comprime en ZIP',
                        '  `!ytdl <núm> c` — Selección + comprime en ZIP',
                        '  `!ytdl <query>` — Buscar videos en YouTube',
                        '',
                        'Ejemplos:',
                        '  `!ytdl https://youtube.com/watch?v=xxx`',
                        '  `!ytdl https://www.tiktok.com/@user/video/xxx`',
                        '  `!ytdl https://x.com/user/status/xxx`',
                        '  `!ytdl 3`  (selecciona la opción 3)',
                        '  `!ytdl c https://youtu.be/xxx`  (comprimido)',
                        '  `!ytdl curso de python`  (búsqueda)',
                    ].join('\n')
                }, { quoted: msg });
                await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
                return;
            }

            // Si NO es una URL directa (http/https) → tratar como búsqueda de YouTube.
            // Cualquier enlace directo (YouTube, TikTok, X/Twitter, etc.) se descarga vía yt-dlp.
            const urlLower = url.toLowerCase().trim();
            const startsWithHttp = urlLower.startsWith('http://') || urlLower.startsWith('https://');
            const isDirectUrl = startsWithHttp;

            if (!isDirectUrl) {
                console.log(`📹 ytdl: buscando: "${url}"`);
                await sock.sendMessage(jid, { react: { text: '🔍', key: msg.key } });

                try {
                    const results = await searchYoutube(url);
                    if (results.length === 0) {
                        await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                        await sock.sendMessage(jid, {
                            text: '❌ No se encontraron resultados para esa búsqueda.'
                        }, { quoted: msg });
                        return;
                    }
                    pendingSearch.set(sender, { results, createdAt: Date.now() });
                    const searchText = buildSearchMenu(results);
                    await sock.sendMessage(jid, { text: searchText }, { quoted: msg });
                    await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
                    console.log(`📹 ytdl: ${results.length} resultados mostrados a ${pushName}`);
                } catch (searchErr) {
                    console.error('❌ ytdl: error en búsqueda:', searchErr.message);
                    await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                    await sock.sendMessage(jid, {
                        text: `❌ Error en la búsqueda: ${searchErr.message.slice(0, 200)}`
                    }, { quoted: msg });
                }
                return;
            }

            // Verificar cookies antes de empezar
            const cookiesAvailable = COOKIES_CANDIDATES.some(f => fs.existsSync(f));
            if (!cookiesAvailable) {
                console.log('📹 ytdl: sin cookies — advirtiendo al usuario');
                await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });
                await sock.sendMessage(jid, {
                    text: '⚠️ No se encontró archivo de cookies.\nVideos restringidos pueden fallar.\nGuía: coloca `assets/www.youtube.com_cookies.json` con cookies exportadas (formato JSON o Netscape).'
                }, { quoted: msg });
            } else {
                console.log('📹 ytdl: cookies encontradas, procesando...');
                await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });
            }

            // Obtener info del video
            console.log(`📹 ytdl: obteniendo info de: ${url}`);
            const info = await fetchVideoInfo(url);
            console.log(`📹 ytdl: info obtenida → "${info.title}" (${fmtDuration(info.duration)})`);

            const options = buildFormatOptions(info);
            console.log(`📹 ytdl: ${options.length} opciones de formato generadas`);

            if (options.length === 0) {
                await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                await sock.sendMessage(jid, {
                    text: '❌ No se encontraron formatos descargables para este video.'
                }, { quoted: msg });
                return;
            }

            // Guardar sesión
            pendingSessions.set(sender, {
                info,
                options,
                createdAt: Date.now()
            });

            // Enviar menú de selección
            const menuText = buildMenuText(info, options);
            await sendMenuWithThumbnail(sock, jid, menuText, info.thumbnail, msg);
            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
            console.log(`📹 ytdl: menú enviado a ${pushName} — esperando selección`);

        } catch (err) {
            console.error('❌ ytdl: ERROR GENERAL:', err.message);

            // Limpiar sesiones si existen
            if (sender) {
                pendingSessions.delete(sender);
                pendingSearch.delete(sender);
            }
            if (COOKIES_TEMP_FILE) cleanupCookiesTemp();

            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });

            const errMsg = err.message || 'Error desconocido';

            // Mensajes específicos según el error
            if (errMsg.includes('Cookies file must be Netscape')) {
                await sock.sendMessage(jid, {
                    text: '❌ El archivo de cookies tiene formato incorrecto.\nAsegúrate de usar la extensión "cookies_localy.txt" que exporta en formato Netscape, o coloca el archivo JSON y el bot lo convertirá automáticamente.'
                }, { quoted: msg });
            } else if (errMsg.includes('HTTP Error 403') || errMsg.includes('private') || errMsg.includes('Private video')) {
                await sock.sendMessage(jid, {
                    text: '❌ El video es privado o requiere inicio de sesión.\nExporta tus cookies con la extensión "cookies_localy.txt" y colócalas en `assets/www.youtube.com_cookies.json`'
                }, { quoted: msg });
            } else if (errMsg.includes('n challenge solving failed') || errMsg.includes('Requested format is not available')) {
                const denoHint = DENO_PATH
                    ? '⚠️ El n-challenge de YouTube falló incluso con Deno.'
                    : '❌ YouTube requiere Deno para resolver el n-challenge.\n   Instalá Deno en el servidor:\n   curl -fsSL https://deno.land/install.sh | sh\n   Después reiniciá PM2: pm2 restart index\n   O usá !restart en WhatsApp si el bot está vivo.';
                await sock.sendMessage(jid, {
                    text: `❌ YouTube bloqueó la descarga.\n${denoHint}`
                }, { quoted: msg });
            } else if (errMsg.includes('No video results') || errMsg.includes('Video unavailable')) {
                await sock.sendMessage(jid, {
                    text: '❌ Video no disponible. Puede haber sido eliminado o restringido en tu región.'
                }, { quoted: msg });
            } else if (errMsg.includes('UNICODE') || errMsg.includes('Emoji')) {
                await sock.sendMessage(jid, {
                    text: '❌ Error con caracteres especiales en el título. Prueba con otro video.'
                }, { quoted: msg });
            } else if (errMsg.includes('Premature close') || errMsg.includes('ECONNRESET') || errMsg.includes('ETIMEDOUT')) {
                await sock.sendMessage(jid, {
                    text: '❌ Error de conexión con YouTube. Reintenta en unos segundos.'
                }, { quoted: msg });
            } else {
                await sock.sendMessage(jid, {
                    text: `❌ Error: ${errMsg.slice(0, 300)}`
                }, { quoted: msg });
            }
        }
    }
};
