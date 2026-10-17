// addons/ocr.js
// Extracción de texto de imágenes (OCR) vía api.ocr.space.
// Portado del bot de rupa y adaptado al router de addons.
// Requiere OCR_SPACE_KEY en .env (https://ocr.space/ocrapi)
const { downloadMediaMessage } = require('@whiskeysockets/baileys');

const OCR_API_KEY = process.env.OCR_SPACE_KEY || 'helloworld'; // 'helloworld' = key gratis de pruebas

module.exports = {
    commands: ['ocr'],

    handler: async (sock, msg) => {
        const jid = msg.key.remoteJid;

        try {
            let buffer = null;
            let mimetype = null;

            if (msg.message?.imageMessage) {
                buffer = await downloadMediaMessage(msg, 'buffer', {});
                mimetype = msg.message.imageMessage.mimetype;
            } else if (msg.message?.extendedTextMessage?.contextInfo?.quotedMessage?.imageMessage) {
                // Mock de mensaje para el media citado
                const quotedMsg = { message: msg.message.extendedTextMessage.contextInfo.quotedMessage };
                buffer = await downloadMediaMessage(quotedMsg, 'buffer', {});
                mimetype = quotedMsg.message.imageMessage.mimetype;
            }

            if (!buffer) {
                await sock.sendMessage(jid, { text: '❌ Por favor envía una imagen o responde a un mensaje con imagen usando el comando ocr' }, { quoted: msg });
                return;
            }

            const formData = new FormData();
            const blob = new Blob([buffer], { type: mimetype });

            formData.append('file', blob, 'image.jpg');
            formData.append('language', 'spa');
            formData.append('detectOrientation', 'true');
            formData.append('isOverlayRequired', 'false');
            formData.append('apikey', OCR_API_KEY);

            const response = await fetch('https://api.ocr.space/parse/image', {
                method: 'POST',
                body: formData
            });

            if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
            const data = await response.json();

            if (data?.ParsedResults?.length > 0) {
                const parsedText = data.ParsedResults[0].ParsedText;
                if (parsedText?.trim()) {
                    await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
                    await sock.sendMessage(jid, { text: `📝 *Texto extraído:*\n\n${parsedText.trim()}` }, { quoted: msg });
                } else {
                    await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                    await sock.sendMessage(jid, { text: '❌ No se detectó texto en la imagen.' }, { quoted: msg });
                }
            } else {
                await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                await sock.sendMessage(jid, { text: '❌ Error al procesar la imagen.' }, { quoted: msg });
            }

        } catch (error) {
            console.error('❌ [ocr] Error:', error.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
            await sock.sendMessage(jid, { text: `❌ Error al procesar: ${error.message}` }, { quoted: msg });
        }
    }
};
