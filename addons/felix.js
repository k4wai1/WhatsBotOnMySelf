// addons/felix.js
// Audio divertido de Felix (assets/audios/felix.mp3), 4 usos / 10 min.
// Portado del bot de rupa y adaptado al router de addons.
const fs = require('fs');
const path = require('path');

const AUDIO_PATH = path.join(__dirname, '..', 'assets', 'audios', 'felix.mp3');
const LIMIT = 4;
const WINDOW_MS = 10 * 60 * 1000;
const userRequests = new Map();

module.exports = {
    commands: ['felix'],

    handler: async (sock, msg) => {
        const jid = msg.key.remoteJid;

        try {
            const userId = msg.key.participant || jid;
            const count = userRequests.get(userId) || 0;

            if (count >= LIMIT) {
                await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                return;
            }

            userRequests.set(userId, count + 1);

            if (!fs.existsSync(AUDIO_PATH)) {
                await sock.sendMessage(jid, { text: 'El audio de Felix no está disponible.' }, { quoted: msg });
                return;
            }

            const buffer = fs.readFileSync(AUDIO_PATH);

            await sock.sendMessage(jid, {
                audio: buffer,
                mimetype: 'audio/mp4',
                ptt: false // true para enviar como nota de voz
            }, { quoted: msg });

            setTimeout(() => {
                userRequests.set(userId, Math.max((userRequests.get(userId) || 1) - 1, 0));
            }, WINDOW_MS);

        } catch (err) {
            console.error('❌ [felix] Error:', err.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
        }
    }
};
