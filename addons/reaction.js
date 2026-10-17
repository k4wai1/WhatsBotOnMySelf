// addons/reaction.js
// Sesión de reacciones de 60 segundos con resultado en vivo.
// Portado del bot de rupa y adaptado al router de addons.
const trackedMessages = new Map(); // msgId -> { reactions: { user: emoji }, timer, jid, key }

module.exports = {
    commands: ['reaction'],

    handler: async (sock, msg) => {
        const jid = msg.key.remoteJid;

        try {
            const sent = await sock.sendMessage(jid, { text: '🪄 Reacciona a este mensaje 👇' });

            const msgId = sent.key.id;
            const timeout = setTimeout(async () => {
                const data = trackedMessages.get(msgId);
                if (data) {
                    const lines = Object.entries(data.reactions).map(([u, e]) => `${e} — ${u.split('@')[0]}`);
                    const finalText = lines.length === 0 ? '😐 Nadie reaccionó...' : `✅ Reacción finalizada:\n${lines.join('\n')}`;
                    try {
                        await sock.sendMessage(jid, { text: finalText, edit: sent.key });
                    } catch (e) { console.error('❌ [reaction] Error al editar:', e.message); }
                    trackedMessages.delete(msgId);
                }
            }, 60000);

            trackedMessages.set(msgId, { reactions: {}, timer: timeout, jid, key: sent.key });

        } catch (err) {
            console.error('❌ [reaction] Error:', err.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
        }
    },

    // Listener nativo de Baileys para reacciones
    init: (sock) => {
        sock.ev.on('messages.reaction', async (reactions) => {
            for (const reaction of reactions) {
                const targetId = reaction.key.id;
                if (trackedMessages.has(targetId)) {
                    const data = trackedMessages.get(targetId);
                    const user = reaction.key.participant || reaction.key.remoteJid;

                    // Actualizar mapa de reacciones
                    if (reaction.reaction.text) {
                        data.reactions[user] = reaction.reaction.text;
                    } else {
                        delete data.reactions[user]; // Si quitan la reacción
                    }

                    const lines = Object.entries(data.reactions).map(([u, e]) => `${e} — ${u.split('@')[0]}`);
                    const updateText = lines.length === 0 ? '🪄 Reacciona a este mensaje 👇' : `💫 Reacciones actuales:\n${lines.join('\n')}`;

                    try {
                        await sock.sendMessage(data.jid, { text: updateText, edit: data.key });
                    } catch (e) { console.error('❌ [reaction] Error al actualizar:', e.message); }
                }
            }
        });
    }
};
