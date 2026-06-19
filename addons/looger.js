// addons/looger.js

const formatBytes = (bytes) => {
    if (!bytes || bytes === 0) return '0 Bytes';
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
};

module.exports = {
    init: (sock) => {
        sock.ev.on('messages.upsert', async (m) => {
            // Aceptamos 'notify' (entrantes) y 'append' (salientes del propio bot)
            if (m.type !== 'notify' && m.type !== 'append') return;

            for (const msg of m.messages) {
                try {
                    if (!msg.message) continue;

                    const jid = msg.key.remoteJid;
                    const time = new Date(msg.messageTimestamp * 1000).toLocaleTimeString();
                    const msgType = Object.keys(msg.message)[0];
                    const msgData = msg.message[msgType];
                    const isFromMe = msg.key.fromMe; // 👈 Detecta si el remitente eres tú/el bot

                    // 1. Clasificación del Chat y Colores
                    let tipoChat = '👤 DM';
                    let color = '\x1b[36m'; // Cyan

                    if (jid.endsWith('@g.us')) {
                        tipoChat = '👥 GRUPO';
                        color = '\x1b[33m'; // Amarillo
                    } else if (jid.endsWith('@newsletter')) {
                        tipoChat = '📢 CANAL';
                        color = '\x1b[35m'; // Magenta
                    }

                    // Si el mensaje es del bot, sobrescribimos el color para resaltarlo
                    if (isFromMe) {
                        color = '\x1b[32m'; // Verde
                    }

                    // Forzamos el nombre si es el bot
                    const senderName = isFromMe ? '🤖 (Bot)' : (msg.pushName || 'Anónimo');
                    const senderNum = (msg.key.participant || jid).split('@')[0];

                    // 2. Extraer texto principal
                    let content = msg.message?.conversation || 
                                  msg.message?.extendedTextMessage?.text || 
                                  '';

                    // 3. Captura de Detalles de Medios/Archivos/Stickers
                    let mediaDetails = '';
                    if (msgData?.fileLength) {
                        const size = formatBytes(msgData.fileLength);
                        const mime = msgData.mimetype || 'Desconocido';
                        const isAnimated = msgData.isAnimated ? 'Animado' : 'Estático';
                        
                        mediaDetails = `\n📦 \x1b[1mArchivo:\x1b[0m ${size} | ${mime}`;
                        if (msgType === 'stickerMessage') {
                            mediaDetails += ` (${isAnimated})`;
                        }
                        
                        if (!content) content = msgData.caption || `[Adjunto: ${msgType}]`;
                    }

                    // 4. Captura de Emojis (Reacciones)
                    let reactionDetails = '';
                    if (msgType === 'reactionMessage') {
                        const emoji = msgData.text;
                        const targetId = msgData.key?.id || 'Desconocido';
                        if (emoji) {
                            content = `[Reaccionó con Emoji]`;
                            reactionDetails = `\n🎭 \x1b[1mEmoji:\x1b[0m ${emoji}\n🎯 \x1b[1mAl Mensaje ID:\x1b[0m ${targetId}`;
                        } else {
                            content = `[Eliminó su reacción]`;
                            reactionDetails = `\n🗑️ \x1b[1mAl Mensaje ID:\x1b[0m ${targetId}`;
                        }
                    }

                    if (!content) content = `[Evento del sistema: ${msgType}]`;

                    // 5. Renderizado
                    console.log(`\n${color}╭─── [${tipoChat}] ⏱️ ${time}\x1b[0m`);
                    console.log(`${color}│\x1b[0m 🆔 \x1b[1mID Chat:\x1b[0m ${jid}`);
                    console.log(`${color}│\x1b[0m 👤 \x1b[1mDe:\x1b[0m ${senderName} (+${senderNum})`);
                    console.log(`${color}│\x1b[0m 💬 \x1b[1mMensaje:\x1b[0m ${content}${mediaDetails}${reactionDetails}`);
                    console.log(`${color}╰───────────────────────────────────────\x1b[0m`);

                } catch (err) {
                    console.error('\x1b[31m[LOG-ERROR] Fallo al procesar el logger:\x1b[0m', err.message);
                }
            }
        });
    }
};
