// addons/speed.js
// Test de velocidad de internet con animación (speedtest-net). Solo dueños.
// Portado del bot de rupa y adaptado al router de addons.
const speedTestNet = require('speedtest-net');
const { jidNormalizedUser } = require('@whiskeysockets/baileys');

const SPINNER = ['⣾', '⣽', '⣻', '⢿', '⡿', '⣟', '⣯', '⣷'];

// 👑 Números con privilegios de dueño: OWNER_NUMBERS=584...,182... en .env (opcional)
const OWNER_NUMBERS = (process.env.OWNER_NUMBERS || '')
    .split(',')
    .map(n => n.replace(/\D/g, ''))
    .filter(Boolean);

module.exports = {
    commands: ['speed'],

    handler: async (sock, msg) => {
        const jid = msg.key.remoteJid;

        try {
            const senderJid = jidNormalizedUser(msg.key.participant || jid);
            if (!OWNER_NUMBERS.includes(senderJid.split('@')[0])) {
                await sock.sendMessage(jid, { react: { text: '⛔', key: msg.key } });
                return;
            }

            const sent = await sock.sendMessage(jid, { text: '🚀 Iniciando Speedtest...' }, { quoted: msg });
            let frame = 0;
            const anim = setInterval(() => {
                sock.sendMessage(jid, { text: `${SPINNER[frame % SPINNER.length]} *Calculando velocidad...*`, edit: sent.key }).catch(() => {});
                frame++;
            }, 700);

            try {
                const data = await speedTestNet({ acceptLicense: true, acceptGdpr: true });
                clearInterval(anim);

                const result = `📊 *Resultado Speedtest*\n\n` +
                    `⬇️ Descarga: *${(data.download.bandwidth / 125000).toFixed(2)} Mbps*\n` +
                    `⬆️ Subida: *${(data.upload.bandwidth / 125000).toFixed(2)} Mbps*\n` +
                    `⏱️ Latencia: *${data.ping.latency.toFixed(0)} ms*`;

                await sock.sendMessage(jid, { text: result, edit: sent.key });
            } catch (e) {
                clearInterval(anim);
                console.error('❌ [speed] Error del test:', e.message);
                await sock.sendMessage(jid, { text: '❌ Error al ejecutar el test.', edit: sent.key });
            }

        } catch (err) {
            console.error('❌ [speed] Error:', err.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
        }
    }
};
