// addons/stickers.js
const { downloadMediaMessage } = require('@whiskeysockets/baileys');
const ffmpeg = require('fluent-ffmpeg');
const fs = require('fs').promises;
const path = require('path');
const { exec } = require('child_process');
const util = require('util');
const execPromise = util.promisify(exec);

const unlinkSafe = async (p) => fs.unlink(p).catch(() => {});
const rmDirSafe = async (p) => fs.rm(p, { recursive: true, force: true }).catch(() => {});

module.exports = {
    commands: ['st', 'sticker', 's'],
    
    handler: async (sock, msg, args) => {
        const cacheDir = path.join(__dirname, '..', 'cache');
        const ramDiskDir = '/dev/shm'; 

        await fs.mkdir(cacheDir, { recursive: true }).catch(() => {});

        // Aplanar y normalizar argumentos
        const normalizedArgs = args.flatMap(a => typeof a === 'string' ? a.toLowerCase().split(/\s+/) : []);

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
            await sock.sendMessage(msg.key.remoteJid, { react: { text: '🚫', key: msg.key } });
            return;
        }

        const jid = msg.key.remoteJid;
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
                finalBuffer = await processAnimatedToMaxUtilization(mediaBuffer, mode, isCrop, isReverse, shapeMode, cacheDir, ramDiskDir, uniqueId, ext, 1000000, isGif);
            }

            const sizeLimit = isAnimatedInput ? 1000000 : 100000;
            if (finalBuffer.length > sizeLimit) {
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

// --- FUNCIONES CORE ---

// Cadenas de Filtros Dinámicas (W y H representan las dimensiones reales del marco en ese punto exacto)
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
        // En crop, aplicamos la forma después del corte a 512x512
        return `scale='max(512,iw*512/ih)':'max(512,ih*512/iw)',format=rgba,crop=512:512${shape}`;
    } else {
        // Si NO es crop (ej: 16:9), aplicamos la forma a su ratio original ANTES del padding para no redondear transparencia
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
                // Intento 1: Forzar el decodificador oficial de WebP Animado en FFmpeg
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
            // Intento 2: "Fallback Glorioso" con ImageMagick
            // Si WhatsApp manda un WebP tan asqueroso que FFmpeg explota, usamos ImageMagick (convert)
            // para limpiarlo y extraer los frames, y LUEGO se lo damos a FFmpeg.
            if (ext === 'webp') {
                console.log(`[Stickers] FFmpeg colapsó con un WebP de WhatsApp. Usando escudo ImageMagick...`);
                const rawDir = path.join(ramDir, `raw_${uniqueId}`);
                await fs.mkdir(rawDir, { recursive: true });
                
                // Extraemos frames puros sin filtros
                await execPromise(`convert "${inputPath}" -coalesce "${path.join(rawDir, 'raw_%04d.png')}"`);
                
                // Pasamos los frames puros por nuestro motor matemático de FFmpeg
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
                throw ffmpegError; // Si no es WebP, fue otro error real
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

        return bestBuffer;
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
