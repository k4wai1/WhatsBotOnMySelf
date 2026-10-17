// addons/pin.js
// Fija/desfija mensajes citados en el grupo (24h / 7d / 30d).
// Portado del bot de rupa y adaptado al router de addons + reacciones.
const { jidNormalizedUser } = require('@whiskeysockets/baileys');

// 👑 Números con privilegios de dueño: OWNER_NUMBERS=584...,182... en .env (opcional)
const OWNER_NUMBERS = (process.env.OWNER_NUMBERS || '')
    .split(',')
    .map(n => n.replace(/\D/g, ''))
    .filter(Boolean);

module.exports = {
    commands: ['pin', 'unpin'],

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;

        const rawText = msg.message?.conversation || msg.message?.extendedTextMessage?.text || '';
        const commandUsed = rawText.trim().split(/\s+/)[0].toLowerCase().replace(/^[.!/,]/, '');

        try {
            // 1. Verificación de permisos (solo admins o dueños)
            const senderJid = jidNormalizedUser(msg.key.participant || jid);
            const userNumber = senderJid.split('@')[0];
            let isAuthorized = OWNER_NUMBERS.includes(userNumber);

            if (jid.endsWith('@g.us')) {
                const groupMetadata = await sock.groupMetadata(jid);
                const userPart = groupMetadata.participants.find(p => jidNormalizedUser(p.id) === senderJid);
                if (userPart && (userPart.admin === 'admin' || userPart.admin === 'superadmin')) {
                    isAuthorized = true;
                }
            }

            if (!isAuthorized) {
                await sock.sendMessage(jid, { react: { text: '⛔', key: msg.key } });
                return;
            }

            // 2. Extraer el mensaje citado
            const contextInfo = msg.message?.extendedTextMessage?.contextInfo;
            if (!contextInfo || !contextInfo.stanzaId) {
                await sock.sendMessage(jid, { text: '⚠️ Debes responder al mensaje que deseas fijar o desfijar.' }, { quoted: msg });
                return;
            }

            const keyToPin = {
                remoteJid: jid,
                id: contextInfo.stanzaId,
                participant: contextInfo.participant
            };

            // 3. Desfijar (.unpin)
            if (commandUsed === 'unpin') {
                await sock.sendMessage(jid, {
                    pin: keyToPin,
                    type: 2 // 2 significa Unpin
                });
                await sock.sendMessage(jid, { react: { text: '📍', key: msg.key } });
                return;
            }

            // 4. Fijar (.pin)
            if (commandUsed === 'pin') {
                let durationInSeconds = 86400; // Por defecto: 24 horas

                if (args[0] === '1') {
                    durationInSeconds = 604800; // 7 días
                } else if (args[0] === '2') {
                    durationInSeconds = 2592000; // 30 días (WhatsApp estándar)
                }

                await sock.sendMessage(jid, {
                    pin: keyToPin,
                    type: 1, // 1 significa Pin
                    time: durationInSeconds
                });

                await sock.sendMessage(jid, { react: { text: '📌', key: msg.key } });
            }

        } catch (err) {
            console.error(`❌ [PIN] Error: ${err.message}`);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
        }
    }
};
