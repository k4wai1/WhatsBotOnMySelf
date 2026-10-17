// addons/id.js
// Muestra el ID (JID) del chat actual.
// Portado del bot de rupa y adaptado al router de addons.
module.exports = {
    commands: ['id'],

    handler: async (sock, msg) => {
        try {
            const jid = msg.key.remoteJid;
            const info = `📝 *Info del Chat*\n\n🆔 *ID:* \`${jid}\``;
            await sock.sendMessage(jid, { text: info }, { quoted: msg });
        } catch (err) {
            console.error('❌ [id] Error:', err.message);
            await sock.sendMessage(msg.key.remoteJid, { react: { text: '❌', key: msg.key } });
        }
    }
};
