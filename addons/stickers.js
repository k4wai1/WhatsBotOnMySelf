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
    // Aquí defines los alias del comando. El index los leerá automáticamente.
    commands: ['st', 'sticker', 's'],
    
    // El index inyecta el socket, el mensaje crudo y los argumentos ya separados
    handler: async (sock, msg, args) => {
        const cacheDir = path.join(__dirname, '..', 'cache');
        const ramDiskDir = '/dev/shm'; 

        await fs.mkdir(cacheDir, { recursive: true }).catch(() => {});

        // Ya no necesitamos separar rawText, usamos 'args' directo
        const isCrop = args.includes('crop') || args.includes('c');
        const isQualityMode = args.some(p => p === 'c');
        const mode = isQualityMode ? 'c' : 'f';

        const quotedMsg = msg.message?.extendedTextMessage?.contextInfo?.quotedMessage;

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

        if (msg.message.imageMessage || msg.message.videoMessage || msg.message.documentMessage) {
            mediaBuffer = await downloadMediaMessage(msg, 'buffer', {});
            mimetype = msg.message.imageMessage?.mimetype ||
                       msg.message.videoMessage?.mimetype ||
                       msg.message.documentMessage?.mimetype;
        } else if (quotedMsg) {
            if (quotedMsg.imageMessage || quotedMsg.videoMessage || quotedMsg.documentMessage) {
                const mockMsg = { message: quotedMsg };
                mediaBuffer = await downloadMediaMessage(mockMsg, 'buffer', {});
                mimetype = quotedMsg.imageMessage?.mimetype ||
                           quotedMsg.videoMessage?.mimetype ||
                           quotedMsg.documentMessage?.mimetype;
            }
        }

        if (!mediaBuffer || !mimetype || (!mimetype.startsWith('image/') && !mimetype.startsWith('video/'))) {
            await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
            return;
        }

        let finalBuffer;
        const uniqueId = Date.now();

        if (mimetype.startsWith('image/') && mimetype !== 'image/gif') {
            finalBuffer = await processStaticToMaxUtilization(mediaBuffer, isCrop, cacheDir, uniqueId, 100000);
        } else {
            const isGif = mimetype === 'image/gif';
            const ext = isGif ? 'gif' : 'mp4';
            finalBuffer = await processAnimatedToMaxUtilization(mediaBuffer, mode, isCrop, cacheDir, ramDiskDir, uniqueId, ext, 1000000, isGif);
        }

        if (finalBuffer.length > (mimetype.startsWith('image/') && mimetype !== 'image/gif' ? 100000 : 1000000)) {
            // Error silencioso, excede el límite
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
            return;
        }

        await sock.sendMessage(jid, { sticker: finalBuffer }, { quoted: msg });
        await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
    }
};

// --- FUNCIONES CORE ---

async function processAnimatedToMaxUtilization(buffer, mode, isCrop, diskDir, ramDir, uniqueId, ext, sizeLimit, isGif) {
    const inputPath = path.join(diskDir, `in_${uniqueId}.${ext}`);
    const framesRamDir = path.join(ramDir, `rupa_frames_${uniqueId}`);
    const outputPath = path.join(diskDir, `out_${uniqueId}.webp`);

    try {
        await fs.writeFile(inputPath, buffer);
        await fs.mkdir(framesRamDir, { recursive: true });

        const durationInfo = await getDurationAndFps(inputPath);
        let sourceDuration = durationInfo.duration || 5;
        let sourceFps = durationInfo.fps || 30;

        let trimDuration = Math.min(sourceDuration, 9.9);
        let targetFps = calculateTargetFps(mode, trimDuration, sourceFps);

        let scaleFilter = isCrop
            ? 'scale=512:512:force_original_aspect_ratio=increase,crop=512:512'
            : 'scale=512:512:force_original_aspect_ratio=decrease,pad=512:512:(ow-iw)/2:(ih-oh)/2:color=0x00000000';

        const extractFilters = `fps=${targetFps},format=rgba,${scaleFilter}`;

        await new Promise((resolve, reject) => {
            const command = ffmpeg(inputPath);
            command.duration(trimDuration);
            command.outputOptions([
                '-c:v png',
                '-pix_fmt rgba',
                `-vf ${extractFilters}`
            ])
            .save(path.join(framesRamDir, 'frame_%04d.png'))
            .on('end', resolve)
            .on('error', reject);
        });

        const targetSize = sizeLimit * 0.90;
        const margin = sizeLimit * 0.05;

        let qMin = 0, qMax = 100, bestBuffer = null, bestDiff = Infinity;
        const frameDelay = Math.round(1000 / targetFps);

        for (let i = 0; i < 4; i++) {
            let currentQ = Math.floor((qMin + qMax) / 2);

            if (isGif) {
                const cmd = `img2webp -loop 0 -lossy -m 4 -q ${currentQ} -d ${frameDelay} ${path.join(framesRamDir, '*.png')} -o ${outputPath}`;
                try {
                    await execPromise(cmd);
                } catch (err) {
                    throw new Error(`Fallo en img2webp. Detalles: ${err.message}`);
                }
            } else {
                await new Promise((resolve, reject) => {
                    ffmpeg(path.join(framesRamDir, 'frame_%04d.png'))
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

async function processStaticToMaxUtilization(buffer, isCrop, diskDir, uniqueId, sizeLimit) {
    const inputPath = path.join(diskDir, `in_static_${uniqueId}.jpg`);
    const outputPath = path.join(diskDir, `out_static_${uniqueId}.webp`);

    try {
        await fs.writeFile(inputPath, buffer);
        let targetSize = sizeLimit * 0.90;

        let scaleFilter = isCrop
            ? 'scale=512:512:force_original_aspect_ratio=increase,crop=512:512'
            : 'scale=512:512:force_original_aspect_ratio=decrease,pad=512:512:(ow-iw)/2:(ih-oh)/2:color=0x00000000';

        let qMin = 10, qMax = 100, bestBuffer = null, bestDiff = Infinity;

        for (let i = 0; i < 3; i++) {
            let q = Math.floor((qMin + qMax) / 2);
            await new Promise((resolve, reject) => {
                ffmpeg(inputPath).outputOptions([
                    '-vcodec libwebp', '-lossless 0', `-q:v ${q}`,
                    '-compression_level 6', '-an', `-vf format=rgba,${scaleFilter}`
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
    if (mode === 'f') {
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
