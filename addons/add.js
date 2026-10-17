// addons/add.js
// Agrega números/contactos al grupo (con modo interactivo de 30s)
// y fallback por enlace de invitación si la privacidad lo bloquea.
// Portado del bot de rupa y adaptado al router de addons + reacciones.
const fs = require('fs');
const path = require('path');
const { jidNormalizedUser } = require('@whiskeysockets/baileys');

const ASSETS = path.join(__dirname, '..', 'assets');
const LOG_FILE = path.join(ASSETS, 'add.log');

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
    console.log(`[ADD] ${text}`);
}

function normalizeNumber(raw) {
    if (!raw) return null;
    let clean = String(raw).replace(/\D/g, '');
    if (clean.startsWith('0') && clean.length === 11) return '58' + clean.slice(1);
    if (clean.length === 10 && (clean.startsWith('4') || clean.startsWith('2'))) return '58' + clean;
    return clean.length >= 7 ? clean : null;
}

module.exports = {
    commands: ['add'],

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;
        if (!jid.endsWith('@g.us')) return;

        // Helper interno: agrega con fallback a enlace de invitación al privado del admin
        async function handleAddWithFallback(numberInput, originalMsgKey, senderJid) {
            logAction(`Procesando número: ${numberInput}`);
            try {
                const [waResult] = await sock.onWhatsApp(`${numberInput}@s.whatsapp.net`);
                if (!waResult?.exists) {
                    await sock.sendMessage(jid, { text: `⚠️ El número *${numberInput}* no está en WhatsApp.` }, { quoted: { key: originalMsgKey } });
                    return;
                }

                const response = await sock.groupParticipantsUpdate(jid, [waResult.jid], 'add');
                const addStatus = response[0]?.status;

                if (addStatus === '200' || addStatus === 200) {
                    await sock.sendMessage(jid, { react: { text: '✅', key: originalMsgKey } });
                } else {
                    throw new Error('Privacidad activa');
                }
            } catch (err) {
                try {
                    const inviteCode = await sock.groupInviteCode(jid);
                    const inviteLink = `https://chat.whatsapp.com/${inviteCode}`;
                    await sock.sendMessage(senderJid, { text: `🔗 Enlace para *${numberInput}*:\n${inviteLink}` });
                    await sock.sendMessage(jid, { react: { text: '📨', key: originalMsgKey } });
                } catch (e) {
                    await sock.sendMessage(jid, { text: '❌ No pude generar el enlace de invitación.' });
                }
            }
        }

        try {
            const senderJid = jidNormalizedUser(msg.key.participant || jid);
            const userNumber = senderJid.split('@')[0];
            const groupMetadata = await sock.groupMetadata(jid);

            // Verificación de admin remitente (dueños o admins del grupo)
            let isAuthorized = OWNER_NUMBERS.includes(userNumber);
            const userPart = groupMetadata.participants.find(p => jidNormalizedUser(p.id) === senderJid);
            if (userPart && (userPart.admin === 'admin' || userPart.admin === 'superadmin')) isAuthorized = true;

            if (!isAuthorized) return sock.sendMessage(jid, { react: { text: '🤡', key: msg.key } });

            // Verificación de admin del bot (identidades dinámicas)
            const allBotIDs = getBotNumbers(sock);
            const botPart = groupMetadata.participants.find(
                p => allBotIDs.includes(jidNormalizedUser(p.id).split('@')[0])
            );

            if (!botPart || (botPart.admin !== 'admin' && botPart.admin !== 'superadmin')) {
                return sock.sendMessage(jid, { text: '❌ El bot necesita ser Admin para agregar personas.' }, { quoted: msg });
            }

            const rawArgs = args.join(' ').trim();
            if (rawArgs.length > 0) {
                const number = normalizeNumber(rawArgs);
                if (number) await handleAddWithFallback(number, msg.key, senderJid);
                return;
            }

            // Caso B: recolector interactivo (30s)
            const promptSent = await sock.sendMessage(jid, { text: '📩 Envía el contacto o escribe el número (30s)...' }, { quoted: msg });
            let collected = false;

            const collector = async (up) => {
                if (up.type !== 'notify') return;
                const incMsg = up.messages[0];
                if (incMsg.key.remoteJid !== jid || incMsg.key.participant !== senderJid) return;
                if (collected) return;

                let numbersToAdd = [];

                // Revisar si es vCard (contacto)
                const contactMsg = incMsg.message?.contactMessage;
                if (contactMsg) {
                    const vcard = contactMsg.vcard;
                    const matches = vcard.match(/waid=([0-9]+)/g);
                    if (matches) matches.forEach(m => numbersToAdd.push(normalizeNumber(m.split('=')[1])));
                } else {
                    // Revisar si es texto
                    const incText = incMsg.message?.conversation || incMsg.message?.extendedTextMessage?.text;
                    const num = normalizeNumber(incText);
                    if (num) numbersToAdd.push(num);
                }

                if (numbersToAdd.length > 0) {
                    collected = true;
                    await sock.sendMessage(jid, { react: { text: '⏳', key: incMsg.key } });

                    let processed = 0;
                    for (const n of numbersToAdd) {
                        if (n) { await handleAddWithFallback(n, incMsg.key, senderJid); processed++; }
                    }

                    if (processed === 0) await sock.sendMessage(jid, { text: '❌ No se detectó un número válido.', edit: promptSent.key });
                    sock.ev.off('messages.upsert', collector);
                }
            };

            sock.ev.on('messages.upsert', collector);
            setTimeout(() => {
                if (!collected) {
                    sock.sendMessage(jid, { delete: promptSent.key });
                    sock.ev.off('messages.upsert', collector);
                }
            }, 30000);

        } catch (err) {
            logAction(`💥 Error crítico: ${err.message}`);
            console.error('❌ [add] Error:', err.message);
            await sock.sendMessage(msg.key.remoteJid, { react: { text: '❌', key: msg.key } });
        }
    }
};
