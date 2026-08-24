// addons/acortar.js
// .acortar <url> → acorta con is.gd (toma texto citado si no hay argumentos)

module.exports = {
    commands: ['acortar', 'short'],

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;
        try {
            let url = args.join(' ').trim();
            if (!url) {
                url = (msg.message?.extendedTextMessage?.contextInfo?.quotedMessage?.conversation || '').trim();
            }
            url = url.split(/\s+/)[0];

            if (!url) {
                await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
                await sock.sendMessage(jid, { text: '🔗 *Acortador*\n\nUso: `.acortar https://enlace-largo.com`' }, { quoted: msg });
                return;
            }
            if (!/^https?:\/\//i.test(url)) url = 'https://' + url;
            try { new URL(url); } catch (_) {
                await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                await sock.sendMessage(jid, { text: 'Ese enlace no parece válido.' }, { quoted: msg });
                return;
            }

            await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

            const api = `https://is.gd/create.php?format=json&url=${encodeURIComponent(url)}`;
            const res = await fetch(api, {
                headers: { 'User-Agent': 'Mozilla/5.0' },
                signal: AbortSignal.timeout(15000)
            });
            const json = await res.json();

            if (!json.shorturl) throw new Error(json.errormessage || 'is.gd rechazó el enlace');

            await sock.sendMessage(jid, { text: `🔗 ${json.shorturl}` }, { quoted: msg });
            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

        } catch (error) {
            console.error('❌ [acortar]:', error.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } }).catch(() => {});
        }
    }
};
