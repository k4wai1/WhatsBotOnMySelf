// addons/qr.js
// Generador y lector de códigos QR.
// .qr <texto/url>          → genera imagen QR
// .qr (respondiendo a una imagen/sticker) → decodifica el contenido

const QRCode = require('qrcode');
const sharp = require('sharp');
const jsQR = require('jsqr');
const { downloadContentFromMessage } = require('@whiskeysockets/baileys');

const MAX_TEXT = 1000;
const MAX_IMAGE_BYTES = 8 * 1024 * 1024;

async function decodeQrFromBuffer(imageBuffer) {
    // Se prueba a varias escalas: los QR pequeños en fotos grandes suelen fallar
    for (const width of [800, 1200, 500, 1600]) {
        const { data, info } = await sharp(imageBuffer)
            .resize({ width, height: width, fit: 'inside', withoutEnlargement: true })
            .ensureAlpha()
            .raw()
            .toBuffer({ resolveWithObject: true });
        const code = jsQR(new Uint8ClampedArray(data), info.width, info.height);
        if (code && code.data) return code.data;
    }
    return null;
}

async function downloadQuotedImage(quotedMsg) {
    const img = quotedMsg?.imageMessage;
    if (!img) return null;
    const stream = await downloadContentFromMessage(img, 'image');
    let buf = Buffer.from([]);
    for await (const chunk of stream) buf = Buffer.concat([buf, chunk]);
    return buf.length ? buf : null;
}

module.exports = {
    commands: ['qr'],

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;
        try {
            const text = args.join(' ').trim();
            const contextInfo = msg.message?.extendedTextMessage?.contextInfo;
            const quotedMsg = contextInfo?.quotedMessage;

            // ── Modo decodificar: sin texto pero con imagen adjunta o citada ──
            if (!text) {
                const sourceImg = msg.message?.imageMessage
                    ? (await downloadContentFromMessage(msg.message.imageMessage, 'image'))
                    : null;

                let buffer = null;
                if (sourceImg) {
                    let b = Buffer.from([]);
                    for await (const chunk of sourceImg) b = Buffer.concat([b, chunk]);
                    buffer = b;
                } else if (quotedMsg) {
                    buffer = await downloadQuotedImage(quotedMsg);
                }

                if (!buffer) {
                    await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
                    await sock.sendMessage(jid, {
                        text: '📱 *Códigos QR*\n\n• `.qr <texto o url>` → genera un QR\n• Responde a una imagen con `.qr` → lee su contenido'
                    }, { quoted: msg });
                    return;
                }

                if (buffer.length > MAX_IMAGE_BYTES) {
                    await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
                    await sock.sendMessage(jid, { text: '❌ Imagen demasiado grande (máx 8MB).' }, { quoted: msg });
                    return;
                }

                await sock.sendMessage(jid, { react: { text: '🔍', key: msg.key } });
                const decoded = await decodeQrFromBuffer(buffer);

                if (!decoded) {
                    await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                    await sock.sendMessage(jid, { text: 'No encontré ningún QR legible en esa imagen. Acércala, mejora la luz o mándala en mejor calidad.' }, { quoted: msg });
                    return;
                }

                await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
                const safe = decoded.length > 1500 ? decoded.slice(0, 1500) + '…' : decoded;
                await sock.sendMessage(jid, { text: `🔓 *Contenido del QR:*\n\n${safe}` }, { quoted: msg });
                return;
            }

            // ── Modo generar ──
            if (text.length > MAX_TEXT) {
                await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
                await sock.sendMessage(jid, { text: `❌ Máximo ${MAX_TEXT} caracteres.` }, { quoted: msg });
                return;
            }

            await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });
            const png = await QRCode.toBuffer(text, {
                type: 'png',
                width: 640,
                margin: 2,
                errorCorrectionLevel: 'M',
                color: { dark: '#000000ff', light: '#ffffffff' }
            });

            await sock.sendMessage(jid, { image: png, caption: `📱 QR de ${text.length} caracteres` }, { quoted: msg });
            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

        } catch (error) {
            console.error('❌ [qr]:', error.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } }).catch(() => {});
        }
    }
};
