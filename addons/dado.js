// addons/dado.js
// .dado [N] → tira un dado de N caras (6 por defecto)
// .moneda   → cara o cruz

module.exports = {
    commands: ['dado', 'moneda'],

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;
        try {
            const rawText = msg.message?.conversation || msg.message?.extendedTextMessage?.text || '';
            const commandUsed = rawText.trim().split(/\s+/)[0].toLowerCase().replace(/^[.!/,]/, '');

            if (commandUsed === 'moneda') {
                const esCara = Math.random() < 0.5;
                await sock.sendMessage(jid, { text: `🪙 ${esCara ? '*CARA*' : '*CRUZ*'}` }, { quoted: msg });
                return;
            }

            let caras = 6;
            if (args[0]) {
                const n = parseInt(args[0], 10);
                if (isNaN(n) || n < 2 || n > 1000) {
                    await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
                    await sock.sendMessage(jid, { text: '🎲 Indica caras entre 2 y 1000. Ej: `.dado 20`' }, { quoted: msg });
                    return;
                }
                caras = n;
            }

            const resultado = 1 + Math.floor(Math.random() * caras);
            await sock.sendMessage(jid, { text: `🎲 ${resultado} (d${caras})` }, { quoted: msg });

        } catch (error) {
            console.error('❌ [dado]:', error.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } }).catch(() => {});
        }
    }
};
