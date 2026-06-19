// addons/emoji-to-gif
const fs = require('fs');
const path = require('path');
const axios = require('axios');

const STATS_DIR = path.join(__dirname, '..', 'statistics');
const CONFIG_FILE = path.join(STATS_DIR, 'reacciones_config.json');
const STATS_FILE = path.join(STATS_DIR, 'reacciones_stats.json');

// Inicialización de archivos
if (!fs.existsSync(STATS_DIR)) fs.mkdirSync(STATS_DIR, { recursive: true });
if (!fs.existsSync(CONFIG_FILE)) fs.writeFileSync(CONFIG_FILE, JSON.stringify({ active_groups: [] }));
if (!fs.existsSync(STATS_FILE)) fs.writeFileSync(STATS_FILE, JSON.stringify({}));

// Emoticones detonadores
const HEART_EMOJIS = ['❤️', '💖', '💕', '💓', '💗', '💘', '💝'];

const saveConfig = (config) => fs.writeFileSync(CONFIG_FILE, JSON.stringify(config, null, 2));
const saveStats = (stats) => fs.writeFileSync(STATS_FILE, JSON.stringify(stats, null, 2));

async function getRoleplayGif(searchTerm) {
    try {
        const url = `https://tenor.googleapis.com/v2/search?q=${encodeURIComponent(searchTerm)}&key=${process.env.TENOR_API_KEY}&limit=20&contentfilter=low`;
        const response = await axios.get(url);
        const results = response.data.results;

        if (!results || results.length === 0) return null;
        
        const randomIndex = Math.floor(Math.random() * results.length);
        const selectedGif = results[randomIndex];
        const formats = selectedGif.media_formats;
        
        return (formats.tinymp4 || formats.mp4 || formats.gif)?.url || null; 
    } catch (error) {
        console.error('❌ Error Tenor API:', error.message);
        return null;
    }
}

module.exports = {
    // Comandos de administración (Enrutados por index.js)
    commands: ['onreaccion', 'offreaccion'],
    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;
        const text = msg.message.conversation || msg.message.extendedTextMessage?.text || '';
        const command = text.trim().split(/\s+/)[0].toLowerCase().replace(/^[.!/]/, '');

        let config = JSON.parse(fs.readFileSync(CONFIG_FILE));

        if (command === 'onreaccion') {
            if (!config.active_groups.includes(jid)) {
                config.active_groups.push(jid);
                saveConfig(config);
                await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
            } else {
                await sock.sendMessage(jid, { react: { text: '👍', key: msg.key } });
            }
        } else if (command === 'offreaccion') {
            const idx = config.active_groups.indexOf(jid);
            if (idx > -1) {
                config.active_groups.splice(idx, 1);
                saveConfig(config);
                await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
            } else {
                await sock.sendMessage(jid, { react: { text: '👍', key: msg.key } });
            }
        }
    },

    // Listener en segundo plano (Iniciado por el ajuste en index.js)
    init: (sock) => {
        sock.ev.on('messages.upsert', async (m) => {
            if (m.type !== 'notify') return;
            const msg = m.messages[0];

            // 1. Verificar si el evento es una reacción
            const reaction = msg.message?.reactionMessage;
            if (!reaction) return;

            const emoji = reaction.text;
            
            // Si la reacción está vacía, significa que el usuario quitó el emoji. Lo ignoramos.
            if (!emoji) return; 
            
            const jid = msg.key.remoteJid;
            const senderId = msg.key.participant || jid; // Quien reaccionó
            const targetId = reaction.key.participant || jid; // Dueño del mensaje original
            
            // 2. Verificar autorización: Solo funciona si el grupo está activo O si la reacción la hiciste tú
            let config = JSON.parse(fs.readFileSync(CONFIG_FILE));
            const isAuthorizedGroup = config.active_groups.includes(jid);
            const isBotOwner = msg.key.fromMe; // Si tú mismo reaccionas, fromMe es true

            if (!isAuthorizedGroup && !isBotOwner) return;

            // 3. Procesar si es un corazón
            if (HEART_EMOJIS.includes(emoji)) {
                
                // Evitar auto-respuestas si te das corazón a ti mismo
                if (senderId === targetId) return;

                // Actualizar estadísticas silenciosamente
                let stats = JSON.parse(fs.readFileSync(STATS_FILE));
                if (!stats[jid]) stats[jid] = {};
                if (!stats[jid][senderId]) stats[jid][senderId] = {};
                if (!stats[jid][senderId][targetId]) stats[jid][senderId][targetId] = 0;
                
                stats[jid][senderId][targetId]++;
                const totalHearts = stats[jid][senderId][targetId];
                saveStats(stats);

                // Obtener nombres para el Roleplay
                const senderName = msg.pushName || 'Alguien';
                
                let targetName = 'alguien';
                // Intentar sacar el nombre del objetivo de la caché de Baileys
                if (sock.store?.contacts?.[targetId]) {
                    targetName = sock.store.contacts[targetId].name || sock.store.contacts[targetId].notify || targetName;
                } else if (reaction.key.fromMe) {
                    targetName = 'ti'; // Si alguien le da corazón a un mensaje del bot/tuyo
                }

                try {
                    // Generar GIF (Busca animaciones de afecto estilo gacha/anime)
                    const gifUrl = await getRoleplayGif('genshin wuthering anime cute hug love');
                    
                    if (gifUrl) {
                        const caption = `💖 *${senderName}* se sintió lleno de afecto y le mandó un corazón a *${targetName}*.\n\n✨ (Le ha enviado amor ${totalHearts} veces)`;
                        
                        // Enviar el GIF de Roleplay respondiendo al mensaje original que recibió la reacción
                        await sock.sendMessage(jid, { 
                            video: { url: gifUrl }, 
                            gifPlayback: true, 
                            caption: caption 
                        }, { quoted: { key: reaction.key, message: {} } });
                    }
                } catch (error) {
                    console.error('Error generando GIF de reacción:', error.message);
                }
            }
        });
    }
};
