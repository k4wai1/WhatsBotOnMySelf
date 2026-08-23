// addons/voz.js
// Texto → nota de voz natural con edge-tts (Microsoft Edge TTS, gratis).
// .voz <texto>            → voz por defecto (es-VE-SebastianNeural)
// .voz @es-MX-DaliaNeural <texto>
// .voz voces              → lista de voces recomendadas

const fs = require('fs-extra');
const path = require('path');
const os = require('os');
const { execFile } = require('child_process');
const { promisify } = require('util');
const execFileP = promisify(execFile);
const ffmpeg = require('fluent-ffmpeg');

const DEFAULT_VOICE = 'es-VE-SebastianNeural';
const MAX_TEXT = 2000;
const VOICE_RE = /^[a-z]{2,3}-[A-Z]{2}-[A-Za-z]+Neural$/;

const VOICES = [
    ['es-VE-SebastianNeural', '🇻🇪 Sebastián (por defecto)'],
    ['es-VE-PaolaNeural', '🇻🇪 Paola'],
    ['es-MX-JorgeNeural', '🇲🇽 Jorge'],
    ['es-MX-DaliaNeural', '🇲🇽 Dalia'],
    ['es-CO-SalomeNeural', '🇨🇴 Salomé'],
    ['es-ES-AlvaroNeural', '🇪🇸 Álvaro'],
    ['es-ES-ElviraNeural', '🇪🇸 Elvira'],
    ['es-US-AlonsoNeural', '🇺🇸 Alonso (neutro)'],
    ['en-US-AriaNeural', '🇺🇸 Aria (inglés)'],
    ['pt-BR-AntonioNeural', '🇧🇷 Antônio (portugués)'],
];

const cooldowns = new Map();

function findEdgeTts() {
    const candidates = [
        process.env.EDGE_TTS_BIN,
        'edge-tts',
        path.join(os.homedir(), '.local/bin/edge-tts'),
        path.join(os.homedir(), '.pyenv/versions/3.10.14/bin/edge-tts'),
        '/usr/local/bin/edge-tts',
    ].filter(Boolean);
    return candidates.find(c => {
        try { return require('fs').existsSync(c) || !c.includes('/'); } catch { return false; }
    });
}

module.exports = {
    commands: ['voz', 'tts'],

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;

        const now = Date.now();
        const last = cooldowns.get(jid) || 0;
        if (now - last < 3000) {
            await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });
            return;
        }
        cooldowns.set(jid, now);

        let voice = DEFAULT_VOICE;
        let rest = args.slice();

        if (rest[0] && rest[0].startsWith('@')) {
            const v = rest[0].slice(1);
            if (!VOICE_RE.test(v)) {
                await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
                await sock.sendMessage(jid, { text: '❌ Voz inválida. Formato: `@es-MX-DaliaNeural`. Mira `.voz voces`.' }, { quoted: msg });
                return;
            }
            voice = v;
            rest = rest.slice(1);
        }

        if (rest[0]?.toLowerCase() === 'voces') {
            const lista = VOICES.map(([v, d]) => `• \`.voz @${v} texto\`\n   ${d}`).join('\n');
            await sock.sendMessage(jid, { text: `🎙️ *Voces recomendadas:*\n\n${lista}\n\nTambién sirven otras de la lista oficial de Edge (formato xx-XX-NombreNeural).` }, { quoted: msg });
            return;
        }

        const text = rest.join(' ').trim();
        if (!text) {
            await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
            await sock.sendMessage(jid, {
                text: '🎙️ Convierte texto a nota de voz.\n\n• `.voz hola, esto es una prueba`\n• `.voz @es-MX-DaliaNeural hola`\n• `.voz voces` para ver voces'
            }, { quoted: msg });
            return;
        }

        const bin = findEdgeTts();
        if (!bin) {
            console.error('❌ [voz] edge-tts no encontrado');
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
            await sock.sendMessage(jid, { text: '❌ edge-tts no está instalado en el servidor.\nInstálalo con: `pip install edge-tts`' }, { quoted: msg });
            return;
        }

        const tmpDir = path.join(os.tmpdir(), `voz_${Date.now()}`);
        const mp3Path = path.join(tmpDir, 'out.mp3');
        const oggPath = path.join(tmpDir, 'out.ogg');

        try {
            await sock.sendMessage(jid, { react: { text: '🎙️', key: msg.key } });
            await fs.ensureDir(tmpDir);

            // 1) Sintetizar MP3
            try {
                await execFileP(bin, [
                    '--voice', voice,
                    '--text', text.slice(0, MAX_TEXT),
                    '--write-media', mp3Path
                ], { timeout: 60000 });
            } catch (e) {
                throw new Error(`edge-tts falló${voice !== DEFAULT_VOICE ? ' (¿voz existente?)' : ''}: ${e.message.slice(0, 120)}`);
            }

            if (!fs.existsSync(mp3Path) || fs.statSync(mp3Path).size < 100) {
                throw new Error('audio vacío generado');
            }

            // 2) Normalizar a OGG/Opus 48kHz (compatible WhatsApp)
            await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });
            await new Promise((resolve, reject) => {
                ffmpeg(mp3Path)
                    .noVideo()
                    .audioFrequency(48000)
                    .toFormat('ogg')
                    .audioCodec('libopus')
                    .save(oggPath)
                    .on('end', resolve)
                    .on('error', reject);
            });

            const buffer = await fs.readFile(oggPath);

            // 3) Enviar como nota de voz
            await sock.sendMessage(jid, {
                audio: buffer,
                mimetype: 'audio/ogg; codecs=opus',
                ptt: true
            }, { quoted: msg });

            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

        } catch (error) {
            console.error('❌ [voz]:', error.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
            await sock.sendMessage(jid, { text: `❌ No pude generar la voz: ${error.message}` }, { quoted: msg });
        } finally {
            await fs.remove(tmpDir).catch(() => {});
        }
    }
};
