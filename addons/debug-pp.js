// addons/debug-pp.js

module.exports = {
    commands: ['getpp', 'debugpp'],
    handler: async (sock, msg, args, store) => {
        const jid = msg.key.remoteJid;
        let reporte = `🖼️ *REPORTE DE FOTO DE PERFIL*\n\n`;

        try {
            await sock.sendMessage(jid, { react: { text: '📸', key: msg.key } });

            const contextInfo = msg.message?.extendedTextMessage?.contextInfo;
            let targetId = contextInfo?.participant 
                || (contextInfo?.mentionedJid?.length > 0 ? contextInfo.mentionedJid[0] : null) 
                || msg.key.participant 
                || jid;

            const pureJid = `${targetId.split('@')[0].split(':')[0]}@s.whatsapp.net`;

            reporte += `🎯 *Objetivo:* ${pureJid}\n\n`;

            let finalUrl = null;
            let successMethod = '';

            // Método 1: JID Crudo, Alta resolución
            try {
                finalUrl = await sock.profilePictureUrl(targetId, 'image');
                successMethod = 'JID Crudo (Alta Res)';
                reporte += `✅ *Método 1 (Crudo/Alta):* ¡Éxito!\n`;
            } catch (err) {
                reporte += `❌ *Método 1 (Crudo/Alta):* ${err.data || err.message || '401/404 Unauthorized'}\n`;
                reporte += `   ↳ _Falla común si targetId tiene un ':15' al final._\n`;
            }

            // Método 2: JID Puro, Alta resolución
            if (!finalUrl) {
                try {
                    finalUrl = await sock.profilePictureUrl(pureJid, 'image');
                    successMethod = 'JID Puro (Alta Res)';
                    reporte += `✅ *Método 2 (Puro/Alta):* ¡Éxito!\n`;
                } catch (err) {
                    reporte += `❌ *Método 2 (Puro/Alta):* ${err.data || err.message || 'Fallo de Privacidad'}\n`;
                }
            }

            // Método 3: JID Puro, Vista Previa (Thumbnail)
            // A veces WhatsApp bloquea la imagen 'image' grande pero permite el 'preview' pequeño.
            if (!finalUrl) {
                try {
                    finalUrl = await sock.profilePictureUrl(pureJid, 'preview');
                    successMethod = 'JID Puro (Thumbnail)';
                    reporte += `✅ *Método 3 (Puro/Baja):* ¡Éxito!\n`;
                } catch (err) {
                    reporte += `❌ *Método 3 (Puro/Baja):* Privacidad estricta o sin foto.\n`;
                }
            }

            // Responder resultados
            if (finalUrl) {
                reporte += `\n🔗 *URL Obtenida:* ${finalUrl}\n`;
                await sock.sendMessage(jid, { 
                    image: { url: finalUrl }, 
                    caption: reporte 
                }, { quoted: msg });
            } else {
                reporte += `\n💀 *Conclusión:* Es imposible obtener la foto. El usuario la tiene restringida a "Mis Contactos" o no tiene foto.`;
                await sock.sendMessage(jid, { text: reporte }, { quoted: msg });
            }

            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

        } catch (error) {
            console.error(error);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
        }
    }
};
