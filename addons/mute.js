// addons/mute.js
// Cierra/abre el grupo con minutos opcionales (auto-unmute).
// Portado del bot de rupa y adaptado al router de addons + reacciones.
const fs = require('fs');
const path = require('path');
const { jidNormalizedUser } = require('@whiskeysockets/baileys');

const ASSETS = path.join(__dirname, '..', 'assets');
const LOG_FILE = path.join(ASSETS, 'mute.log');
const DATA_FILE = path.join(ASSETS, 'mutes.json');

// 👑 Números con privilegios de dueño: OWNER_NUMBERS=584...,182... en .env (opcional)
const OWNER_NUMBERS = (process.env.OWNER_NUMBERS || '')
    .split(',')
    .map(n => n.replace(/\D/g, ''))
    .filter(Boolean);

function getBotNumbers(sock) {
    const nums = new Set();
    const ids = [sock.user?.id, sock.authState?.creds?.me?.id, sock.authState?.creds?.me?.lid];
    for (const id of ids.filter(Boolean)) {
        const clean = jidNormalizedUser(id).split('@')[0];
        if (clean) nums.add(clean);
    }
    for (const n of (process.env.BOT_NUMBERS || '').split(',')) {
        const clean = n.replace(/\D/g, '');
        if (clean) nums.add(clean);
    }
    return [...nums];
}

function logAction(text) {
    try { fs.appendFileSync(LOG_FILE, `[${new Date().toISOString()}] ${text}\n`); } catch (e) {}
    console.log(`[MUTE] ${text}`);
}

function loadMutes() {
    try {
        if (!fs.existsSync(DATA_FILE)) return { groups: {} };
        const raw = fs.readFileSync(DATA_FILE, 'utf8');
        return JSON.parse(raw || '{}') || { groups: {} };
    } catch (e) { return { groups: {} }; }
}

function saveMutes(data) {
    try { fs.writeFileSync(DATA_FILE, JSON.stringify(data, null, 2)); } catch (e) {}
}

module.exports = {
    commands: ['mute', 'unmute'],

    // 🔓 Auto-unmute: revisa cada minuto los mutes temporales vencidos
    init: (sock) => {
        setInterval(async () => {
            try {
                const data = loadMutes();
                const now = Date.now();
                const entries = Object.entries(data.groups || {});
                if (entries.length === 0) return;

                for (const [groupId, info] of entries) {
                    if (!info || !info.until) continue;

                    if (now >= Number(info.until)) {
                        try {
                            // 'not_announcement' permite que todos envíen mensajes
                            await sock.groupSettingUpdate(groupId, 'not_announcement');
                            await sock.sendMessage(groupId, { text: '🔓 Tiempo cumplido. Chat abierto.' });
                            logAction(`🔓 Auto-Unmute en ${groupId}`);
                        } catch (e) {
                            logAction(`⚠️ Error Auto-Unmute ${groupId}: ${e.message}`);
                        }
                        delete data.groups[groupId];
                        saveMutes(data);
                    }
                }
            } catch (e) {}
        }, 60 * 1000);
    },

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;
        if (!jid.endsWith('@g.us')) return;

        const rawText = msg.message?.conversation || msg.message?.extendedTextMessage?.text || '';
        const commandUsed = rawText.trim().split(/\s+/)[0].toLowerCase().replace(/^[.!/,]/, '');

        try {
            const senderJid = jidNormalizedUser(msg.key.participant || jid);
            const userNumber = senderJid.split('@')[0];
            const groupMetadata = await sock.groupMetadata(jid);

            // 1. Autorización del usuario
            let isAuthorized = OWNER_NUMBERS.includes(userNumber);
            const userPart = groupMetadata.participants.find(p => jidNormalizedUser(p.id) === senderJid);
            if (userPart && (userPart.admin === 'admin' || userPart.admin === 'superadmin')) {
                isAuthorized = true;
            }

            if (!isAuthorized) {
                await sock.sendMessage(jid, { react: { text: '⛔', key: msg.key } });
                return;
            }

            // 2. Autorización del bot (detección de admin con múltiples IDs)
            const allBotIDs = getBotNumbers(sock);
            const botPart = groupMetadata.participants.find(
                p => allBotIDs.includes(jidNormalizedUser(p.id).split('@')[0])
            );

            if (!botPart || (botPart.admin !== 'admin' && botPart.admin !== 'superadmin')) {
                await sock.sendMessage(jid, { text: '❌ El bot no es Admin aquí.' }, { quoted: msg });
                return;
            }

            const data = loadMutes();

            if (commandUsed === 'mute') {
                let minutes = null;
                let until = null;

                if (args[0]) {
                    const parsed = Math.round(parseFloat(args[0]));
                    if (Number.isFinite(parsed) && parsed > 0) {
                        minutes = parsed;
                        until = Date.now() + minutes * 60 * 1000;
                    } else {
                        await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
                        return;
                    }
                }

                // 'announcement' restringe mensajes solo a administradores
                await sock.groupSettingUpdate(jid, 'announcement');

                data.groups[jid] = { until, by: userNumber, mutedAt: Date.now(), chatName: groupMetadata.subject };
                saveMutes(data);

                if (until === null) {
                    await sock.sendMessage(jid, { react: { text: '🔇', key: msg.key } });
                } else {
                    await sock.sendMessage(jid, { react: { text: '⏱️', key: msg.key } });
                    await sock.sendMessage(jid, { text: `🔒 Chat cerrado por ${minutes} minutos.` }, { quoted: msg });
                }
                logAction(`Mute (${minutes || 'inf'}) en ${jid}`);
            }

            if (commandUsed === 'unmute') {
                await sock.groupSettingUpdate(jid, 'not_announcement');

                if (data.groups[jid]) {
                    delete data.groups[jid];
                    saveMutes(data);
                }

                await sock.sendMessage(jid, { react: { text: '🔊', key: msg.key } });
                logAction(`Unmute en ${jid}`);
            }

        } catch (err) {
            logAction(`💥 Error: ${err.message}`);
            console.error('❌ [mute] Error:', err.message);
            await sock.sendMessage(msg.key.remoteJid, { react: { text: '❌', key: msg.key } });
        }
    }
};
