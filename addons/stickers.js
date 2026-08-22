// addons/stickers.js
const { downloadMediaMessage } = require('@whiskeysockets/baileys');
const ffmpeg = require('fluent-ffmpeg');
const fs = require('fs').promises;
const fss = require('fs');
const path = require('path');
const { exec } = require('child_process');
const util = require('util');
const execPromise = util.promisify(exec);

const unlinkSafe = async (p) => fs.unlink(p).catch(() => {});
const rmDirSafe = async (p) => fs.rm(p, { recursive: true, force: true }).catch(() => {});

// --- Estado global para sesiones de colección ---
const sessions = {};
const STICKER_CMDS = new Set(['st', 'sticker', 's']);
let cachedPrefixes = null;
let prefixesAt = 0;

function getPrefixes() {
    const now = Date.now();
    if (cachedPrefixes && now - prefixesAt < 10000) return cachedPrefixes;
    let prefixes = [',', '!', '/'];
    try {
        const cfg = JSON.parse(fss.readFileSync(path.join(__dirname, '..', 'assets', 'config.json'), 'utf-8'));
        if (Array.isArray(cfg.prefixes) && cfg.prefixes.length) prefixes = cfg.prefixes;
    } catch (_) {}
    cachedPrefixes = prefixes;
    prefixesAt = now;
    return prefixes;
}

function isStickerCommand(text) {
    if (!text) return false;
    for (const p of getPrefixes()) {
        if (text.startsWith(p)) {
            const cmd = text.slice(p.length).trim().split(/\s+/)[0]?.toLowerCase();
            if (STICKER_CMDS.has(cmd)) return true;
        }
    }
    return false;
}

// --- Registro del listener (uno por socket vivo; index llama init() en cada reconexión) ---
function registerListener(sock) {
    sock.ev.on('messages.upsert', async ({ messages }) => {
        for (const msg of messages) {
            // Ignorar mensajes que sean comandos (los gestiona el handler)
            const text = msg.message?.extendedTextMessage?.text || '';
            if (isStickerCommand(text)) continue;

            const jid = msg.key.remoteJid;
            const session = sessions[jid];
            if (!session || !session.isActive) continue;

            // Determinar si el mensaje contiene medio
            let mediaBuffer = null;
            let mimetype = null;
            const quotedMsg = msg.message?.extendedTextMessage?.contextInfo?.quotedMessage;

            if (msg.message.imageMessage || msg.message.videoMessage || msg.message.documentMessage || msg.message.stickerMessage) {
                mediaBuffer = await downloadMediaMessage(msg, 'buffer', {});
                mimetype = msg.message.imageMessage?.mimetype ||
                           msg.message.videoMessage?.mimetype ||
                           msg.message.documentMessage?.mimetype ||
                           msg.message.stickerMessage?.mimetype;
            } else if (quotedMsg) {
                if (quotedMsg.imageMessage || quotedMsg.videoMessage || quotedMsg.documentMessage || quotedMsg.stickerMessage) {
                    const mockMsg = {
                        key: {
                            remoteJid: jid,
                            id: msg.message.extendedTextMessage.contextInfo.stanzaId,
                            participant: msg.message.extendedTextMessage.contextInfo.participant
                        },
                        message: quotedMsg
                    };
                    try {
                        mediaBuffer = await downloadMediaMessage(mockMsg, 'buffer', {});
                    } catch (e) {}
                    mimetype = quotedMsg.imageMessage?.mimetype ||
                               quotedMsg.videoMessage?.mimetype ||
                               quotedMsg.documentMessage?.mimetype ||
                               quotedMsg.stickerMessage?.mimetype;
                }
            }

            if (mediaBuffer && mediaBuffer.length > 100 && mimetype && (mimetype.startsWith('image/') || mimetype.startsWith('video/'))) {
                // Guardamos también si es sticker animado (para saber si es entrada animada)
                const isAnimatedSticker = msg.message?.stickerMessage?.isAnimated ||
                                          quotedMsg?.stickerMessage?.isAnimated ||
                                          false;
                session.buffers.push({ buffer: mediaBuffer, mimetype, isAnimatedSticker });
                await sock.sendMessage(jid, { react: { text: '📥', key: msg.key } });
            }
        }
    });
}

// --- Procesamiento de la colección: cada archivo -> un sticker ---
async function processCollection(sock, jid, session) {
    const buffers = session.buffers;
    if (buffers.length === 0) {
        await sock.sendMessage(jid, { text: '❌ No se recibieron archivos durante la colección.' });
        return;
    }

    const args = session.args;
    const isCrop = args.includes('crop') || args.includes('c');
    const isReverse = args.includes('reverse') || args.includes('r');
    let shapeMode = 'none';
    if (args.includes('spherical') || args.includes('s')) shapeMode = 'spherical';
    else if (args.includes('border') || args.includes('b')) shapeMode = 'border';

    let mode = 'fluid';
    for (const arg of args) {
        if (arg === 'h' || arg === 'high') { mode = 'high'; break; }
        if (arg === 'f' || arg === 'fluid') { mode = 'fluid'; break; }
    }

    const cacheDir = path.join(__dirname, '..', 'cache');
    await fs.mkdir(cacheDir, { recursive: true }).catch(() => {});

    // Procesar cada archivo individualmente
    let processed = 0;
    for (const item of buffers) {
        const { buffer, mimetype, isAnimatedSticker } = item;
        const uniqueId = Date.now() + '_' + Math.random().toString(36).slice(2, 7);

        const isGif = mimetype === 'image/gif';
        const isVideo = mimetype.startsWith('video/');
        const isAnimatedInput = isGif || isVideo || isAnimatedSticker;

        try {
            let finalBuffer;
            if (!isAnimatedInput) {
                const ext = mimetype.includes('webp') ? 'webp' : 'jpg';
                finalBuffer = await processStaticToMaxUtilization(buffer, isCrop, shapeMode, cacheDir, uniqueId, 100000, ext);
            } else {
                const ext = isGif ? 'gif' : (isAnimatedSticker ? 'webp' : 'mp4');
                finalBuffer = await processAnimatedToMaxUtilization(buffer, mode, isCrop, isReverse, shapeMode, cacheDir, '/dev/shm', uniqueId, ext, 1000000, isGif);
            }

            const sizeLimit = isAnimatedInput ? 1000000 : 100000;
            if (!finalBuffer || finalBuffer.length > sizeLimit) {
                await sock.sendMessage(jid, { text: `⚠️ Un archivo supera el límite de tamaño (${(sizeLimit/1000).toFixed(0)} KB) y no se pudo procesar.` });
                continue;
            }

            await sock.sendMessage(jid, { sticker: finalBuffer });
            processed++;
        } catch (error) {
            console.error('Error procesando un elemento de la colección:', error);
            await sock.sendMessage(jid, { text: '❌ Error al procesar uno de los archivos.' });
        }
    }

    await sock.sendMessage(jid, { text: `✅ Colección completada. Se generaron ${processed} stickers.` });
}

// --- Funciones originales (sin modificar) ---

function getFilterChain(isCrop, shapeMode) {
    const rgb = `r='p(X,Y)':g='p(X,Y)':b='p(X,Y)'`;
    let shape = '';

    if (shapeMode === 'spherical') {
        shape = `,geq=${rgb}:a='p(X,Y)*lte((X-W/2)*(X-W/2)+(Y-H/2)*(Y-H/2),(min(W,H)/2)*(min(W,H)/2))'`;
    } else if (shapeMode === 'border') {
        const r = `(min(W,H)*0.1)`;
        const dx = `max(0,abs(X-W/2)-(W/2-${r}))`;
        const dy = `max(0,abs(Y-H/2)-(H/2-${r}))`;
        shape = `,geq=${rgb}:a='p(X,Y)*lte(${dx}*${dx}+${dy}*${dy},${r}*${r})'`;
    }

    if (isCrop) {
        return `scale='max(512,iw*512/ih)':'max(512,ih*512/iw)',format=rgba,crop=512:512${shape}`;
    } else {
        return `scale=512:512:force_original_aspect_ratio=decrease,format=rgba${shape},pad=512:512:(ow-iw)/2:(oh-ih)/2:color=0x00000000`;
    }
}

async function processAnimatedToMaxUtilization(buffer, mode, isCrop, isReverse, shapeMode, diskDir, ramDir, uniqueId, ext, sizeLimit, isGif) {
    const inputPath = path.join(diskDir, `in_${uniqueId}.${ext}`);
    const framesRamDir = path.join(ramDir, `rupa_frames_${uniqueId}`);
    const outputPath = path.join(diskDir, `out_${uniqueId}.webp`);

    try {
        await fs.writeFile(inputPath, buffer);
        await fs.mkdir(framesRamDir, { recursive: true });

        const durationInfo = await getDurationAndFps(inputPath);
        let sourceDuration = durationInfo.duration || 5;
        let sourceFps = durationInfo.fps || 30;

        let trimDuration = isReverse ? Math.min(sourceDuration, 4.9) : Math.min(sourceDuration, 9.9);
        let expectedFinalDuration = isReverse ? (trimDuration * 2) : trimDuration;
        let targetFps = calculateTargetFps(mode, expectedFinalDuration, sourceFps);

        const filterChain = getFilterChain(isCrop, shapeMode);
        const extractFilters = `fps=${targetFps},${filterChain}`;

        try {
            await new Promise((resolve, reject) => {
                const command = ffmpeg(inputPath);
                if (ext === 'webp') {
                    command.inputOptions(['-vcodec', 'libwebp_anim']);
                }
                command.inputOptions(['-err_detect', 'ignore_err']);
                command.duration(trimDuration);
                command.outputOptions([
                    '-c:v png',
                    '-pix_fmt rgba',
                    `-vf`, extractFilters
                ])
                .save(path.join(framesRamDir, 'frame_%04d.png'))
                .on('end', resolve)
                .on('error', reject);
            });
        } catch (ffmpegError) {
            if (ext === 'webp') {
                console.log(`[Stickers] FFmpeg colapsó con un WebP de WhatsApp. Usando escudo ImageMagick...`);
                const rawDir = path.join(ramDir, `raw_${uniqueId}`);
                await fs.mkdir(rawDir, { recursive: true });
                await execPromise(`convert "${inputPath}" -coalesce "${path.join(rawDir, 'raw_%04d.png')}"`);
                await new Promise((resolve, reject) => {
                    ffmpeg(path.join(rawDir, 'raw_%04d.png'))
                        .inputOptions([`-framerate`, `${sourceFps}`])
                        .duration(trimDuration)
                        .outputOptions([
                            '-c:v png',
                            '-pix_fmt rgba',
                            `-vf`, extractFilters
                        ])
                        .save(path.join(framesRamDir, 'frame_%04d.png'))
                        .on('end', resolve)
                        .on('error', reject);
                });
                await rmDirSafe(rawDir);
            } else {
                throw ffmpegError;
            }
        }

        if (isReverse) {
            const frames = (await fs.readdir(framesRamDir)).filter(f => f.startsWith('frame_')).sort();
            const totalFrames = frames.length;
            if (totalFrames > 0) {
                const mappedFrames = [];
                let currentSourceFrame = 0;
                while (currentSourceFrame < totalFrames - 1) {
                    mappedFrames.push(Math.round(currentSourceFrame));
                    let progress = currentSourceFrame / totalFrames;
                    let speed = 1.0;
                    if (progress <= 0.25) {
                        let t = progress / 0.25;
                        speed = 0.6 + 0.4 * Math.sin(t * Math.PI / 2);
                    } else if (progress >= 0.75) {
                        let t = (progress - 0.75) / 0.25;
                        speed = 1.0 + 0.4 * (1 - Math.cos(t * Math.PI / 2));
                    }
                    currentSourceFrame += speed;
                }
                if (mappedFrames[mappedFrames.length - 1] !== totalFrames - 1) {
                    mappedFrames.push(totalFrames - 1);
                }
                const finalSequence = [...mappedFrames, ...mappedFrames.slice(0, -1).reverse()];
                for (let i = 0; i < finalSequence.length; i++) {
                    const sourceFrame = frames[finalSequence[i]];
                    const targetName = `loop_${String(i + 1).padStart(4, '0')}.png`;
                    await fs.copyFile(path.join(framesRamDir, sourceFrame), path.join(framesRamDir, targetName));
                }
                for (const frame of frames) {
                    await unlinkSafe(path.join(framesRamDir, frame));
                }
            }
        }

        const targetSize = sizeLimit * 0.90;
        const margin = sizeLimit * 0.05;
        let qMin = 0, qMax = 100, bestBuffer = null, bestDiff = Infinity;
        const frameDelay = Math.round(1000 / targetFps);

        const framePrefix = isReverse ? 'loop_%04d.png' : 'frame_%04d.png';
        const imgGlob = isReverse ? 'loop_*.png' : '*.png';

        for (let i = 0; i < 4; i++) {
            let currentQ = Math.floor((qMin + qMax) / 2);

            if (isGif) {
                const cmd = `img2webp -loop 0 -lossy -m 4 -q ${currentQ} -d ${frameDelay} ${path.join(framesRamDir, imgGlob)} -o ${outputPath}`;
                try {
                    await execPromise(cmd);
                } catch (err) {
                    throw new Error(`Fallo en img2webp: ${err.message}`);
                }
            } else {
                await new Promise((resolve, reject) => {
                    ffmpeg(path.join(framesRamDir, framePrefix))
                        .inputOptions([`-framerate ${targetFps}`])
                        .outputOptions([
                            '-c:v libwebp', '-lossless 0', `-q:v ${currentQ}`,
                            '-compression_level 4', '-loop 0', '-an'
                        ])
                        .save(outputPath).on('end', resolve).on('error', reject);
                });
            }

            const currentBuffer = await fs.readFile(outputPath);
            const currentSize = currentBuffer.length;
            const sizeDiff = Math.abs(targetSize - currentSize);

            if (currentSize <= sizeLimit && sizeDiff < bestDiff) {
                bestBuffer = currentBuffer;
                bestDiff = sizeDiff;
            }

            if (currentSize >= (targetSize - margin) && currentSize <= (targetSize + margin)) break;

            if (currentSize > targetSize) qMax = currentQ - 1;
            else qMin = currentQ + 1;
        }

        return bestBuffer || (await fs.readFile(outputPath));

    } finally {
        await unlinkSafe(inputPath);
        await unlinkSafe(outputPath);
        await rmDirSafe(framesRamDir);
    }
}

async function processStaticToMaxUtilization(buffer, isCrop, shapeMode, diskDir, uniqueId, sizeLimit, ext = 'jpg') {
    const inputPath = path.join(diskDir, `in_static_${uniqueId}.${ext}`);
    const outputPath = path.join(diskDir, `out_static_${uniqueId}.webp`);

    try {
        await fs.writeFile(inputPath, buffer);
        let targetSize = sizeLimit * 0.90;

        const filterChain = getFilterChain(isCrop, shapeMode);

        let qMin = 10, qMax = 100, bestBuffer = null, bestDiff = Infinity;

        for (let i = 0; i < 3; i++) {
            let q = Math.floor((qMin + qMax) / 2);
            await new Promise((resolve, reject) => {
                ffmpeg(inputPath)
                .inputOptions(['-err_detect', 'ignore_err'])
                .outputOptions([
                    '-vcodec libwebp', '-lossless 0', `-q:v ${q}`,
                    '-compression_level 6', '-an',
                    `-vf`, `${filterChain}`
                ])
                .save(outputPath).on('end', resolve).on('error', reject);
            });

            const currentBuffer = await fs.readFile(outputPath);
            const currentSize = currentBuffer.length;

            if (currentSize <= sizeLimit && Math.abs(targetSize - currentSize) < bestDiff) {
                bestBuffer = currentBuffer;
                bestDiff = Math.abs(targetSize - currentSize);
            }

            if (currentSize > targetSize) qMax = q - 1;
            else qMin = q + 1;
        }

        return bestBuffer || (await fs.readFile(outputPath));
    } finally {
        await unlinkSafe(inputPath); await unlinkSafe(outputPath);
    }
}

function calculateTargetFps(mode, duration, sourceFps) {
    let fps = 18;
    if (mode === 'high') {
        if (duration < 1.5) fps = Math.min(30, sourceFps);
        else if (duration < 3.0) fps = 24;
        else if (duration < 6.0) fps = 20;
        else fps = 18;
    } else {
        if (duration < 2.0) fps = 24;
        else if (duration < 4.0) fps = 16;
        else if (duration < 6.0) fps = 12;
        else fps = 8;
    }
    return fps;
}

function getDurationAndFps(filePath) {
    return new Promise((resolve) => {
        ffmpeg.ffprobe(filePath, (err, metadata) => {
            if (err || !metadata || !metadata.streams) return resolve({ duration: 9.9, fps: 24 });
            const videoStream = metadata.streams.find(s => s.codec_type === 'video');
            let duration = metadata.format.duration || 9.9;
            let fps = 24;

            if (videoStream && videoStream.r_frame_rate) {
                const parts = videoStream.r_frame_rate.split('/');
                if (parts.length === 2 && parts[1] !== '0') {
                    fps = parseInt(parts[0], 10) / parseInt(parts[1], 10);
                }
            }
            resolve({ duration: parseFloat(duration), fps: Math.round(fps) });
        });
    });
}

// --- EXPORTACIÓN DEL MÓDULO (handler modificado para soportar 'a') ---

module.exports = {
    commands: ['st', 'sticker', 's'],

    init: (sock) => {
        registerListener(sock);
    },

    handler: async (sock, msg, args) => {
        const cacheDir = path.join(__dirname, '..', 'cache');
        await fs.mkdir(cacheDir, { recursive: true }).catch(() => {});

        // Aplanar y normalizar argumentos
        const normalizedArgs = args.flatMap(a => typeof a === 'string' ? a.toLowerCase().split(/\s+/) : []);

        // --- DETECCIÓN DEL FLAG 'a' (colección) ---
        const isCollection = normalizedArgs.includes('a');
        const jid = msg.key.remoteJid;

        if (isCollection) {
            // Si ya hay sesión, reiniciar
            if (sessions[jid]) {
                clearTimeout(sessions[jid].timeout);
                delete sessions[jid];
            }

            // Argumentos sin 'a'
            const argsWithoutA = normalizedArgs.filter(arg => arg !== 'a');

            // Crear nueva sesión
            const session = {
                buffers: [],
                args: argsWithoutA,
                isActive: true,
                timeout: setTimeout(async () => {
                    session.isActive = false;
                    delete sessions[jid];
                    try {
                        await processCollection(sock, jid, session);
                    } catch (e) {
                        console.error('Error procesando colección de stickers:', e);
                        await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } }).catch(() => {});
                    }
                }, 120000) // 2 minutos
            };
            sessions[jid] = session;

            await sock.sendMessage(jid, { text: '📦 Modo colección activado. Envíame imágenes, vídeos o stickers durante el próximo minuto. Cada archivo se convertirá en un sticker individual con los filtros indicados.' }, { quoted: msg });
            await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });
            return; // No procesar más este mensaje
        }

        // --- COMPORTAMIENTO NORMAL (sin colección) ---
        // Si hay sesión activa, pero el mensaje actual es un comando sin 'a', se procesa normalmente
        // y no interfiere con la sesión.

        let isCrop = normalizedArgs.includes('crop') || normalizedArgs.includes('c');
        const isReverse = normalizedArgs.includes('reverse') || normalizedArgs.includes('r');

        let mode = 'fluid';
        for (const arg of normalizedArgs) {
            if (arg === 'h' || arg === 'high') { mode = 'high'; break; }
            if (arg === 'f' || arg === 'fluid') { mode = 'fluid'; break; }
        }

        let shapeMode = 'none';
        for (const arg of normalizedArgs) {
            if (arg === 's' || arg === 'spherical') { shapeMode = 'spherical'; break; }
            if (arg === 'b' || arg === 'border') { shapeMode = 'border'; break; }
        }

        if (shapeMode === 'spherical') {
            isCrop = true;
        }

        const contextInfo = msg.message?.extendedTextMessage?.contextInfo || {};
        const quotedMsg = contextInfo.quotedMessage;

        const isViewOnce =
            msg.message?.imageMessage?.viewOnce === true ||
            msg.message?.videoMessage?.viewOnce === true ||
            quotedMsg?.imageMessage?.viewOnce === true ||
            quotedMsg?.videoMessage?.viewOnce === true ||
            msg.message?.viewOnceMessage ||
            msg.message?.viewOnceMessageV2 ||
            quotedMsg?.viewOnceMessage ||
            quotedMsg?.viewOnceMessageV2;

        if (isViewOnce) {
            await sock.sendMessage(jid, { react: { text: '🚫', key: msg.key } });
            return;
        }

        await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

        let mediaBuffer = null;
        let mimetype = null;
        let isAnimatedSticker = false;

        if (msg.message.imageMessage || msg.message.videoMessage || msg.message.documentMessage || msg.message.stickerMessage) {
            mediaBuffer = await downloadMediaMessage(msg, 'buffer', {});
            mimetype = msg.message.imageMessage?.mimetype ||
                       msg.message.videoMessage?.mimetype ||
                       msg.message.documentMessage?.mimetype ||
                       msg.message.stickerMessage?.mimetype;
            isAnimatedSticker = msg.message.stickerMessage?.isAnimated || false;
        } else if (quotedMsg) {
            if (quotedMsg.imageMessage || quotedMsg.videoMessage || quotedMsg.documentMessage || quotedMsg.stickerMessage) {
                const mockMsg = {
                    key: {
                        remoteJid: msg.key.remoteJid,
                        id: contextInfo.stanzaId,
                        participant: contextInfo.participant
                    },
                    message: quotedMsg
                };
                try {
                    mediaBuffer = await downloadMediaMessage(mockMsg, 'buffer', {});
                } catch (e) {
                    console.log("Fallo al descargar mensaje citado:", e.message);
                }

                mimetype = quotedMsg.imageMessage?.mimetype ||
                           quotedMsg.videoMessage?.mimetype ||
                           quotedMsg.documentMessage?.mimetype ||
                           quotedMsg.stickerMessage?.mimetype;
                isAnimatedSticker = quotedMsg.stickerMessage?.isAnimated || false;
            }
        }

        if (!mediaBuffer || mediaBuffer.length < 100 || !mimetype || (!mimetype.startsWith('image/') && !mimetype.startsWith('video/'))) {
            await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
            return;
        }

        let finalBuffer;
        const uniqueId = Date.now();

        const isGif = mimetype === 'image/gif';
        const isVideo = mimetype.startsWith('video/');
        const isAnimatedInput = isGif || isVideo || isAnimatedSticker;

        try {
            if (!isAnimatedInput) {
                const ext = mimetype.includes('webp') ? 'webp' : 'jpg';
                finalBuffer = await processStaticToMaxUtilization(mediaBuffer, isCrop, shapeMode, cacheDir, uniqueId, 100000, ext);
            } else {
                const ext = isGif ? 'gif' : (isAnimatedSticker ? 'webp' : 'mp4');
                finalBuffer = await processAnimatedToMaxUtilization(mediaBuffer, mode, isCrop, isReverse, shapeMode, cacheDir, '/dev/shm', uniqueId, ext, 1000000, isGif);
            }

            const sizeLimit = isAnimatedInput ? 1000000 : 100000;
            if (!finalBuffer || finalBuffer.length > sizeLimit) {
                await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                return;
            }

            await sock.sendMessage(jid, { sticker: finalBuffer }, { quoted: msg });
            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
        } catch (error) {
            console.error("Error en addon de stickers:", error);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
        }
    }
};
