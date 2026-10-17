// addons/alarm.js
// Alarmas a una hora fija del día (se rearma sola si la hora ya pasó).
// Portado del bot de rupa (alarmas.js) y adaptado al router de addons.
// Datos: assets/recordatorios.json (distinto de rem.js, que usa reminders.json)
const fs = require('fs').promises;
const fsSync = require('fs');
const path = require('path');

const DB_PATH = path.join(__dirname, '..', 'assets', 'recordatorios.json');

async function initDB() {
    try {
        await fs.access(DB_PATH);
    } catch (error) {
        await fs.writeFile(DB_PATH, JSON.stringify([]));
    }
}

module.exports = {
    commands: ['a', 'alarma'],

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;
        const senderId = msg.key.participant || msg.key.remoteJid;
        const isGroup = jid.endsWith('@g.us');

        try {
            await initDB();

            // El router ya quitó el comando, así que aquí solo vienen los argumentos
            const rawArgs = args.join(' ').trim();

            // Captura: 1.Horas | 2.Minutos (opcional) | 3.Descripción (opcional)
            const regex = /^(\d{1,2})(?:[:\s](\d{1,2}))?(?:\s+(.*))?$/i;
            const match = rawArgs.match(regex);

            if (!match) {
                await sock.sendMessage(jid, { text: '⚠️ Formato: `.a 12`, `.a 13 45` o `.a 17:36 descripción`' }, { quoted: msg });
                return;
            }

            const hours = parseInt(match[1], 10);
            const minutes = parseInt(match[2] || '0', 10);
            const description = match[3] || '¡Es hora de tu recordatorio!'; // Texto personalizado

            if (hours < 0 || hours > 23 || minutes < 0 || minutes > 59) {
                await sock.sendMessage(jid, { text: '❌ Hora o minutos fuera de rango (0-23h / 0-59m).' }, { quoted: msg });
                return;
            }

            const now = new Date();
            const targetDate = new Date(now.getFullYear(), now.getMonth(), now.getDate(), hours, minutes, 0, 0);

            if (targetDate.getTime() <= now.getTime()) {
                targetDate.setDate(targetDate.getDate() + 1);
            }

            const newReminder = {
                id: Date.now().toString(),
                chatId: jid,
                senderId: senderId,
                isGroup: isGroup,
                targetTime: targetDate.getTime(),
                message: description
            };

            try {
                const data = JSON.parse(await fs.readFile(DB_PATH, 'utf-8'));
                data.push(newReminder);
                await fs.writeFile(DB_PATH, JSON.stringify(data, null, 2));

                const hh = hours.toString().padStart(2, '0');
                const mm = minutes.toString().padStart(2, '0');

                await sock.sendMessage(jid, {
                    text: `✅ Alarma establecida: *${hh}:${mm}*\n📝 Nota: ${description}`
                }, { quoted: msg });

            } catch (error) {
                await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
            }

        } catch (err) {
            console.error('❌ [alarm] Error:', err.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
        }
    },

    // Verificador cada 5 minutos
    init: (sock) => {
        if (!fsSync.existsSync(DB_PATH)) {
            fsSync.writeFileSync(DB_PATH, JSON.stringify([]));
        }

        setInterval(async () => {
            try {
                const dataText = await fs.readFile(DB_PATH, 'utf-8').catch(() => '[]');
                let reminders = JSON.parse(dataText);
                if (reminders.length === 0) return;

                const now = Date.now();
                const toTrigger = reminders.filter(r => now >= r.targetTime);
                const toKeep = reminders.filter(r => now < r.targetTime);

                if (toTrigger.length > 0) {
                    await fs.writeFile(DB_PATH, JSON.stringify(toKeep, null, 2));

                    for (const r of toTrigger) {
                        let mentionText = `⏰ *RECORDATORIO*\n\n> ${r.message}`;
                        let mentionConfig = {};

                        if (r.isGroup) {
                            mentionText += `\n\n🔔 @${r.senderId.split('@')[0]}`;
                            mentionConfig = { mentions: [r.senderId] };
                        }

                        await sock.sendMessage(r.chatId, { text: mentionText, ...mentionConfig });
                    }
                }
            } catch (e) {
                console.error("❌ [alarm] Error en cron:", e.message);
            }
        }, 5 * 60 * 1000);
    }
};
