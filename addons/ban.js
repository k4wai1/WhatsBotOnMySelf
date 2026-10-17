// addons/ban.js
// Expulsa usuarios del grupo con ban temporal opcional (auto-reincorporación).
// Portado del bot de rupa y adaptado al router de addons + reacciones.
const fs = require('fs');
const path = require('path');
const { jidNormalizedUser } = require('@whiskeysockets/baileys');

const ASSETS = path.join(__dirname, '..', 'assets');
const BAN_FILE = path.join(ASSETS, 'banlist.json');
const LOG_FILE = path.join(ASSETS, 'ban.log');

// 👑 Números con privilegios de dueño: OWNER_NUMBERS=584...,182... en .env (opcional)
const OWNER_NUMBERS = (process.env.OWNER_NUMBERS || '')
    .split(',')
    .map(n => n.replace(/\D/g, ''))
    .filter(Boolean);

// Identidades del bot (dinámicas + BOT_NUMBERS del .env si hiciera falta)
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

function readBanList() {
    try {
        if (!fs.existsSync(BAN_FILE)) return [];
        const data = JSON.parse(fs.readFileSync(BAN_FILE, 'utf8'));
        return Array.isArray(data) ? data : [];
    } catch (err) { return []; }
}

function writeBanList(list) {
    try { fs.writeFileSync(BAN_FILE, JSON.stringify(list, null, 2)); } catch (err) {}
}

function logAction(text) {
    try { fs.appendFileSync(LOG_FILE, `[${new Date().toISOString()}] ${text}\n`); } catch (e) {}
    console.log(`[BAN] ${text}`);
}

function sanitizePhoneNumber(rawInput) {
    let clean = String(rawInput).replace(/\D/g, '');
    if (clean.startsWith('0') && clean.length === 11) return '58' + clean.slice(1);
    if (clean.length === 10 && (clean.startsWith('4') || clean.startsWith('2'))) return '58' + clean;
    return clean;
}

module.exports = {
    commands: ['ban'],

    // ♻️ Tarea de reincorporación automática (revisa cada minuto)
    init: (sock) => {
        setInterval(async () => {
            const bans = readBanList();
            if (bans.length === 0) return;
            const now = Date.now();
            const remaining = [];

            for (const ban of bans) {
                if (ban.until <= now) {
                    try {
                        const targetJid = ban.fullJid || `${ban.number}@s.whatsapp.net`;
                        await sock.groupParticipantsUpdate(ban.chatId, [targetJid], 'add');
                        logAction(`♻️ Usuario ${ban.number} reincorporado.`);
                    } catch (err) { remaining.push(ban); }
                } else { remaining.push(ban); }
            }
            writeBanList(remaining);
        }, 60 * 1000);
    },

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;
        if (!jid.endsWith('@g.us')) return;

        try {
            await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

            // 1. Identificar al remitente y verificar permisos
            const senderJid = jidNormalizedUser(msg.key.participant || jid);
            const userNumber = senderJid.split('@')[0];
            const groupMetadata = await sock.groupMetadata(jid);

            let isAuthorized = OWNER_NUMBERS.includes(userNumber);
            const userParticipant = groupMetadata.participants.find(
                p => jidNormalizedUser(p.id) === senderJid || p.id === senderJid
            );
            if (userParticipant && (userParticipant.admin === 'admin' || userParticipant.admin === 'superadmin')) {
                isAuthorized = true;
            }

            if (!isAuthorized) {
                await sock.sendMessage(jid, { react: { text: '🤡', key: msg.key } });
                return;
            }

            // 2. Extraer objetivos (menciones, cita o número)
            const rawArgs = args.join(' ').trim();
            let targetJids = [];
            let minutes = 0;

            const quotedInfo = msg.message?.extendedTextMessage?.contextInfo;

            if (quotedInfo?.mentionedJid?.length > 0) {
                targetJids = quotedInfo.mentionedJid.map(id => jidNormalizedUser(id));
                const lastArg = args[args.length - 1];
                if (lastArg && !isNaN(lastArg)) minutes = parseInt(lastArg);
            }
            else if (quotedInfo?.participant) {
                targetJids = [jidNormalizedUser(quotedInfo.participant)];
                const firstArg = args[0];
                if (firstArg && !isNaN(firstArg)) minutes = parseInt(firstArg);
            }
            else if (rawArgs.length > 0) {
                const timeMatch = rawArgs.match(/\s(\d+)$/);
                let numberPartRaw = rawArgs;
                if (timeMatch) {
                    minutes = parseInt(timeMatch[1]);
                    numberPartRaw = rawArgs.substring(0, timeMatch.index);
                }
                const cleanedNumber = sanitizePhoneNumber(numberPartRaw);
                if (cleanedNumber.length >= 10) {
                    const foundParticipant = groupMetadata.participants.find(p => p.id.startsWith(cleanedNumber));
                    targetJids = [foundParticipant ? foundParticipant.id : `${cleanedNumber}@s.whatsapp.net`];
                }
            }

            if (targetJids.length === 0) {
                await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
                return;
            }

            // 3. Verificar que el bot sea admin
            const allKnownBotIDs = getBotNumbers(sock);
            const botParticipant = groupMetadata.participants.find(p => {
                const pNum = jidNormalizedUser(p.id).split('@')[0];
                return allKnownBotIDs.includes(pNum);
            });

            if (!botParticipant || (botParticipant.admin !== 'admin' && botParticipant.admin !== 'superadmin')) {
                await sock.sendMessage(jid, { text: '❌ Necesito ser administrador del grupo para esto.' }, { quoted: msg });
                await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                return;
            }

            // 4. Ejecutar expulsión
            await sock.sendMessage(jid, { react: { text: '🔨', key: msg.key } });
            const bans = readBanList();
            let successCount = 0;

            // Protegidos: dueños y el bot
            const protectedNumbers = [...OWNER_NUMBERS, ...allKnownBotIDs];

            for (const targetId of targetJids) {
                const targetNum = targetId.split('@')[0];

                // Fuego amigo
                if (protectedNumbers.includes(targetNum)) continue;

                const targetParticipant = groupMetadata.participants.find(
                    p => jidNormalizedUser(p.id) === targetId || p.id === targetId
                );
                if (targetParticipant && (targetParticipant.admin === 'admin' || targetParticipant.admin === 'superadmin')) {
                    continue;
                }

                try {
                    await sock.groupParticipantsUpdate(jid, [targetId], 'remove');
                    successCount++;

                    if (minutes > 0) {
                        bans.push({
                            chatId: jid,
                            number: targetNum,
                            fullJid: targetId,
                            until: Date.now() + minutes * 60 * 1000
                        });
                    }
                } catch (err) {
                    logAction(`❌ Error al expulsar ${targetNum}: ${err.message}`);
                }
            }

            if (minutes > 0) writeBanList(bans);
            await sock.sendMessage(jid, { react: { text: successCount > 0 ? '✅' : '⚠️', key: msg.key } });

        } catch (err) {
            logAction(`💥 Error: ${err.message}`);
            console.error('❌ [ban] Error:', err.message);
            await sock.sendMessage(msg.key.remoteJid, { react: { text: '❌', key: msg.key } });
        }
    }
};
