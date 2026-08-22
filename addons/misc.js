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
    // Consulta de registro contra los servidores de WA (round-trip real).
    // Nota: el iq 'w:pinger' clásico ya no recibe respuesta, por eso no se usa.
    const meId = sock.authState?.creds?.me?.id?.split(':')[0] || '1';
    const jidNum = meId.includes('@') ? meId : meId + '@s.whatsapp.net';
    const start = Date.now();
    await sock.onWhatsApp(jidNum);
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
                const ms = await pingWhatsAppServers(sock);
                await sock.sendMessage(jid, {
                    text: [
                        '🚀 *Ping a WhatsApp*',
                        '',
                        `⚡ Latencia: *${ms}ms*`
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
