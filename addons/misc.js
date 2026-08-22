// addons/misc.js
// Comandos misceláneos: .ping (latencia con servidores de WhatsApp)
//                      .pong (tiempo entre trigger del usuario y envío, luego se edita)

function detectCommand(msg) {
    let prefixes = [',', '!', '/'];
    try {
        const cfg = JSON.parse(require('fs').readFileSync(require('path').join(__dirname, '..', 'assets', 'config.json'), 'utf-8'));
        if (Array.isArray(cfg.prefixes) && cfg.prefixes.length) prefixes = cfg.prefixes;
    } catch (_) {}
    const text = msg.message?.conversation || msg.message?.extendedTextMessage?.text || '';
    const trimmed = text.trim();
    for (const p of prefixes) {
        if (trimmed.startsWith(p)) {
            return trimmed.slice(p.length).trim().split(/\s+/)[0]?.toLowerCase();
        }
    }
    return null;
}

async function pingWhatsAppServers(sock) {
    const start = Date.now();
    await sock.query({
        tag: 'iq',
        attrs: { to: '@s.whatsapp.net', type: 'get', xmlns: 'w:pinger' }
    });
    return Date.now() - start;
}

module.exports = {
    commands: ['ping', 'pong'],

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;
        const cmd = detectCommand(msg);

        try {
            if (cmd === 'ping') {
                await sock.sendMessage(jid, { react: { text: '📡', key: msg.key } });
                const latency = await pingWhatsAppServers(sock);
                await sock.sendMessage(jid, {
                    text: [
                        '🚀 *Ping a WhatsApp*',
                        '',
                        `⚡ Latencia: *${latency}ms*`
                    ].join('\n')
                }, { quoted: msg });
                await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
                return;
            }

            if (cmd === 'pong') {
                const start = Date.now();
                const sent = await sock.sendMessage(jid, { text: '🏓' }, { quoted: msg });
                const elapsed = Date.now() - start;
                await sock.sendMessage(jid, { text: `🏓 pong: ${elapsed}ms`, edit: sent.key });
                return;
            }

            await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
        } catch (error) {
            console.error('Error en misc:', error);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
        }
    }
};
