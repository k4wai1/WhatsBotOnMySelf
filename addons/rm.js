// addons/rm.js
// Borrado masivo/inteligente de mensajes del grupo con filtros de tipo y usuario.
// Portado del bot de rupa, adaptado al router de addons y con buffer propio de mensajes.
const { jidNormalizedUser } = require('@whiskeysockets/baileys');

// 👑 Números con privilegios de dueño: OWNER_NUMBERS=584...,182... en .env (opcional)
const OWNER_NUMBERS = (process.env.OWNER_NUMBERS || '')
    .split(',')
    .map(n => n.replace(/\D/g, ''))
    .filter(Boolean);

// Función auxiliar para crear la pausa (cooldown)
const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// Buffer propio de mensajes recientes por chat (reemplaza al store de rupa)
const MAX_BUFFER = 250;
const msgBuffer = new Map(); // jid -> [{ key, message }]

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

module.exports = {
    commands: ['rm'],

    // Buffer en memoria de los últimos mensajes de cada chat
    init: (sock) => {
        sock.ev.on('messages.upsert', (m) => {
            if (m.type !== 'notify') return;
            for (const msg of m.messages) {
                if (!msg.message) continue;
                const jid = msg.key.remoteJid;
                if (!jid) continue;
                if (!msgBuffer.has(jid)) msgBuffer.set(jid, []);
                const arr = msgBuffer.get(jid);
                arr.push({ key: msg.key, message: msg.message });
                if (arr.length > MAX_BUFFER) arr.splice(0, arr.length - MAX_BUFFER);
            }
        });
    },

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;
        if (!jid.endsWith('@g.us')) return;

        try {
            const senderJid = msg.key.participant || jid;
            const userNumber = jidNormalizedUser(senderJid).split('@')[0];

            let isAuthorized = OWNER_NUMBERS.includes(userNumber);
            const groupMetadata = await sock.groupMetadata(jid);
            const userParticipant = groupMetadata.participants.find(
                p => p.id === senderJid || jidNormalizedUser(p.id) === jidNormalizedUser(senderJid)
            );

            if (userParticipant && (userParticipant.admin === 'admin' || userParticipant.admin === 'superadmin')) {
                isAuthorized = true;
            }

            if (!isAuthorized) {
                await sock.sendMessage(jid, { text: '⛔ Comando de moderación: Requiere Admin.' }, { quoted: msg });
                return;
            }

            const botJid = jidNormalizedUser(sock.user.id);
            const allBotIDs = getBotNumbers(sock);
            const botParticipant = groupMetadata.participants.find(p => {
                const pNum = jidNormalizedUser(p.id).split('@')[0];
                return allBotIDs.includes(pNum);
            });

            if (botParticipant && botParticipant.admin !== 'admin' && botParticipant.admin !== 'superadmin') {
                await sock.sendMessage(jid, { text: '❌ No puedo borrar mensajes de otros si no soy Administrador.' }, { quoted: msg });
                return;
            }

            const quotedInfo = msg.message?.extendedTextMessage?.contextInfo;

            let count = 1;
            let targetFormat = 'all';
            let targetUser = null;

            if (quotedInfo && quotedInfo.mentionedJid && quotedInfo.mentionedJid.length > 0) {
                targetUser = quotedInfo.mentionedJid[0];
            }

            for (const arg of args) {
                if (!isNaN(arg)) {
                    count = parseInt(arg);
                } else if (arg.startsWith('@')) {
                    if (!targetUser) {
                        const possibleNumber = arg.replace(/[^0-9]/g, '');
                        if (possibleNumber) {
                            targetUser = possibleNumber + '@s.whatsapp.net';
                        }
                    }
                } else {
                    targetFormat = arg.toLowerCase();
                }
            }

            count = Math.min(Math.max(count, 1), 100);

            if (count === 1 && targetFormat === 'all' && !targetUser && quotedInfo?.stanzaId) {
                const isMsgFromBot = jidNormalizedUser(quotedInfo.participant) === botJid;
                await sock.sendMessage(jid, {
                    delete: {
                        remoteJid: jid,
                        fromMe: isMsgFromBot,
                        id: quotedInfo.stanzaId,
                        participant: quotedInfo.participant
                    }
                });
                return await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
            }

            const msgs = msgBuffer.get(jid) || [];
            if (msgs.length === 0) {
                return sock.sendMessage(jid, { text: '❌ No tengo un registro de mensajes en memoria para este chat.' }, { quoted: msg });
            }

            const isQuoting = !!quotedInfo?.stanzaId;
            const anchorId = quotedInfo?.stanzaId || msg.key.id;
            let anchorIndex = msgs.findIndex(m => m.key.id === anchorId);

            if (isQuoting && anchorIndex === -1) {
                return sock.sendMessage(jid, { text: `❌ El mensaje citado es muy antiguo y ya no está en mi memoria reciente (límite: ${MAX_BUFFER} mensajes).` }, { quoted: msg });
            }

            if (anchorIndex === -1) anchorIndex = msgs.length - 1;

            let messagesToDelete = [];

            const getDetailedMsgType = (mObject) => {
                if (!mObject.message) return null;
                let content = mObject.message;

                if (content.ephemeralMessage) content = content.ephemeralMessage.message;
                if (content.viewOnceMessage) content = content.viewOnceMessage.message;
                if (content.viewOnceMessageV2) content = content.viewOnceMessageV2.message;
                if (content.documentWithCaptionMessage) content = content.documentWithCaptionMessage.message;

                if (content.imageMessage) return 'img';
                if (content.videoMessage) return content.videoMessage.gifPlayback ? 'gif' : 'video';
                if (content.audioMessage) return content.audioMessage.ptt ? 'ptt' : 'audio';
                if (content.stickerMessage) return 'sticker';
                if (content.documentMessage) return 'doc';
                if (content.contactMessage || content.contactsArrayMessage) return 'contact';
                if (content.locationMessage || content.liveLocationMessage) return 'location';

                return 'text';
            };

            const targetNorm = targetUser ? jidNormalizedUser(targetUser) : null;

            for (let i = anchorIndex; i >= 0 && messagesToDelete.length < count; i--) {
                let mToEval = msgs[i];
                if (mToEval.key.id === msg.key.id) continue;

                const mType = getDetailedMsgType(mToEval);
                let matchesType = false;
                let matchesUser = false;

                // 1. Validar tipo (audio empareja también notas de voz)
                if (targetFormat === 'all') matchesType = true;
                else if (targetFormat === 'audio' && ['audio', 'ptt'].includes(mType)) matchesType = true;
                else if (targetFormat === mType) matchesType = true;
                else if (targetFormat === 'media' && ['img', 'video', 'gif', 'audio', 'ptt', 'sticker', 'doc'].includes(mType)) matchesType = true;

                // 2. Validar usuario (incluye mensajes del bot)
                if (!targetNorm) {
                    matchesUser = true;
                } else {
                    const msgParticipant = mToEval.key.participant ? jidNormalizedUser(mToEval.key.participant) : null;

                    // En grupos, los mensajes de otros usuarios SIEMPRE traen participant.
                    // Si no trae participant o tiene fromMe=true, es del bot.
                    const isMsgFromBot = mToEval.key.fromMe || !msgParticipant;

                    if (isMsgFromBot && targetNorm === botJid) {
                        matchesUser = true;
                    } else if (msgParticipant === targetNorm) {
                        matchesUser = true;
                    }
                }

                if (matchesType && matchesUser) {
                    messagesToDelete.push(mToEval);
                }
            }

            if (messagesToDelete.length === 0) {
                const errorDetail = targetUser ? ` de ese usuario` : ``;
                return sock.sendMessage(jid, { text: `🤷‍♂️ No encontré mensajes de tipo '${targetFormat}'${errorDetail} por encima de ese punto en mi memoria actual.` }, { quoted: msg });
            }

            await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

            let deletedCount = 0;
            for (const mDel of messagesToDelete) {
                const isDelFromBot = mDel.key.fromMe || (!mDel.key.participant && jid.endsWith('@g.us'));
                const participantToDel = isDelFromBot ? botJid : mDel.key.participant;

                const keyToDelete = {
                    remoteJid: jid,
                    fromMe: isDelFromBot,
                    id: mDel.key.id,
                    participant: participantToDel
                };

                try {
                    await sock.sendMessage(jid, { delete: keyToDelete });
                    deletedCount++;
                } catch (e) {
                    console.error('❌ [rm] Error borrando mensaje en bucle:', e.message);
                }

                await delay(100);
            }

            if (deletedCount > 1) {
                await sock.sendMessage(jid, { text: `🧹 Se eliminaron ${deletedCount} mensajes.` });
            } else {
                await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
            }

        } catch (err) {
            console.error(`❌ [rm] Error: ${err.message}`);
            await sock.sendMessage(msg.key.remoteJid, { react: { text: '❌', key: msg.key } });
        }
    }
};
