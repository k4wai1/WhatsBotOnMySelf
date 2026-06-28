// addons/ytdl.js — YouTube Downloader interactivo
// Dependencia externa: yt-dlp (instalado en el sistema)
// Cookies: assets/www.youtube.com_cookies.json (exportadas con extensión cookies_localy.txt)
// ────────────────────────────────────────────────────────────────────────────

const { execFile } = require('child_process');
const fs = require('fs-extra');
const path = require('path');
const crypto = require('crypto');
const util = require('util');

const execFileP = util.promisify(execFile);

// ─── Constantes ──────────────────────────────────────────────────────────

const COOKIES_PATH = path.join(__dirname, '..', 'assets', 'www.youtube.com_cookies.json');
const TMP_BASE = '/tmp/ytdl';

// Umbral de tamaño: si el archivo estimado supera esto, se advierte (en bytes)
const WARN_SIZE = 60 * 1024 * 1024; // 60 MB

// Resoluciones objetivo en píxeles de alto (ordenadas de mayor a menor)
const RES_TIERS = [4320, 2160, 1440, 1080, 720, 480, 360, 240, 144];

// ─── Estado de sesiones interactivas ─────────────────────────────────────
// Map<senderJid -> { url, title, duration, channel, thumb, formats, estimatedSizes, createdAt }>
const pendingSessions = new Map();

const SESSION_TTL_MS = 10 * 60 * 1000; // 10 minutos

// Limpieza periódica de sesiones expiradas
setInterval(() => {
    const now = Date.now();
    for (const [jid, session] of pendingSessions) {
        if (now - session.createdAt > SESSION_TTL_MS) pendingSessions.delete(jid);
    }
}, 60_000);

// ─── Helpers ─────────────────────────────────────────────────────────────

/** Devuelve el argumento de cookies si el archivo existe */
function cookiesArg() {
    return fs.existsSync(COOKIES_PATH) ? ['--cookies', COOKIES_PATH] : [];
}

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

/** Escapa caracteres problemáticos para nombre de archivo */
function safeFilename(str) {
    return String(str).replace(/[<>:"/\\|?*]/g, '_').slice(0, 120);
}

/** Crea un directorio temporal único */
async function ensureTmpDir() {
    const dir = path.join(TMP_BASE, crypto.randomBytes(4).toString('hex'));
    await fs.ensureDir(dir);
    return dir;
}

/**
 * Extrae el prefijo real usado en el mensaje original.
 * Examina el texto del mensaje para detectar con qué prefijo (,, !, /) fue invocado.
 */
function detectPrefix(msg) {
    const text = msg.message?.conversation ||
                 msg.message?.extendedTextMessage?.text || '';
    const match = text.match(/^([,.!\/])\s*/);
    return match ? match[1] : ','; // fallback visible
}

/** Limpia un directorio temporal ignorando errores */
async function cleanupDir(dir) {
    if (dir) await fs.remove(dir).catch(() => {});
}

/**
 * Ejecuta yt-dlp y devuelve stdout.
 * Lanza un error descriptivo si falla.
 */
async function ytdlp(args) {
    const allArgs = [...args, ...cookiesArg()];
    try {
        const { stdout } = await execFileP('yt-dlp', allArgs, { timeout: 120_000, maxBuffer: 10 * 1024 * 1024 });
        return stdout;
    } catch (err) {
        // Extraer mensaje de error de yt-dlp si es posible
        const stderr = err.stderr || '';
        const msg = stderr.split('\n').find(l => l.includes('ERROR:')) || err.message;
        throw new Error(msg.slice(0, 300));
    }
}

/**
 * Obtiene el info JSON de un video de YouTube.
 * Retorna { id, title, duration, channel, thumbnail, formats, webpage_url }.
 */
async function fetchVideoInfo(url) {
    const stdout = await ytdlp([
        '--dump-json',
        '--no-warnings',
        '--no-playlist',      // Evita listas de reproducción
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
        // Buscar formatos en este tier (con tolerancia de 50px hacia abajo)
        const candidates = videoFormats.filter(f =>
            f.height <= tier && f.height > tier - 80 && !usedTiers.has(f.height)
        );
        if (candidates.length === 0) continue;

        // Marcar esta altura como usada
        const best = candidates.reduce((a, b) => {
            // Preferir formatos con audio incluido, luego mayor bitrate
            const aScore = (a.acodec && a.acodec !== 'none' ? 1000 : 0) + (a.tbr || 0);
            const bScore = (b.acodec && b.acodec !== 'none' ? 1000 : 0) + (b.tbr || 0);
            return bScore - aScore;
        });
        usedTiers.add(best.height);

        // Estimar tamaño: sumar video + mejor audio posible
        const audioFormats = formats.filter(f =>
            f.acodec && f.acodec !== 'none' && (!f.vcodec || f.vcodec === 'none')
        );
        const bestAudio = audioFormats.reduce((a, b) => ((a.tbr || 0) > (b.tbr || 0) ? a : b), { tbr: 0 });
        const totalBitrate = (best.tbr || 0) + (bestAudio.tbr || 0);
        const estBytes = totalBitrate > 0 && duration > 0
            ? Math.round(totalBitrate * 1000 / 8 * duration)
            : best.filesize || best.filesize_approx || 0;

        // Construir format spec limpio para yt-dlp
        const hasAudio = best.acodec && best.acodec !== 'none';
        const fmtSpec = hasAudio
            ? `best[height<=${best.height}]`
            : `bestvideo[height<=${best.height}]+bestaudio/best[height<=${best.height}]`;

        // Etiqueta humana
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
        '--no-part',           // No usar archivos .part
        '--no-mtime',
        '-o', outputTemplate,
        url
    ]);

    // Encontrar el archivo descargado (yt-dlp crea un archivo con el título del video)
    const files = await fs.readdir(outputDir);
    const videoFile = files.find(f => f !== '.' && f !== '..' && !f.startsWith('.'));
    if (!videoFile) throw new Error('No se encontró el archivo descargado.');
    return path.join(outputDir, videoFile);
}

// ─── Interfaz de texto para el menú de formatos ─────────────────────────

function buildMenuText(info, options, prefix) {
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
    lines.push(`Responde con:  ${prefix}ytdl <núm>`);
    lines.push(`Ejemplo: ${prefix}ytdl 3  (para 1080p)`);
    lines.push(`${prefix}ytdl cancel para salir`);

    return lines.join('\n');
}

// ─── Handler principal ───────────────────────────────────────────────────

module.exports = {
    commands: ['ytdl', 'ytdlp', 'yt', 'youtube'],

    handler: async (sock, msg, args, store) => {
        const jid = msg.key.remoteJid;
        // En grupos el sender es el participant; en privado es el remoteJid
        const sender = msg.key.participant || jid;
        const pushName = msg.pushName || 'Usuario';
        const prefix = detectPrefix(msg);

        console.log(`📹 ytdl — llamado por ${pushName} (${sender}) args: [${args.join(', ')}]`);

        try {
            // ─── CASO 1: Cancelar sesión ────────────────────────────────
            if (args[0]?.toLowerCase() === 'cancel') {
                if (pendingSessions.has(sender)) {
                    pendingSessions.delete(sender);
                    console.log(`🚫 ytdl — ${pushName} canceló la descarga`);
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
                    await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
                    await sock.sendMessage(jid, {
                        text: `❌ Número inválido. Elige entre 1 y ${options.length}.\nUsa "${prefix}ytdl cancel" para salir.`
                    }, { quoted: msg });
                    return;
                }

                const selected = options[selection - 1];
                pendingSessions.delete(sender); // Consumir la sesión
                console.log(`📥 ytdl — ${pushName} seleccionó opción ${selection}: ${selected.label} — ${info.title}`);

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

                    // ── Leer el buffer ──
                    const fileBuffer = await fs.readFile(downloadedPath);
                    const ext = path.extname(downloadedPath).toLowerCase();

                    // ── Enviar según tipo ──
                    if (selected.isAudio) {
                        // Enviar como audio
                        await sock.sendMessage(jid, {
                            audio: fileBuffer,
                            mimetype: 'audio/mpeg',
                            ptt: false
                        }, { quoted: msg });
                    } else if (ext === '.mp4' || ext === '.webm' || ext === '.mkv') {
                        // Convertir a mp4 si es necesario (yt-dlp ya mergea a mp4)
                        const actualExt = ext === '.mkv' || ext === '.webm' ? '.mp4' : ext;
                        let sendBuffer = fileBuffer;

                        // Si el archivo es muy grande, advertir
                        if (fileSize > WARN_SIZE) {
                            await sock.sendMessage(jid, {
                                text: `⚠️ El archivo pesa ${fmtSize(fileSize)}. WhatsApp puede rechazar archivos >64 MB.`
                            }, { quoted: msg });
                        }

                        await sock.sendMessage(jid, {
                            video: sendBuffer,
                            caption: `📹 ${info.title}`,
                            mimetype: `video/${actualExt.replace('.', '')}`
                        }, { quoted: msg });
                    } else {
                        // Enviar como documento genérico
                        await sock.sendMessage(jid, {
                            document: fileBuffer,
                            mimetype: `application/octet-stream`,
                            fileName: path.basename(downloadedPath)
                        }, { quoted: msg });
                    }

                    console.log(`✅ ytdl — ${pushName}: video enviado (${selected.label}, ${fmtSize(fileSize)}) — ${info.title}`);
                    await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
                } catch (dlErr) {
                    console.error('Error en descarga ytdl:', dlErr);
                    await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                    await sock.sendMessage(jid, {
                        text: `❌ Error descargando: ${dlErr.message.slice(0, 200)}`
                    }, { quoted: msg });
                } finally {
                    await cleanupDir(tmpDir);
                }
                return;
            }

            // ─── CASO 3: Nueva descarga (primer paso) ────────────────────
            const url = args[0];
            if (!url) {
                await sock.sendMessage(jid, {
                    text: [
                        '📹 *YouTube Downloader*',
                        '',
                        'Uso:',
                        `  ${prefix}ytdl <url> — Lista resoluciones disponibles`,
                        `  ${prefix}ytdl <núm> — Descarga la opción seleccionada`,
                        `  ${prefix}ytdl cancel — Cancela la descarga actual`,
                        '',
                        'Ejemplos:',
                        `  ${prefix}ytdl https://youtube.com/watch?v=xxx`,
                        `  ${prefix}ytdl 3  (selecciona la opción 3)`,
                    ].join('\n')
                }, { quoted: msg });
                await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
                return;
            }

            // Validar que parezca una URL de YouTube
            const urlLower = url.toLowerCase();
            if (!urlLower.includes('youtube.com') && !urlLower.includes('youtu.be') &&
                !urlLower.includes('m.youtube.com') && !urlLower.includes('youtube-nocookie.com')) {
                await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
                await sock.sendMessage(jid, {
                    text: '❌ Eso no parece un enlace de YouTube válido.\nAsegúrate de incluir el `https://`.'
                }, { quoted: msg });
                return;
            }

            // Informar sobre cookies si no existen
            if (!fs.existsSync(COOKIES_PATH)) {
                await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });
                await sock.sendMessage(jid, {
                    text: '⚠️ No se encontró `assets/www.youtube.com_cookies.json`.\nVideos restringidos por edad/región pueden fallar.'
                }, { quoted: msg });
            } else {
                await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });
            }

            // Obtener info del video
            const info = await fetchVideoInfo(url);
            const options = buildFormatOptions(info);

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

            console.log(`📋 ytdl — ${pushName}: menú enviado con ${options.length} opciones para: ${info.title}`);
            // Enviar menú de selección
            const menuText = buildMenuText(info, options, prefix);
            await sock.sendMessage(jid, { text: menuText }, { quoted: msg });
            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

        } catch (err) {
            console.error('Error en addon ytdl:', err);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
            // Limpiar sesión si existe
            if (sender) pendingSessions.delete(sender);

            // Mensaje de error amigable
            const errMsg = err.message || 'Error desconocido';
            if (errMsg.includes('HTTP Error 403') || errMsg.includes('private') || errMsg.includes('Private video')) {
                await sock.sendMessage(jid, {
                    text: '❌ El video es privado o requiere inicio de sesión.\nExporta tus cookies de YouTube a `assets/www.youtube.com_cookies.json`'
                }, { quoted: msg });
            } else if (errMsg.includes('not available') || errMsg.includes('No video results')) {
                await sock.sendMessage(jid, {
                    text: '❌ Video no disponible. Puede haber sido eliminado o restringido en tu región.'
                }, { quoted: msg });
            } else if (errMsg.includes('UNICODE') || errMsg.includes('Emoji')) {
                await sock.sendMessage(jid, {
                    text: '❌ Error con caracteres especiales en el título. Prueba con otro video.'
                }, { quoted: msg });
            } else {
                await sock.sendMessage(jid, {
                    text: `❌ Error: ${errMsg.slice(0, 250)}`
                }, { quoted: msg });
            }
        }
    }
};
