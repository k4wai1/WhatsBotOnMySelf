// addons/scan.js
// Lector de códigos QR.
// .scan (enviando una imagen con el comando como caption) → decodifica
// .scan (respondiendo a una imagen)                        → decodifica
// Devuelve únicamente el contenido del QR, sin decoración.

const sharp = require('sharp');
const jsQR = require('jsqr');
const { downloadMediaMessage } = require('@whiskeysockets/baileys');

const MAX_IMAGE_BYTES = 16 * 1024 * 1024;

// Varios tamaños: QR pequeños en fotos grandes necesitan resoluciones distintas.
const SCALES = [1200, 800, 1600, 2000, 600, 2400, null];

function getMedia(message) {
    if (message?.imageMessage) return message.imageMessage;
    if (message?.documentMessage?.mimetype?.startsWith('image/')) return message.documentMessage;
    return null;
}

async function getImageBuffer(msg) {
    if (getMedia(msg.message)) {
        return downloadMediaMessage(msg, 'buffer', {});
    }

    const context = msg.message?.extendedTextMessage?.contextInfo;
    if (context?.quotedMessage && getMedia(context.quotedMessage)) {
        const quoted = {
            key: {
                remoteJid: msg.key.remoteJid,
                id: context.stanzaId,
                participant: context.participant
            },
            message: context.quotedMessage
        };
        return downloadMediaMessage(quoted, 'buffer', {});
    }

    return null;
}

async function tryDecode(pipeline) {
    const { data, info } = await pipeline
        .ensureAlpha()
        .raw()
        .toBuffer({ resolveWithObject: true });

    const code = jsQR(new Uint8ClampedArray(data), info.width, info.height, {
        inversionAttempts: 'attemptBoth'
    });

    return code?.data || null;
}

async function decodeQrFromBuffer(imageBuffer) {
    // Primero color normal; luego escala de grises con contraste para fotos pobres.
    const variants = [
        (width) => {
            let p = sharp(imageBuffer).rotate();
            if (width) p = p.resize({ width, fit: 'inside', withoutEnlargement: false, kernel: 'lanczos3' });
            return p;
        },
        (width) => {
            let p = sharp(imageBuffer).rotate().grayscale().normalize();
            if (width) p = p.resize({ width, fit: 'inside', withoutEnlargement: false, kernel: 'lanczos3' });
            return p;
        }
    ];

    for (const build of variants) {
        for (const width of SCALES) {
            try {
                const decoded = await tryDecode(build(width));
                if (decoded) return decoded;
            } catch (_) {
                // Una variante inválida no debe cancelar el resto de intentos.
            }
        }
    }

    return null;
}

module.exports = {
    commands: ['scan'],

    decodeQrFromBuffer,

    handler: async (sock, msg) => {
        const jid = msg.key.remoteJid;

        try {
            const buffer = await getImageBuffer(msg);

            if (!buffer || !buffer.length) {
                await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
                await sock.sendMessage(jid, {
                    text: 'Envía una imagen con el comando `.scan` o responde a una imagen con `.scan`.'
                }, { quoted: msg });
                return;
            }

            if (buffer.length > MAX_IMAGE_BYTES) {
                await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
                await sock.sendMessage(jid, {
                    text: '❌ Imagen demasiado grande (máx 16 MB).'
                }, { quoted: msg });
                return;
            }

            await sock.sendMessage(jid, { react: { text: '🔍', key: msg.key } });

            const decoded = await decodeQrFromBuffer(buffer);

            if (!decoded) {
                await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                await sock.sendMessage(jid, {
                    text: 'No se encontró ningún QR legible en la imagen.'
                }, { quoted: msg });
                return;
            }

            const clean = decoded.replace(/\r\n?/g, '\n').replace(/\u0000/g, '').trim();

            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
            await sock.sendMessage(jid, { text: clean }, { quoted: msg });

        } catch (error) {
            console.error('❌ [scan]:', error.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } }).catch(() => {});
        }
    }
};
