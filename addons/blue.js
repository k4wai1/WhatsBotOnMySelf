// addons/blue.js
// Ejercicios de integrales aleatorios (imágenes locales en assets/blue), 4 usos / 10 min.
// Portado del bot de rupa y adaptado al router de addons.
const fs = require('fs');
const path = require('path');

const IMG_FOLDER = path.join(__dirname, '..', 'assets', 'blue');
const LIMIT = 4;
const WINDOW_MS = 10 * 60 * 1000;
const userRequests = new Map();

module.exports = {
    commands: ['blue'],

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

            const files = fs.readdirSync(IMG_FOLDER).filter(file => /\.(jpg|jpeg|png|gif)$/i.test(file));

            if (files.length === 0) {
                await sock.sendMessage(jid, { text: 'No hay ejercicios' }, { quoted: msg });
                return;
            }

            const randomFile = files[Math.floor(Math.random() * files.length)];
            const buffer = fs.readFileSync(path.join(IMG_FOLDER, randomFile));

            await sock.sendMessage(jid, { image: buffer, caption: 'Unas integrales?' }, { quoted: msg });

            setTimeout(() => {
                userRequests.set(userId, Math.max((userRequests.get(userId) || 1) - 1, 0));
            }, WINDOW_MS);

        } catch (err) {
            console.error('❌ [blue] Error:', err.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
        }
    }
};
