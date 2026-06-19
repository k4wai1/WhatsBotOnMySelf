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

const downloadAudio = async (audioMsg) => {
    const stream = await downloadContentFromMessage(audioMsg, 'audio');
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

module.exports = {
    commands: ['au'],
    handler: async (sock, msg, args, store) => {
        const jid = msg.key.remoteJid;
        const quotedMsg = msg.message?.extendedTextMessage?.contextInfo?.quotedMessage;
        const audioMsg = quotedMsg?.audioMessage;

        if (!audioMsg) {
            await sock.sendMessage(jid, { react: { text: '🎧', key: msg.key } });
            await sock.sendMessage(jid, { text: '❌ Debes responder a un mensaje de audio.' }, { quoted: msg });
            return;
        }

        const sessionId = crypto.randomBytes(4).toString('hex');
        let tempDir;
        try {
            tempDir = await ensureTempDir(sessionId);
            const rawAudioPath = path.join(tempDir, 'raw.tmp');
            const processedAudioPath = path.join(tempDir, 'processed.ogg');

            await sock.sendMessage(jid, { react: { text: '📥', key: msg.key } });
            const audioBuffer = await downloadAudio(audioMsg);
            
            // 🛡️ Validación estricta para evitar crasheos por audios expirados o nulos
            if (!audioBuffer || audioBuffer.length === 0) {
                throw new Error("El buffer devuelto está vacío. El audio original podría estar expirado o inaccesible.");
            }

            await fs.writeFile(rawAudioPath, audioBuffer);

            const subCmd = args[0]?.toLowerCase();

            if (subCmd === 'vn' || subCmd === 'audio') {
                const asVoiceNote = subCmd === 'vn';
                await sock.sendMessage(jid, { react: { text: '🔄', key: msg.key } });

                // Procesamos Opus
                await new Promise((resolve, reject) => {
                    ffmpeg(rawAudioPath)
                        .toFormat('ogg')
                        .audioCodec('libopus')
                        .save(processedAudioPath)
                        .on('end', resolve)
                        .on('error', reject);
                });

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

                await new Promise((resolve, reject) => {
                    ffmpeg(rawAudioPath)
                        .setStartTime(startSec)
                        .setDuration(endSec - startSec)
                        .toFormat('ogg')
                        .audioCodec('libopus')
                        .save(processedAudioPath)
                        .on('end', resolve)
                        .on('error', reject);
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
                text: `🎵 *Editor de audio AU*\nComandos:\n- \`.au vn\` → convierte el audio a nota de voz.\n- \`.au audio\` → convierte a audio normal.\n- \`.au r <inicio> <fin> [vn]\` → recorta del segundo A al B. Agrega "vn" al final para que sea nota de voz.\nEjemplo: \`.au r 2,5 8.3 vn\``
            }, { quoted: msg });
            await sock.sendMessage(jid, { react: { text: 'ℹ️', key: msg.key } });

        } catch (error) {
            console.error('Error en AU:', error);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
            await sock.sendMessage(jid, { text: `❌ Error procesando audio: ${error.message}` }, { quoted: msg });
        } finally {
            if (tempDir) await fs.remove(tempDir).catch(() => {});
        }
    }
};
