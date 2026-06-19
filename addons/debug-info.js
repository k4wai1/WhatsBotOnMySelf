// addons/debug-info.js

module.exports = {
    commands: ['info', 'debug'],
    handler: async (sock, msg, args, store) => {
        const jid = msg.key.remoteJid;
        let reporte = `🔍 *REPORTE DE EXTRACCIÓN DE DATOS*\n\n`;

        try {
            await sock.sendMessage(jid, { react: { text: '📡', key: msg.key } });

            // 1. Detección del Objetivo (Quien fue citado o el remitente)
            const contextInfo = msg.message?.extendedTextMessage?.contextInfo;
            let targetId = contextInfo?.participant
                || (contextInfo?.mentionedJid?.length > 0 ? contextInfo.mentionedJid[0] : null)
                || msg.key.participant
                || jid;

            reporte += `🎯 *ID Objetivo crudo:* ${targetId}\n\n`;

            // ----- LÓGICA AVANZADA DE RESOLUCIÓN DE LID -----
            let pureJid = '';
            let tipoId = 'Desconocido';
            let metodoResolucion = '❌ Bloqueo de Privacidad de WA (Comunidad/Anuncio/Ajustes)';

            if (targetId.endsWith('@lid')) {
                tipoId = 'LID (Identidad Oculta)';

                // MÉTODO 1: ¿Es el LID del propio Bot?
                const botLid = sock.authState?.creds?.me?.lid;
                if (botLid && targetId === botLid) {
                    const botId = sock.user?.id || sock.authState?.creds?.me?.id;
                    if (botId) {
                        pureJid = botId.split(':')[0] + '@s.whatsapp.net';
                        metodoResolucion = '✅ Resuelto (Identidad propia del Bot)';
                    }
                }

                // MÉTODO 2: Extraer del Metadata del Grupo
                if (!pureJid && jid.endsWith('@g.us')) {
                    try {
                        const metadata = await sock.groupMetadata(jid);
                        const participante = metadata.participants.find(p => p.lid === targetId || p.id === targetId);

                        if (participante && participante.id && participante.id.endsWith('@s.whatsapp.net')) {
                            pureJid = participante.id;
                            metodoResolucion = '✅ Resuelto vía Metadatos del Grupo';
                        }
                    } catch (e) {
                        // Silencioso, seguimos al método 3
                    }
                }

                // MÉTODO 3: Búsqueda EXHAUSTIVA y cruzada en la Caché (Store)
                if (!pureJid && store?.contacts) {
                    for (const key in store.contacts) {
                        const c = store.contacts[key];
                        // Buscamos si el objetivo coincide con la llave, el LID interno o el ID interno
                        if (key === targetId || c.lid === targetId || c.id === targetId) {
                            if (key.endsWith('@s.whatsapp.net')) {
                                pureJid = key;
                                metodoResolucion = '✅ Resuelto vía Llave Maestra en Store';
                                break;
                            } else if (c.id && c.id.endsWith('@s.whatsapp.net')) {
                                pureJid = c.id;
                                metodoResolucion = '✅ Resuelto vía Propiedad ID en Store';
                                break;
                            } else if (c.lid && c.lid.endsWith('@s.whatsapp.net')) {
                                pureJid = c.lid; // Raro, pero previene estructuras anómalas
                                metodoResolucion = '✅ Resuelto vía Cruce Anómalo en Store';
                                break;
                            }
                        }
                    }
                }
            } else {
                tipoId = 'JID Estándar (Número Real)';
                const numClean = targetId.split('@')[0].split(':')[0];
                pureJid = `${numClean}@s.whatsapp.net`;
                metodoResolucion = '✅ No requirió conversión (Es público)';
            }

            // Extraemos el número telefónico final libre de @s.whatsapp.net
            const finalNumber = pureJid ? pureJid.split('@')[0] : 'No resuelto';

            // ----- PRUEBAS DE TELÉFONO / JID -----
            reporte += `📱 *MÉTODOS DE NÚMERO:*\n`;
            reporte += `🔹 *Tipo detectado:* ${tipoId}\n`;
            reporte += `🔹 *Estado:* ${metodoResolucion}\n`;
            reporte += `${pureJid ? '✅' : '❌'} *Número Real:* ${finalNumber}\n`;
            reporte += `   ↳ _JID Puro final: ${pureJid || 'No disponible'}_\n\n`;

            // ----- PRUEBAS DE NOMBRE -----
            reporte += `👤 *MÉTODOS DE NOMBRE:*\n`;

            // Método 1: msg.pushName
            const isSelfOrSender = targetId === msg.key.participant || (!msg.key.participant && targetId === jid);
            if (msg.pushName && isSelfOrSender) {
                reporte += `✅ *msg.pushName:* ${msg.pushName}\n`;
            } else {
                reporte += `❌ *msg.pushName:* Indisponible\n`;
                reporte += `   ↳ _Falla: WhatsApp no adjunta el pushName en mensajes citados._\n`;
            }

            // Método 2: Búsqueda dinámica en Store
            // Usamos el JID puro si lo logramos resolver, sino usamos el raw targetId
            const storeKey = pureJid || targetId;
            const storeContact = store?.contacts?.[storeKey];
            
            if (storeContact) {
                const bestName = storeContact.verifiedName || storeContact.name || storeContact.notify;
                if (bestName) {
                    reporte += `✅ *Store Caché:* ${bestName}\n`;
                    reporte += `   ↳ _Origen: ${storeContact.verifiedName ? 'Verificado' : (storeContact.name ? 'Agenda' : 'Notificación')}_\n`;
                } else {
                    reporte += `❌ *Store Caché:* Usuario encontrado en memoria pero sin nombre.\n`;
                }
            } else {
                reporte += `❌ *Store Caché:* Indisponible.\n`;
                reporte += `   ↳ _Falla: No hay registros previos de interacción con esta entidad._\n`;
            }

            // Enviar reporte
            await sock.sendMessage(jid, { text: reporte }, { quoted: msg });
            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

        } catch (error) {
            console.error(error);
            await sock.sendMessage(jid, { text: `❌ Error de depuración: ${error.message}` });
        }
    }
};
