// addons/fx.js
// Responde a una imagen/sticker con .fx <efecto>
// Efectos: blur, gris, sepia, espejo

const sharp = require('sharp');
const { downloadMediaMessage } = require('@whiskeysockets/baileys');

const MAX_BYTES = 15 * 1024 * 1024;

const EFECTOS = {
    blur: (p) => p.blur(8),
    gris: (p) => p.grayscale(),
    sepia: (p) => p.grayscale().tint({ r: 255, g: 220, b: 177 }),
    espejo: (p) => p.flop()
};

const AYUDA = [
    '🎨 *Efectos de imagen*',
    '',
    'Responde a una imagen o sticker con:',
    '• `.fx blur` — desenfoque',
    '• `.fx gris` — blanco y negro',
    '• `.fx sepia` — tono vintage',
    '• `.fx espejo` — reflejo horizontal'
].join('\n');

async function obtenerImagen(msg) {
    const ctx = msg.message?.extendedTextMessage?.contextInfo;
    const quoted = ctx?.quotedMessage;
    const source = msg.message.imageMessage ? msg : (quoted?.imageMessage || quoted?.stickerMessage ? { message: quoted } : null);
    if (!source) return null;

    const buffer = await downloadMediaMessage(source, 'buffer', {});
    return buffer && buffer.length && buffer.length <= MAX_BYTES ? buffer : null;
}

module.exports = {
    commands: ['fx', 'efecto'],

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;
        try {
            const efecto = (args[0] || '').toLowerCase();
            if (!EFECTOS[efecto]) {
                await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
                await sock.sendMessage(jid, { text: AYUDA }, { quoted: msg });
                return;
            }

            await sock.sendMessage(jid, { react: { text: '🔄', key: msg.key } });

            const input = await obtenerImagen(msg);
            if (!input) {
                await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                await sock.sendMessage(jid, { text: 'No encontré imagen. Responde a una foto, imagen o sticker estático.' }, { quoted: msg });
                return;
            }

            let pipeline = sharp(input, { animated: false }).ensureAlpha();
            pipeline = EFECTOS[efecto](pipeline);

            const output = await pipeline.png().toBuffer();

            await sock.sendMessage(jid, { image: output, caption: `✨ ${efecto}` }, { quoted: msg });
            await sock.sendMessage(jid, { react: { text: '✨', key: msg.key } });

        } catch (error) {
            console.error('❌ [fx]:', error.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } }).catch(() => {});
        }
    }
};
