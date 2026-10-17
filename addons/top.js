// addons/top.js
// Estadísticas de actividad del grupo: .ongrup activa la recolección y .top muestra rankings.
// Portado del bot de rupa y adaptado al router de addons.
const fs = require('fs');
const path = require('path');

// --- CONFIGURACIÓN ---
const STATS_DIR = path.join(__dirname, '..', 'assets', 'statistics');
const ACTIVE_GROUPS_FILE = path.join(STATS_DIR, 'active_groups.json');
const HEARTBEAT_INTERVAL_MINUTES = 5;
const MAX_CHAR_LIMIT = 4000;
const spamTracker = {};

// Asegurar directorios
if (!fs.existsSync(STATS_DIR)) fs.mkdirSync(STATS_DIR, { recursive: true });
if (!fs.existsSync(ACTIVE_GROUPS_FILE)) fs.writeFileSync(ACTIVE_GROUPS_FILE, JSON.stringify([]));

// Helpers
const getActiveGroups = () => {
    try { return JSON.parse(fs.readFileSync(ACTIVE_GROUPS_FILE)); }
    catch { return []; }
};
const saveActiveGroups = (groups) => fs.writeFileSync(ACTIVE_GROUPS_FILE, JSON.stringify(groups));

const getStatsFilePath = (chatId, monthStr = null) => {
    if (!monthStr) {
        const date = new Date();
        monthStr = `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}`;
    }
    return path.join(STATS_DIR, `${chatId}_${monthStr}.json`);
};

const loadStats = (chatId, monthStr = null) => {
    const filePath = getStatsFilePath(chatId, monthStr);
    if (!fs.existsSync(filePath)) return null;
    return JSON.parse(fs.readFileSync(filePath));
};

// --- FUNCIÓN UPDATE STATS ---
const updateStats = (chatId, userId, type, userName, amount = 1) => {
    const activeGroups = getActiveGroups();
    if (!activeGroups.includes(chatId)) return;

    const filePath = getStatsFilePath(chatId);
    let data = { uptime_minutes: 0, users: {} };

    if (fs.existsSync(filePath)) {
        data = JSON.parse(fs.readFileSync(filePath));
    }

    if (!data.users) data.users = {};

    if (!data.users[userId]) {
        data.users[userId] = {
            name: userName || 'Usuario',
            msg: 0, sticker: 0, image: 0, video: 0, audio: 0, reaction: 0, char: 0
        };
    }

    const isGenericName = (name) => !name || name === 'Usuario' || name === 'Anónimo';
    if (!isGenericName(userName)) {
        data.users[userId].name = userName;
    }

    if (data.users[userId][type] === undefined) {
        data.users[userId][type] = 0;
    }

    data.users[userId][type] += amount;
    fs.writeFileSync(filePath, JSON.stringify(data, null, 2));
};

const startUptimeHeartbeat = () => {
    setInterval(() => {
        const activeGroups = getActiveGroups();
        if (activeGroups.length === 0) return;

        activeGroups.forEach(chatId => {
            const filePath = getStatsFilePath(chatId);
            let data = { uptime_minutes: 0, users: {} };

            if (fs.existsSync(filePath)) {
                try { data = JSON.parse(fs.readFileSync(filePath)); } catch (e) { return; }
            }
            if (!data.uptime_minutes) data.uptime_minutes = 0;
            data.uptime_minutes += HEARTBEAT_INTERVAL_MINUTES;

            fs.writeFileSync(filePath, JSON.stringify(data, null, 2));
        });
    }, HEARTBEAT_INTERVAL_MINUTES * 60 * 1000);
};

module.exports = {
    commands: ['ongrup', 'top'],

    // Recolector de estadísticas en segundo plano + heartbeat de uptime
    init: (sock) => {
        startUptimeHeartbeat();

        // Prefixes configurados (para no contar los propios comandos como mensajes)
        let prefixes = [',', '!', '/'];
        try {
            const cfg = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'assets', 'config.json'), 'utf-8'));
            if (Array.isArray(cfg.prefixes) && cfg.prefixes.length) prefixes = cfg.prefixes;
        } catch (_) {}

        const isStatsCommand = (text) => {
            const t = (text || '').trim().toLowerCase();
            for (const p of prefixes) {
                if (t.startsWith(p.toLowerCase())) {
                    const first = t.slice(p.length).trim().split(/\s+/)[0];
                    if (first === 'ongrup' || first === 'top') return true;
                }
            }
            return false;
        };

        // Evento único para mensajes y reacciones
        sock.ev.on('messages.upsert', async (m) => {
            if (m.type !== 'notify') return;
            const msg = m.messages[0];

            if (!msg.message) return;

            const chatId = msg.key.remoteJid;
            if (!chatId.endsWith('@g.us')) return; // Actuar solo en grupos

            // Identificar al usuario. Si es fromMe, forzar el nombre del bot.
            const isFromMe = msg.key.fromMe;
            const userId = isFromMe ? sock.user.id.split(':')[0] + '@s.whatsapp.net' : (msg.key.participant || chatId);

            // --- 1. DETECCIÓN DE REACCIONES ---
            if (msg.message.reactionMessage) {
                const userName = isFromMe ? 'Bot' : (msg.pushName || 'Usuario');
                updateStats(chatId, userId, 'reaction', userName, 1);
                return;
            }

            // --- EXTRACCIÓN DE TEXTO Y TIPO ---
            const textMsg = msg.message.conversation || msg.message.extendedTextMessage?.text || '';
            const userName = isFromMe ? 'Bot' : (msg.pushName || 'Usuario');

            // No contar los comandos de estadísticas
            if (isStatsCommand(textMsg)) return;

            let type = 'msg';
            if (msg.message.stickerMessage) type = 'sticker';
            else if (msg.message.imageMessage) type = 'image';
            else if (msg.message.videoMessage) type = 'video';
            else if (msg.message.audioMessage) type = 'audio';

            // --- 2. CONTEO DE ESTADÍSTICAS NORMALES ---
            if (textMsg.length > MAX_CHAR_LIMIT) return;

            if (!spamTracker[chatId]) {
                spamTracker[chatId] = { lastUser: null, count: 0 };
            }

            const tracker = spamTracker[chatId];

            if (tracker.lastUser === userId) {
                tracker.count += 1;
                if (tracker.count > 5) return;
            } else {
                tracker.lastUser = userId;
                tracker.count = 1;
            }

            updateStats(chatId, userId, type, userName, 1);

            if (type === 'msg' && textMsg.length > 0) {
                updateStats(chatId, userId, 'char', userName, textMsg.length);
            }
        });
    },

    handler: async (sock, msg, args) => {
        const chatId = msg.key.remoteJid;

        try {
            const rawText = msg.message?.conversation || msg.message?.extendedTextMessage?.text || '';
            const commandUsed = rawText.trim().split(/\s+/)[0].toLowerCase().replace(/^[.!/,]/, '');

            // 📊 Activar estadísticas en el grupo
            if (commandUsed === 'ongrup') {
                const activeGroups = getActiveGroups();
                if (!activeGroups.includes(chatId)) {
                    activeGroups.push(chatId);
                    saveActiveGroups(activeGroups);
                    await sock.sendMessage(chatId, { text: '✅ *Estadísticas activadas.*' }, { quoted: msg });
                } else {
                    await sock.sendMessage(chatId, { text: 'ℹ️ Las estadísticas ya están activas.' }, { quoted: msg });
                }
                return;
            }

            // 🏆 Ranking
            let targetMonth = null;
            let category = 'msg';

            const categoriesMap = {
                'mensajes': 'msg', 'msg': 'msg',
                'stickers': 'sticker', 'sticker': 'sticker',
                'imagenes': 'image', 'imagen': 'image', 'fotos': 'image',
                'videos': 'video', 'video': 'video',
                'reacciones': 'reaction', 'reaccion': 'reaction',
                'caracteres': 'char', 'letras': 'char', 'char': 'char', 'texto': 'char',
                'audios': 'audio', 'audio': 'audio', 'notas': 'audio', 'vn': 'audio'
            };

            args.forEach(arg => {
                const a = arg.toLowerCase();
                if (a.match(/^\d{4}-\d{2}$/)) targetMonth = a;
                else if (categoriesMap[a]) category = categoriesMap[a];
            });

            const stats = loadStats(chatId, targetMonth);

            if (!stats) {
                await sock.sendMessage(chatId, { text: `📂 No hay datos para ${targetMonth || 'este mes'}.` }, { quoted: msg });
                return;
            }

            let uptimeMsg = '';
            if (stats.uptime_minutes) {
                const now = new Date();
                let totalPossibleMinutes;
                if (targetMonth) {
                    const [y, m] = targetMonth.split('-');
                    const daysInMonth = new Date(y, m, 0).getDate();
                    totalPossibleMinutes = daysInMonth * 24 * 60;
                } else {
                    const startOfMonth = new Date(now.getFullYear(), now.getMonth(), 1);
                    totalPossibleMinutes = Math.floor((now - startOfMonth) / 1000 / 60);
                }

                // Evitar división por cero
                if (totalPossibleMinutes < 1) totalPossibleMinutes = 1;

                // Límite estricto del porcentaje
                let uptimePercent = (stats.uptime_minutes / totalPossibleMinutes) * 100;
                if (uptimePercent > 100) uptimePercent = 100;
                uptimePercent = uptimePercent.toFixed(2);

                let filled = Math.round(uptimePercent / 10);
                if (filled > 10) filled = 10;
                if (filled < 0) filled = 0;

                const bar = '🟩'.repeat(filled) + '⬛'.repeat(10 - filled);
                uptimeMsg = `\n⚡ *Uptime Bot:* ${uptimePercent}%\n${bar} (${stats.uptime_minutes} min activos)\n`;
            }

            let limit = 10;
            try {
                const groupMetadata = await sock.groupMetadata(chatId);
                if (groupMetadata.participants && groupMetadata.participants.length > 0) {
                    limit = Math.ceil(groupMetadata.participants.length / 2);
                }
            } catch (e) {
                // Falla silenciosa si no puede obtener metadatos
            }

            const sortedUsers = Object.entries(stats.users || {})
                .map(([id, data]) => ({ id, ...data }))
                .sort((a, b) => (b[category] || 0) - (a[category] || 0))
                .slice(0, limit);

            const titulos = {
                'msg': '💬 Top Mensajes',
                'sticker': '👾 Top Stickers',
                'image': '📸 Top Imágenes',
                'video': '🎥 Top Videos',
                'audio': '🎤 Top Audios',
                'reaction': '❤️ Top Reacciones',
                'char': '📝 Top Caracteres'
            };

            let respuesta = `📊 *${titulos[category] || 'Top'}*\n👥 Mostrando top ${limit} (50% del grupo)\n📅 Fecha: ${targetMonth || 'Mes Actual'}${uptimeMsg}\n\n`;

            let pos = 1;
            for (const user of sortedUsers) {
                if (user[category] > 0) {
                    const medal = pos === 1 ? '🥇' : pos === 2 ? '🥈' : pos === 3 ? '🥉' : `${pos}.`;
                    const valorFormateado = user[category].toLocaleString();
                    respuesta += `${medal} ${user.name}: ${valorFormateado}\n`;
                    pos++;
                }
            }

            await sock.sendMessage(chatId, { text: respuesta }, { quoted: msg });

        } catch (err) {
            console.error('❌ [top] Error:', err.message);
            await sock.sendMessage(chatId, { react: { text: '❌', key: msg.key } });
        }
    }
};
