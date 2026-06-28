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
// Map<senderJid -> { info, options, createdAt }>
const pendingSessions = new Map();

const SESSION_TTL_MS = 10 * 60 * 1000; // 10 minutos

// Limpieza periódica de sesiones expiradas
setInterval(() => {
    const now = Date.now();
    for (const [jid, session] of pendingSessions) {
        if (now - session.createdAt > SESSION_TTL_MS) pendingSessions.delete(jid);
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
    lines.push('`.ytdl cancel` para salir');

    return lines.join('\n');
}

// ─── Handler principal ───────────────────────────────────────────────────

module.exports = {
    // ytdlp agregado como alias para quienes escriben con 'p' al final
    commands: ['ytdl', 'ytdlp', 'yt', 'youtube'],

    handler: async (sock, msg, args, store) => {
        const jid = msg.key.remoteJid;
        const sender = msg.key.participant || jid;
        const pushName = msg.pushName || 'Usuario';

        // Log de depuración: mostrar quién llamó y con qué args
        console.log(`📹 ytdl — llamado por ${pushName} (${sender}) args:`, JSON.stringify(args));

        try {
            // ─── CASO 1: Cancelar sesión ────────────────────────────────
            if (args[0]?.toLowerCase() === 'cancel') {
                if (pendingSessions.has(sender)) {
                    pendingSessions.delete(sender);
                    console.log(`📹 ytdl: sesión cancelada por ${pushName}`);
                    await sock.sendMessage(jid, { text: '✅ Descarga cancelada.' }, { quoted: msg });
                } else {
                    await sock.sendMessage(jid, { text: '❌ No tienes ninguna descarga activa.' }, { quoted: msg });
                }
                return;
            }

            // ─── CASO 2: Selección numérica (sesión activa) ──────────────
            const selection = parseInt(args[0], 10);
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

                    const fileBuffer = await fs.readFile(downloadedPath);
                    const ext = path.extname(downloadedPath).toLowerCase();

                    // ── Enviar según tipo ──
                    if (selected.isAudio) {
                        await sock.sendMessage(jid, {
                            audio: fileBuffer,
                            mimetype: 'audio/mpeg',
                            ptt: false
                        }, { quoted: msg });
                    } else if (ext === '.mp4' || ext === '.webm' || ext === '.mkv') {
                        const actualExt = ext === '.mkv' || ext === '.webm' ? '.mp4' : ext;

                        if (fileSize > WARN_SIZE) {
                            await sock.sendMessage(jid, {
                                text: `⚠️ El archivo pesa ${fmtSize(fileSize)}. WhatsApp puede rechazar archivos >64 MB.`
                            }, { quoted: msg });
                        }

                        await sock.sendMessage(jid, {
                            video: fileBuffer,
                            caption: `📹 ${info.title}`,
                            mimetype: `video/${actualExt.replace('.', '')}`
                        }, { quoted: msg });
                    } else {
                        await sock.sendMessage(jid, {
                            document: fileBuffer,
                            mimetype: 'application/octet-stream',
                            fileName: path.basename(downloadedPath)
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

            // ─── CASO 3: Nueva descarga (primer paso) ────────────────────
            const url = args[0];
            if (!url) {
                console.log('📹 ytdl: uso sin args — mostrando ayuda');
                await sock.sendMessage(jid, {
                    text: [
                        '📹 *YouTube Downloader*',
                        '',
                        'Uso:',
                        '  `!ytdl <url>` — Lista resoluciones disponibles',
                        '  `!ytdl <núm>` — Descarga la opción seleccionada',
                        '  `!ytdl cancel` — Cancela la descarga actual',
                        '',
                        'Ejemplos:',
                        '  `!ytdl https://youtube.com/watch?v=xxx`',
                        '  `!ytdl 3`  (selecciona la opción 3)',
                    ].join('\n')
                }, { quoted: msg });
                await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
                return;
            }

            // Validar que parezca una URL de YouTube
            const urlLower = url.toLowerCase();
            if (!urlLower.includes('youtube.com') && !urlLower.includes('youtu.be') &&
                !urlLower.includes('m.youtube.com') && !urlLower.includes('youtube-nocookie.com')) {
                console.log(`📹 ytdl: URL inválida: ${url}`);
                await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
                await sock.sendMessage(jid, {
                    text: '❌ Eso no parece un enlace de YouTube válido.\nAsegúrate de incluir el `https://`.'
                }, { quoted: msg });
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
            await sock.sendMessage(jid, { text: menuText }, { quoted: msg });
            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
            console.log(`📹 ytdl: menú enviado a ${pushName} — esperando selección`);

        } catch (err) {
            console.error('❌ ytdl: ERROR GENERAL:', err.message);

            // Limpiar sesión si existe
            if (sender) pendingSessions.delete(sender);
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
