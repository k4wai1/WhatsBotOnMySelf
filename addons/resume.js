// addons/resu.js
const { downloadContentFromMessage } = require('@whiskeysockets/baileys');

module.exports = {
  commands: ['resu'],
  handler: async (sock, msg, args) => {
    const jid = msg.key.remoteJid;
    const userPrompt = args.join(' ').trim();

    const isAudio = msg.message?.audioMessage;
    const isQuotedAudio = msg.message?.extendedTextMessage?.contextInfo?.quotedMessage?.audioMessage;

    if (!isAudio && !isQuotedAudio) {
      // Error silencioso: indica que falta el audio
      await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
      return;
    }

    const audioMessage = isAudio ? msg.message.audioMessage : msg.message.extendedTextMessage.contextInfo.quotedMessage.audioMessage;

    try {
      await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

      // 1. Descargar el buffer del audio
      const stream = await downloadContentFromMessage(audioMessage, 'audio');
      let buffer = Buffer.from([]);
      for await (const chunk of stream) {
        buffer = Buffer.concat([buffer, chunk]);
      }

      // 2. Transcripción con Groq
      const blob = new Blob([buffer], { type: 'audio/ogg' });
      const formData = new FormData();
      formData.append('file', blob, 'audio.ogg');
      formData.append('model', 'whisper-large-v3');
      formData.append('response_format', 'json');

      const groqRes = await fetch('https://api.groq.com/openai/v1/audio/transcriptions', {
        method: 'POST',
        headers: { 'Authorization': `Bearer ${process.env.GROQ_API_KEY}` },
        body: formData
      });

      if (!groqRes.ok) throw new Error(`Error en Groq: ${groqRes.statusText}`);

      const groqData = await groqRes.json();
      const transcription = groqData.text;

      if (!transcription) throw new Error('Groq no devolvió texto.');

      // 3. Prompt de DeepSeek mejorado para forzar el idioma de origen
      let systemInstruction = 'Eres un asistente experto en síntesis. Tu única tarea es leer la transcripción proporcionada y resumir su mensaje principal en exactamente UNA sola oración. Es estrictamente necesario que el resumen generado esté en el mismo idioma en el que está escrita la transcripción original. No añadas introducciones, viñetas ni comentarios extra.';
      
      if (userPrompt) {
        systemInstruction += ` Adicionalmente, el usuario ha solicitado que apliques esta condición/enfoque a tu resumen: "${userPrompt}".`;
      }

      // 4. Procesamiento con DeepSeek
      const dsRes = await fetch('https://api.deepseek.com/chat/completions', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${process.env.DEEPSEEK_API_KEY}`,
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({
          model: 'deepseek-v4-flash', 
          messages: [
            { role: 'system', content: systemInstruction },
            { role: 'user', content: transcription }
          ]
        })
      });

      if (!dsRes.ok) throw new Error('Fallo en la respuesta de DeepSeek');

      const dsData = await dsRes.json();
      const summaryText = dsData.choices[0].message.content.trim();

      await sock.sendMessage(jid, { text: `📝 \n${summaryText}` }, { quoted: msg });
      await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

    } catch (err) {
      console.error('Error en addon .resu:', err.message);
      // Falla en silencio con una reacción
      await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
    }
  }
};
