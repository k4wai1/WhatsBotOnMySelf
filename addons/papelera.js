// addons/papelera.js
// Global: si cualquier mensaje enviado por el bot recibe la reacción 🗑 (papeleta de basura),
// se elimina ese mensaje al instante. Modo silencioso, sin comandos.
// Escucha AMBOS canales por los que las reacciones pueden llegar en este fork:
//   - messages.upsert   → msg.message.reactionMessage (key = mensaje reaccionado)
//   - messages.update   → update.reactionMessage     (key EXTERNA = mensaje reaccionado)

const { jidNormalizedUser } = require('@whiskeysockets/baileys');

function esPapelera(text) {
    const t = String(text || '').replace(/\uFE0F/g, '');
    return t === '🗑' || t === '🚮';
}

function getBotNumbers(sock) {
    const nums = new Set();
    const ids = [sock.user?.id, sock.authState?.creds?.me?.id, sock.authState?.creds?.me?.lid];
    for (const id of ids.filter(Boolean)) {
        const clean = jidNormalizedUser(id).split('@')[0].replace(/\D/g, '');
        if (clean) nums.add(clean);
    }
    for (const n of (process.env.BOT_NUMBERS || '').split(',')) {
        const clean = n.replace(/\D/g, '');
        if (clean) nums.add(clean);
    }
    return nums;
}

// ¿El mensaje al que reaccionaron fue enviado por este bot?
// La reacción llega con la key desde la perspectiva de quien reacciona.
function esMensajeDelBot(target, botNums) {
    if (!target?.id) return false;
    if (target.fromMe) {
        if (!target.participant) return true; // chat privado del propio bot
        const p = String(target.participant).split('@')[0].replace(/\D/g, '');
        return !p || botNums.has(p);
    }
    const author = target.participant
        ? String(target.participant).split('@')[0].replace(/\D/g, '')
        : String(target.remoteJid || '').split('@')[0];
    return botNums.has(author);
}

async function manejarPapelera(sock, target, text, de) {
    if (!target || !esPapelera(text)) return;

    const botNums = getBotNumbers(sock);
    const jid = target.remoteJid;
    if (!jid || jid.startsWith('status@')) return;

    if (!esMensajeDelBot(target, botNums)) {
        console.warn(`🗑️ papelera: ignorada (${de}) en ${jid} — mensaje de ${target.participant || jid} (fromMe=${target.fromMe}) id=${target.id}`);
        return;
    }

    const delKey = { remoteJid: jid, id: target.id, fromMe: true };
    // En grupos el participant debe ser el JID con el que el bot firmó el mensaje
    // (puede ser un @lid distinto al número del bot): usar tal cual llegó.
    if (jid.endsWith('@g.us')) {
        delKey.participant = target.participant || jidNormalizedUser(sock.user.id);
    }

    console.warn(`🗑️ papelera: borrando ${target.id} en ${jid} (${de})`);
    try {
        await sock.sendMessage(jid, { delete: delKey });
    } catch (e) {
        console.error(`🗑️ papelera: no se pudo borrar ${target.id} en ${jid}: ${e.message}`);
    }
}

module.exports = {
    init: (sock) => {
        sock.ev.on('messages.upsert', async (m) => {
            if (m.type !== 'notify') return;
            for (const msg of m.messages) {
                const reaction = msg.message?.reactionMessage;
                if (!reaction) continue;
                try {
                    await manejarPapelera(sock, reaction.key, reaction.text, 'upsert');
                } catch (e) {
                    console.error('🗑️ papelera:', e.message);
                }
            }
        });

        sock.ev.on('messages.update', async (updates) => {
            for (const u of updates) {
                const reaction = u?.update?.reactionMessage;
                if (!reaction) continue;
                try {
                    // En messages.update la key EXTERNA es la del mensaje reaccionado;
                    // reactionMessage.key también suele venir: puede llevar más contexto.
                    const target = reaction.key?.id ? reaction.key : u.key;
                    await manejarPapelera(sock, target, reaction.text || '', 'update');
                } catch (e) {
                    console.error('🗑️ papelera:', e.message);
                }
            }
        });
    }
};
