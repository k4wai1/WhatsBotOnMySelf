// addons/server.js
// Estado de servidores Minecraft (con resolución SRV para Aternos).
// Portado del bot de rupa y adaptado al router de addons.
// Servidor por defecto configurable: MC_DEFAULT_SERVER en .env
const mcUtil = require('minecraft-server-util');
const dns = require('dns').promises;

const DEFAULT_SERVER = process.env.MC_DEFAULT_SERVER || 'FocaTostada.aternos.me';

module.exports = {
    commands: ['server'],

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;

        // Si el usuario pasa puerto manual (.server ip:puerto), lo separamos
        let targetIp = args[0] || DEFAULT_SERVER;
        let targetPort = 25565;

        if (targetIp.includes(':')) {
            const parts = targetIp.split(':');
            targetIp = parts[0];
            targetPort = parseInt(parts[1]);
        }

        try {
            await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

            let finalHost = targetIp;
            let finalPort = targetPort;

            // EL TRUCO: si es Aternos y no nos dieron puerto, buscamos la IP trasera real
            if (targetPort === 25565 && targetIp.toLowerCase().includes('aternos.me')) {
                try {
                    const records = await dns.resolveSrv(`_minecraft._tcp.${targetIp}`);
                    if (records.length > 0) {
                        finalHost = records[0].name;  // Ej: node1.aternos.host
                        finalPort = records[0].port;  // Ej: 40941
                        console.log(`[SERVER] Bypass exitoso: Redirigiendo a nodo trasero -> ${finalHost}:${finalPort}`);
                    }
                } catch (e) {
                    console.log('[SERVER] No se pudo resolver SRV manual:', e.message);
                }
            }

            // Ping directo a la máquina real, con el SRV desactivado porque ya lo resolvimos
            const result = await mcUtil.status(finalHost, finalPort, {
                timeout: 5000,
                enableSRV: false,
                protocolVersion: 767 // Simulamos ser un cliente 1.21.1
            });

            let output = `🟢 *Servidor Activo* 🟢\n\n`;
            output += `*IP:* ${targetIp}\n`;
            output += `*Puerto Real:* ${finalPort}\n`;
            output += `*Versión:* ${result.version.name}\n`;
            output += `*Jugadores:* ${result.players.online} / ${result.players.max}\n`;

            if (result.players.sample && result.players.sample.length > 0) {
                output += `\n*Jugadores conectados:*\n`;
                result.players.sample.forEach(player => {
                    const cleanName = player.name.replace(/§[0-9a-fk-or]/ig, '');
                    output += `🎮 ${cleanName}\n`;
                });
            }

            await sock.sendMessage(jid, { text: output }, { quoted: msg });
            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

        } catch (error) {
            console.error('[SERVER] Fallo definitivo:', error.message);

            await sock.sendMessage(jid, {
                text: `🔴 El servidor *${targetIp}* está apagado o es inaccesible.\n\n_Aternos bloquea activamente las consultas desde la IP del VPS._`
            }, { quoted: msg });

            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
        }
    }
};
