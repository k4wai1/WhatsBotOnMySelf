// addons/sysinfo.js
// Estado y estadísticas del bot: Node, plataforma, memoria y uptime.
// Portado del bot de rupa (info.js) y adaptado al router de addons.
// Nota: el comando .info ya está ocupado por debug-info.js (extracción de datos),
// por eso este addon responde a sysinfo / sistema.
const v8 = require('v8');

module.exports = {
    commands: ['sysinfo', 'sistema'],

    handler: async (sock, msg) => {
        const jid = msg.key.remoteJid;

        try {
            const mem = process.memoryUsage();

            // Nota: v8 se mantiene por compatibilidad con el addon original
            void v8;

            const info = `🤖 *Bot Status*\n\n` +
                `⚙️ *Sistema*\n` +
                `• Node: ${process.version}\n` +
                `• Plataforma: ${process.platform}\n\n` +
                `💾 *Memoria*\n` +
                `• RSS: ${(mem.rss / 1024 / 1024).toFixed(2)} MB\n` +
                `• Heap: ${(mem.heapUsed / 1024 / 1024).toFixed(2)} MB\n\n` +
                `🕒 *Uptime*: ${(process.uptime() / 3600).toFixed(2)} horas`;

            await sock.sendMessage(jid, { text: info }, { quoted: msg });

        } catch (err) {
            console.error('❌ [sysinfo] Error:', err.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
        }
    }
};
