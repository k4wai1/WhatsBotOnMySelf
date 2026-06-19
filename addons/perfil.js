// addons/perfil.js

module.exports = {
  commands: ['pf', 'perfil'],
  handler: async (sock, msg, args, store) => {
    const jid = msg.key.remoteJid;

    try {
      await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

      let targetId;
      const contextInfo = msg.message?.extendedTextMessage?.contextInfo;
      const isGroup = jid.endsWith('@g.us');

      // 1. Obtención de ID inteligente
      if (contextInfo?.mentionedJid?.length > 0) {
        targetId = contextInfo.mentionedJid[0];
      } else if (contextInfo?.quotedMessage) {
        targetId = contextInfo.participant || (isGroup ? msg.key.participant : jid);
      } else {
        targetId = msg.key.participant || (msg.key.fromMe ? sock.user.id : jid);
      }

      // Traductor de @lid a JID si existe en memoria
      if (targetId.includes('@lid')) {
        const contactLid = store?.contacts?.[targetId];
        if (contactLid?.pn) {
            targetId = `${contactLid.pn}@s.whatsapp.net`;
        }
      }

      const pureJid = `${targetId.split('@')[0].split(':')[0]}@s.whatsapp.net`;
      let ppUrl = null;

      // ----- CASCADA DE REINTENTOS PARA LA FOTO -----
      try {
        ppUrl = await sock.profilePictureUrl(targetId, 'image');
      } catch (e1) {
        try {
          ppUrl = await sock.profilePictureUrl(pureJid, 'image');
        } catch (e2) {
          try {
            ppUrl = await sock.profilePictureUrl(pureJid, 'preview');
          } catch (e3) {
            ppUrl = null;
          }
        }
      }

      if (!ppUrl) {
        await sock.sendMessage(jid, { react: { text: '🤷‍♂️', key: msg.key } });
        await sock.sendMessage(jid, { 
            text: '❌ El bot no tiene acceso a la foto de perfil (Oculta por privacidad o sin foto configurada).' 
        }, { quoted: msg });
        return;
      }

      const response = await fetch(ppUrl);
      const buffer = Buffer.from(await response.arrayBuffer());

      await sock.sendMessage(jid, { image: buffer, caption: '📸' }, { quoted: msg });
      await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

    } catch (err) {
      console.error('Error crítico en addon .perfil:', err.message);
      await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
    }
  }
};
