// addons/audio_editor.js
const fs = require('fs-extra');
const path = require('path');
const ffmpeg = require('fluent-ffmpeg');
const { downloadContentFromMessage } = require('@whiskeysockets/baileys');
const crypto = require('crypto');

// Helpers
const ensureTempDir = async (sessionId) => {
    const dir = path.join(__dirname, `../temp_au_${sessionId}`);
    await fs.ensureDir(dir);
    return dir;
};

const downloadAudio = async (mediaMsg, type) => {
    const stream = await downloadContentFromMessage(mediaMsg, type);
    let buffer = Buffer.from([]);
    for await (const chunk of stream) {
        buffer = Buffer.concat([buffer, chunk]);
    }
    return buffer;
};

const parseTime = (str) => {
    return parseFloat(str.replace(',', '.'));
};

// 🧠 ALGORITMO DE EXTRACCIÓN DE ONDAS REALES
const getRealWaveform = async (audioPath) => {
    return new Promise((resolve, reject) => {
        let pcmData = Buffer.alloc(0);

        // Convertimos a audio crudo de 16-bits para leer la amplitud matemáticamente
        ffmpeg(audioPath)
            .format('s16le')
            .audioChannels(1)
            .audioFrequency(16000)
            .on('error', reject)
            .pipe()
            .on('data', (chunk) => {
                pcmData = Buffer.concat([pcmData, chunk]);
            })
            .on('end', () => {
                const waveform = new Uint8Array(64);
                const totalSamples = Math.floor(pcmData.length / 2); // 2 bytes por sample
                const chunkSize = Math.floor(totalSamples / 64);

                if (chunkSize === 0) return resolve(waveform); // Audio demasiado corto

                let maxVal = 0;
                const peaks = [];

                // Dividimos el audio en 64 fragmentos y buscamos el pico máximo en cada uno
                for (let i = 0; i < 64; i++) {
                    let chunkMax = 0;
                    const startOffset = i * chunkSize * 2;

                    for (let j = 0; j < chunkSize; j++) {
                        const offset = startOffset + (j * 2);
                        if (offset < pcmData.length - 1) {
                            const val = Math.abs(pcmData.readInt16LE(offset));
                            if (val > chunkMax) chunkMax = val;
                        }
                    }
                    peaks.push(chunkMax);
                    if (chunkMax > maxVal) maxVal = chunkMax;
                }

                // Normalizamos los picos a una escala de 0 a 100 para la UI de WhatsApp
                for (let i = 0; i < 64; i++) {
                    waveform[i] = maxVal === 0 ? 0 : Math.floor((peaks[i] / maxVal) * 100);
                }

                resolve(waveform);
            });
    });
};

// 🎯 Normalización a formato compatible con WhatsApp (OGG/Opus 48kHz, sin video)
const normalizeToWhatsAppAudio = (inputPath, outputPath, { startSec, durationSec } = {}) => {
    return new Promise((resolve, reject) => {
        let cmd = ffmpeg(inputPath)
            .noVideo()              // crítico para entradas de video
            .audioFrequency(48000); // estándar de WhatsApp

        if (startSec !== undefined) cmd = cmd.setStartTime(startSec);
        if (durationSec !== undefined) cmd = cmd.setDuration(durationSec);

        cmd.toFormat('ogg')
            .audioCodec('libopus')
            .save(outputPath)
            .on('end', resolve)
            .on('error', reject);
    });
};

// ─── Modo UNIR: unir varios audios en uno solo (.au unir) ───────────────────
const UNIR_DURACION_MS = 60 * 1000;  // ventana de recogida: 1 minuto
const UNIR_MAX_AUDIOS = 25;          // tope por sesión
const UNIR_MAX_CACHE = 400;          // cuántos medios recordar para reacciones

// Caché global de audios/videos vistos por el bot (permite reaccionar a medios
// ya enviados en el chat, aunque sea antes de iniciar el modo unir)
const mediaCache = new Map();        // "<chat>:<idMensaje>" -> { msg, content }
const unirSessions = new Map();      // "<chat>|<owner>" -> sesión activa

const senderOf = (msg) => msg.key.participant || msg.key.remoteJid;

const isUnirMedia = (content) => !!content && !!(content.audioMessage || content.videoMessage);

// Desenrolla view-once / efímeros para hallar el audio/video real
const unwrapMedia = (msg) => {
    let m = msg?.message;
    for (let i = 0; i < 5 && m; i++) {
        if (m.audioMessage || m.videoMessage) return m;
        m = m.viewOnceMessage?.message
            || m.viewOnceMessageV2?.message
            || m.viewOnceMessageV2Extension?.message
            || m.ephemeralMessage?.message
            || null;
    }
    return null;
};

const recordMediaCache = (msg, content) => {
    if (!isUnirMedia(content)) return;
    const chat = msg.key.remoteJid;
    const id = msg.key.id;
    if (!chat || !id) return;
    if (mediaCache.size >= UNIR_MAX_CACHE) {
        const oldest = mediaCache.keys().next().value;
        mediaCache.delete(oldest);
    }
    mediaCache.set(`${chat}:${id}`, { msg, content });
};

// 🔗 Une N audios (o pistas de audio de videos) en un solo OGG/Opus 48kHz mono
const mergeAudios = (inputPaths, outputPath) => {
    return new Promise((resolve, reject) => {
        const n = inputPaths.length;
        if (n === 0) return resolve();

        const chains = inputPaths.map((_, i) =>
            `[${i}:a]aformat=sample_rates=48000:channel_layouts=mono[a${i}]`
        );
        const graph = `${chains.join(';')};${inputPaths.map((_, i) => `[a${i}]`).join('')}concat=n=${n}:v=0:a=1[out]`;

        let cmd = ffmpeg(inputPaths[0]);
        for (let i = 1; i < n; i++) cmd = cmd.input(inputPaths[i]);

        cmd.complexFilter(graph)
            .outputOptions(['-map', '[out]'])
            .noVideo()
            .audioCodec('libopus')
            .audioFrequency(48000)
            .audioBitrate('64k')
            .toFormat('ogg')
            .save(outputPath)
            .on('end', resolve)
            .on('error', reject);
    });
};

const avisoTope = async (session) => {
    if (session.avisadoLleno) return;
    session.avisadoLleno = true;
    await session.sock.sendMessage(session.chat, {
        text: `⚠️ Tope de ${UNIR_MAX_AUDIOS} audios para la unión; lo que llegue de más se ignorará.`
    }, { quoted: session.invoker });
};

const iniciarUnir = async (sock, msg, asVoiceNote) => {
    const chat = msg.key.remoteJid;
    const owner = senderOf(msg);
    const sessionKey = `${chat}|${owner}`;

    const prev = unirSessions.get(sessionKey);
    if (prev) clearTimeout(prev.timeout);

    const session = {
        key: sessionKey,
        chat,
        owner,
        sock,
        invoker: msg,
        vn: asVoiceNote,
        audios: [],           // { msg, content }
        usados: new Set(),    // ids ya recogidos (evita duplicados por medio o reacción)
        done: false,
        avisadoLleno: false,
        avisadoViejo: false,
        timeout: null
    };
    unirSessions.set(sessionKey, session);

    session.timeout = setTimeout(async () => {
        try {
            await finalizarUnir(session);
        } catch (e) {
            console.error('Error finalizando .au unir:', e);
        }
    }, UNIR_DURACION_MS);

    await sock.sendMessage(chat, { react: { text: '⏱️', key: msg.key } });
    await sock.sendMessage(chat, {
        text:
            `🔗 *Modo unir activado* (1 minuto)\n\n` +
            `Manda los audios que quieras unir, *o reacciona a audios/videos ya enviados en este chat* ` +
            `(la reacción solo funciona con medios que el bot haya visto desde que está encendido; ` +
            `si uno viejo no lo reconoce, reenvíalo).\n\n` +
            `Cada medio recogido se marca con ✓. Al minuto los envío unidos en *un solo audio*. ` +
            (asVoiceNote ? 'El resultado irá como *nota de voz* 🎙️.\n' : '') +
            `Progreso: ${prev ? '(reinicié una unión en curso) ' : ''}Cancelar con \`.au unir cancel\``
    }, { quoted: msg });
};

const cancelarUnir = async (sock, msg) => {
    const chat = msg.key.remoteJid;
    const owner = senderOf(msg);
    const session = unirSessions.get(`${chat}|${owner}`);

    if (!session || session.done) {
        await sock.sendMessage(chat, { text: 'ℹ️ No hay ninguna unión de audios activa en este chat.' }, { quoted: msg });
        await sock.sendMessage(chat, { react: { text: '🤷', key: msg.key } });
        return;
    }

    session.done = true; // evita que el timer la procese igual
    clearTimeout(session.timeout);
    unirSessions.delete(session.key);

    await sock.sendMessage(chat, { react: { text: '🛑', key: msg.key } });
    await sock.sendMessage(chat, { text: `🛑 Unión cancelada. Los ${session.audios.length} audios recogidos no se enviarán.` }, { quoted: msg });
};

// audio/video que llega al chat mientras la ventana corre
const manejarUnirMedia = async (sock, msg) => {
    const content = unwrapMedia(msg);
    if (!content) return;

    recordMediaCache(msg, content); // siempre: útil incluso para uniones futuras

    const chat = msg.key.remoteJid;
    const session = unirSessions.get(`${chat}|${senderOf(msg)}`);
    if (!session || session.done) return;

    const id = msg.key.id;
    if (session.usados.has(id)) return;

    if (session.audios.length >= UNIR_MAX_AUDIOS) {
        await avisoTope(session);
        return;
    }

    session.usados.add(id);
    session.audios.push({ msg, content });

    await sock.sendMessage(chat, { react: { text: '✅', key: msg.key } });
};

// reacción del dueño de la sesión: marca un medio (nuevo o ya en el chat) para unir
const manejarUnirReaccion = async (sock, msg, reaction) => {
    const chat = msg.key.remoteJid;
    const reactor = senderOf(msg);
    const session = unirSessions.get(`${chat}|${reactor}`);
    if (!session || session.done) return;

    const targetKey = reaction.key;
    if (!targetKey?.id) return;

    const cacheItem = mediaCache.get(`${targetKey.remoteJid || chat}:${targetKey.id}`)
        || mediaCache.get(`${chat}:${targetKey.id}`);

    if (!cacheItem || !isUnirMedia(cacheItem.content)) {
        // El medio reaccionado no está en caché (muy viejo o el bot se reinició)
        if (!session.avisadoViejo) {
            session.avisadoViejo = true;
            await sock.sendMessage(chat, {
                text: 'ℹ️ No tengo en caché ese audio reaccionado (muy antiguo o el bot se reinició). Reenvíalo durante el minuto y lo agrego igual.'
            }, { quoted: msg });
        }
        return;
    }

    const id = cacheItem.msg.key.id;
    if (session.usados.has(id)) return;

    if (session.audios.length >= UNIR_MAX_AUDIOS) {
        await avisoTope(session);
        return;
    }

    session.usados.add(id);
    session.audios.push(cacheItem);

    await sock.sendMessage(chat, { react: { text: '✅', key: cacheItem.msg.key } });
};

// ⏱️ Se dispara al terminar el minuto: descarga, une y envía
const finalizarUnir = async (session) => {
    if (session.done) return;
    session.done = true;
    clearTimeout(session.timeout);
    unirSessions.delete(session.key);

    const { sock, chat } = session;

    const total = session.audios.length;
    if (total === 0) {
        await sock.sendMessage(chat, { text: '🫥 No se recogió ningún audio; el modo unir termina sin resultado.' }, { quoted: session.invoker });
        await sock.sendMessage(chat, { react: { text: '🫥', key: session.invoker.key } });
        return;
    }

    const sessionId = crypto.randomBytes(4).toString('hex');
    const tempDir = await ensureTempDir(sessionId);
    try {
        // 1) Descargar todo lo recogido
        const inputs = [];
        for (let i = 0; i < total; i++) {
            const item = session.audios[i];
            const mediaType = item.content.audioMessage ? 'audio' : 'video';
            const buffer = await downloadAudio(item.content, mediaType);
            if (!buffer || buffer.length === 0) continue; // medio expirado: se omite
            const rawPath = path.join(tempDir, `src_${i}.${mediaType === 'video' ? 'mp4' : 'ogg'}`);
            await fs.writeFile(rawPath, buffer);
            inputs.push(rawPath);
        }
        if (inputs.length === 0) {
            throw new Error('todos los audios recogidos están expirados o no descargables');
        }

        await sock.sendMessage(chat, { react: { text: '🔗', key: session.invoker.key } });

        // 2) Fusionar en un solo OGG/Opus
        const mergedPath = path.join(tempDir, 'merged.ogg');
        await mergeAudios(inputs, mergedPath);
        const mergedBuffer = await fs.readFile(mergedPath);
        if (!mergedBuffer || mergedBuffer.length === 0) throw new Error('la fusión salió vacía');

        if (mergedBuffer.length > 15 * 1024 * 1024) {
            throw new Error(`el audio unido pesa ${(mergedBuffer.length / 1024 / 1024).toFixed(1)} MB y WhatsApp lo rechaza; une menos o más cortos`);
        }

        // 3) Enviar
        const realWaveform = session.vn ? await getRealWaveform(mergedPath) : undefined;

        await sock.sendMessage(chat, {
            audio: mergedBuffer,
            mimetype: 'audio/ogg; codecs=opus',
            ptt: session.vn,
            waveform: realWaveform
        }, { quoted: session.invoker });

        await sock.sendMessage(chat, { react: { text: '✅', key: session.invoker.key } });
        const omitidos = total - inputs.length;
        await sock.sendMessage(chat, {
            text: `🔗 Audios unidos: ${inputs.length}${omitidos > 0 ? ` (${omitidos} omitidos por estar expirados)` : ''}`
        }, { quoted: session.invoker });

    } catch (error) {
        console.error('Error en AU/unir (fusión):', error);
        if (session.invoker?.key) {
            await sock.sendMessage(chat, { react: { text: '❌', key: session.invoker.key } }).catch(() => {});
        }
        await sock.sendMessage(chat, { text: `❌ Error al unir audios: ${error.message}` }, { quoted: session.invoker }).catch(() => {});
    } finally {
        await fs.remove(tempDir).catch(() => {});
    }
};

module.exports = {
    commands: ['au'],
    handler: async (sock, msg, args, store) => {
        const jid = msg.key.remoteJid;
        const quotedMsg = msg.message?.extendedTextMessage?.contextInfo?.quotedMessage;
        const audioMsg = quotedMsg?.audioMessage;
        const videoMsg = quotedMsg?.videoMessage;
        const mediaMsg = audioMsg || videoMsg;

        // ─── .au unir: no requiere medio citado ──────────────────────
        const subCmdPre = args[0]?.toLowerCase();
        if (subCmdPre === 'unir') {
            const asVoiceNote = args.slice(1).some(a => a?.toLowerCase() === 'vn');
            if (['cancel', 'cancelar'].includes(args[1]?.toLowerCase())) {
                return await cancelarUnir(sock, msg);
            }
            try {
                return await iniciarUnir(sock, msg, asVoiceNote);
            } catch (error) {
                console.error('Error iniciando .au unir:', error);
                await sock.sendMessage(jid, { text: `❌ Error al iniciar el modo unir: ${error.message}` }, { quoted: msg });
                await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
            }
            return;
        }

        if (!mediaMsg) {
            await sock.sendMessage(jid, { react: { text: '🎧', key: msg.key } });
            await sock.sendMessage(jid, { text: '❌ Debes responder a un mensaje de audio o video.' }, { quoted: msg });
            return;
        }

        const mediaType = audioMsg ? 'audio' : 'video';

        const sessionId = crypto.randomBytes(4).toString('hex');
        let tempDir;
        try {
            tempDir = await ensureTempDir(sessionId);
            const rawAudioPath = path.join(tempDir, 'raw.tmp');
            const processedAudioPath = path.join(tempDir, 'processed.ogg');

            await sock.sendMessage(jid, { react: { text: '📥', key: msg.key } });
            const audioBuffer = await downloadAudio(mediaMsg, mediaType);

            // 🛡️ Validación estricta para evitar crasheos por audios expirados o nulos
            if (!audioBuffer || audioBuffer.length === 0) {
                throw new Error("El buffer devuelto está vacío. El medio original podría estar expirado o inaccesible.");
            }

            await fs.writeFile(rawAudioPath, audioBuffer);

            const subCmd = args[0]?.toLowerCase();

            if (subCmd === 'vn' || subCmd === 'audio') {
                const asVoiceNote = subCmd === 'vn';
                await sock.sendMessage(jid, { react: { text: '🔄', key: msg.key } });

                await normalizeToWhatsAppAudio(rawAudioPath, processedAudioPath);

                const processedBuffer = await fs.readFile(processedAudioPath);

                // Extraemos las ondas reales a partir del archivo generado
                const realWaveform = asVoiceNote ? await getRealWaveform(processedAudioPath) : undefined;

                await sock.sendMessage(jid, {
                    audio: processedBuffer,
                    mimetype: 'audio/ogg; codecs=opus',
                    ptt: asVoiceNote,
                    waveform: realWaveform // Inyectamos la magia
                }, { quoted: msg });

                await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
                return;
            }

            if (subCmd === 'r') {
                if (args.length < 3) {
                    await sock.sendMessage(jid, { text: '❌ Uso: .au r <inicio> <fin> [vn]\nEjemplo: .au r 0 5.5 vn' }, { quoted: msg });
                    await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
                    return;
                }

                const startSec = parseTime(args[1]);
                const endSec = parseTime(args[2]);
                const asVoiceNote = args[3]?.toLowerCase() === 'vn';

                if (isNaN(startSec) || isNaN(endSec) || startSec < 0 || endSec <= startSec) {
                    await sock.sendMessage(jid, { text: '❌ Tiempos inválidos. Asegúrate de que inicio < fin y sean números positivos.' }, { quoted: msg });
                    await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
                    return;
                }

                await sock.sendMessage(jid, { react: { text: '✂️', key: msg.key } });

                await normalizeToWhatsAppAudio(rawAudioPath, processedAudioPath, {
                    startSec,
                    durationSec: endSec - startSec
                });

                const processedBuffer = await fs.readFile(processedAudioPath);
                const realWaveform = asVoiceNote ? await getRealWaveform(processedAudioPath) : undefined;

                await sock.sendMessage(jid, {
                    audio: processedBuffer,
                    mimetype: 'audio/ogg; codecs=opus',
                    ptt: asVoiceNote,
                    waveform: realWaveform
                }, { quoted: msg });

                await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
                return;
            }

            await sock.sendMessage(jid, {
                text: `🎵 *Editor de audio AU*\nComandos:\n- \`.au vn\` → convierte el audio/video a nota de voz.\n- \`.au audio\` → convierte a audio normal.\n- \`.au r <inicio> <fin> [vn]\` → recorta del segundo A al B. Agrega "vn" al final para que sea nota de voz.\n- \`.au unir [vn]\` → junta varios audios en uno solo. Manda (o reacciona a) los audios durante 1 minuto; al final los envía unidos. Con "vn" el resultado va como nota de voz.\n- \`.au unir cancel\` → cancela la unión en curso.\nEjemplo: \`.au r 2,5 8.3 vn\`\n\n💡 Funciona con audios y videos (extrae el audio del video automáticamente).`
            }, { quoted: msg });
            await sock.sendMessage(jid, { react: { text: 'ℹ️', key: msg.key } });

        } catch (error) {
            console.error('Error en AU:', error);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
            await sock.sendMessage(jid, { text: `❌ Error procesando medio: ${error.message}` }, { quoted: msg });
        } finally {
            if (tempDir) await fs.remove(tempDir).catch(() => {});
        }
    },

    init: (sock) => {
        // Entre reconexiones, invalida sesiones que usan un sock viejo
        for (const [, s] of unirSessions) clearTimeout(s.timeout);
        unirSessions.clear();

        sock.ev.on('messages.upsert', async (m) => {
            if (m.type !== 'notify') return;
            for (const msg of m.messages) {
                try {
                    const reaction = msg.message?.reactionMessage;
                    if (reaction) {
                        await manejarUnirReaccion(sock, msg, reaction);
                    } else {
                        await manejarUnirMedia(sock, msg);
                    }
                } catch (e) {
                    console.error('Error en .au unir (listener):', e);
                }
            }
        });
    }
};
