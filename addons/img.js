// addons/img.js
const { downloadContentFromMessage } = require('@whiskeysockets/baileys');

module.exports = {
  commands: ['img', 'vision'],
  handler: async (sock, msg, args) => {
    const jid = msg.key.remoteJid;

    // 1. Detectar si el mensaje es una imagen o si está respondiendo (citando) a una
    const isImage = msg.message?.imageMessage;
    const isQuotedImage = msg.message?.extendedTextMessage?.contextInfo?.quotedMessage?.imageMessage;

    if (!isImage && !isQuotedImage) {
      await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
      return;
    }

    const imageMessage = isImage ? msg.message.imageMessage : msg.message.extendedTextMessage.contextInfo.quotedMessage.imageMessage;

    try {
      await sock.sendMessage(jid, { react: { text: '👁️', key: msg.key } });

      // 2. Descargar el stream de la imagen y convertirlo a un Buffer
      const stream = await downloadContentFromMessage(imageMessage, 'image');
      let buffer = Buffer.from([]);
      for await (const chunk of stream) {
        buffer = Buffer.concat([buffer, chunk]);
      }

      // 3. Convertir el Buffer a Base64 para el payload de DeepSeek
      const base64Image = buffer.toString('base64');
      const mimeType = imageMessage.mimetype || 'image/jpeg';
      const dataUrl = `data:${mimeType};base64,${base64Image}`;

      // 4. Preparar las instrucciones (y añadir argumentos del usuario si los hay)
      const userArgs = args.join(' ').trim();
      const systemInstruction = "Describe esta imagen detalladamente. Si la imagen contiene texto, es de suma importancia que lo extraigas y lo proporciones textualmente tal cual aparece.";
      const finalPrompt = userArgs ? `${systemInstruction}\n\nInstrucción adicional del usuario: ${userArgs}` : systemInstruction;

      // 5. Petición a DeepSeek vía fetch nativo (formato OpenAI Multimodal)
      const dsRes = await fetch('https://api.deepseek.com/chat/completions', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${process.env.DEEPSEEK_API_KEY}`,
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({
          model: 'deepseek-chat', // O el modelo específico multimodal que estés utilizando
          messages: [
            {
              role: 'user',
              content: [
                {
                  type: 'text',
                  text: finalPrompt
                },
                {
                  type: 'image_url',
                  image_url: { 
                    url: dataUrl 
                  }
                }
              ]
            }
          ]
        })
      });

      if (!dsRes.ok) {
        const errText = await dsRes.text();
        throw new Error(`Fallo en DeepSeek: ${dsRes.status} - ${errText}`);
      }

      const dsData = await dsRes.json();
      const description = dsData.choices[0].message.content.trim();

      // 6. Enviar el resultado al chat
      await sock.sendMessage(jid, { text: `📸 \n\n${description}` }, { quoted: msg });
      await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

    } catch (err) {
      console.error('Error en addon de imagen (img.js):', err.message);
      await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
      await sock.sendMessage(jid, { text: `⚠️ No se pudo procesar la imagen.\nDetalle: ${err.message}` }, { quoted: msg });
    }
  }
};
