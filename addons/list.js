// addons/list.js
// Lista todos los grupos donde está el bot (nombre, ID y participantes).
// Portado del bot de rupa y adaptado al router de addons + reacciones.
const CONFIG = { MAX_GROUPS_PER_MESSAGE: 50 };

module.exports = {
    commands: ['list'],

    handler: async (sock, msg) => {
        const jid = msg.key.remoteJid;

        try {
            await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

            // En Baileys obtenemos todos los metadatos de los grupos actuales
            const allGroups = await sock.groupFetchAllParticipating();
            const groups = Object.values(allGroups);

            if (groups.length === 0) {
                return sock.sendMessage(jid, { text: '📭 No estás en ningún grupo actualmente.' }, { quoted: msg });
            }

            // Dividir en grupos de 50
            for (let i = 0; i < groups.length; i += CONFIG.MAX_GROUPS_PER_MESSAGE) {
                const chunk = groups.slice(i, i + CONFIG.MAX_GROUPS_PER_MESSAGE);
                let response = `📋 *Lista de Grupos* (${Math.floor(i / CONFIG.MAX_GROUPS_PER_MESSAGE) + 1})\n\n`;

                chunk.forEach((group, idx) => {
                    response += `${i + idx + 1}. *${group.subject}*\n   ID: \`${group.id}\`\n   👥 Participantes: ${group.participants.length}\n\n`;
                });

                response += `💡 *Tip:* Copia el ID para usar en otros comandos`;
                await sock.sendMessage(jid, { text: response });
                if (i + CONFIG.MAX_GROUPS_PER_MESSAGE < groups.length) {
                    await new Promise(r => setTimeout(r, 1000));
                }
            }

        } catch (error) {
            console.error('❌ [list] Error:', error.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
        }
    }
};
