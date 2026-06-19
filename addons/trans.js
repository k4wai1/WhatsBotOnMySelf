// addons/trans.js
const { downloadContentFromMessage } = require('@whiskeysockets/baileys');

module.exports = {
  commands: ['tr', 'tri'],
  handler: async (sock, msg, args) => {
    const jid = msg.key.remoteJid;

    // Detectar qué comando específico detonó este handler (.tr o .tri)
    const rawText = msg.message?.conversation || 
                    msg.message?.extendedTextMessage?.text || 
                    msg.message?.audioMessage?.caption || '';
    const commandUsed = rawText.trim().split(/\s+/)[0].toLowerCase().replace(/^[.!/]/, '');

    const isAudio = msg.message?.audioMessage;
    const isQuotedAudio = msg.message?.extendedTextMessage?.contextInfo?.quotedMessage?.audioMessage;

    if (!isAudio && !isQuotedAudio) {
      await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
      return;
    }

    const audioMessage = isAudio ? msg.message.audioMessage : msg.message.extendedTextMessage.contextInfo.quotedMessage.audioMessage;

    try {
      await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

      // 1. Descargar el buffer
      const stream = await downloadContentFromMessage(audioMessage, 'audio');
      let buffer = Buffer.from([]);
      for await (const chunk of stream) {
        buffer = Buffer.concat([buffer, chunk]);
      }

      // 2. Preparar el FormData para Groq
      const blob = new Blob([buffer], { type: 'audio/ogg' });
      const formData = new FormData();
      formData.append('file', blob, 'audio.ogg');
      formData.append('model', 'whisper-large-v3'); 
      formData.append('response_format', 'json');

      // 3. Transcripción con Groq
      const groqRes = await fetch('https://api.groq.com/openai/v1/audio/transcriptions', {
        method: 'POST',
        headers: { 'Authorization': `Bearer ${process.env.GROQ_API_KEY}` },
        body: formData
      });

      if (!groqRes.ok) throw new Error(`Error en Groq: ${groqRes.statusText}`);
      
      const groqData = await groqRes.json();
      const transcription = groqData.text;

      if (!transcription) throw new Error('Groq no devolvió texto.');

      // 4. Lógica .tr (Cruda)
      if (commandUsed === 'tr') {
        await sock.sendMessage(jid, { text: `🎙️ \n${transcription}` }, { quoted: msg });
        await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
        return;
      }

      // 5. Lógica .tri (IA con limpieza de texto)
      if (commandUsed === 'tri') {
        try {
          const dsRes = await fetch('https://api.deepseek.com/chat/completions', {
            method: 'POST',
            headers: {
              'Authorization': `Bearer ${process.env.DEEPSEEK_API_KEY}`,
              'Content-Type': 'application/json'
            },
            body: JSON.stringify({
              model: 'deepseek-v4-flash',
              messages: [
                { 
                  role: 'system', 
                  content: 'Eres un editor de texto experto. Tu única tarea es tomar la transcripción de voz proporcionada y limpiarla: elimina muletillas, tartamudeos y repeticiones innecesarias, y corrige errores gramaticales. Es de suma importancia que mantengas estrictamente el mismo idioma en el que está hablada la transcripción original. No añadas introducciones, explicaciones, ni comentarios extra. Solo devuelve el texto limpio.' 
                },
                { 
                  role: 'user', 
                  content: transcription 
                }
              ]
            })
          });

          if (!dsRes.ok) throw new Error('Fallo en la respuesta de DeepSeek');

          const dsData = await dsRes.json();
          const cleanText = dsData.choices[0].message.content.trim();

          await sock.sendMessage(jid, { text: `✨ \n${cleanText}` }, { quoted: msg });
          await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

        } catch (dsError) {
          console.error('Error en DeepSeek:', dsError);
          // Si DeepSeek falla por rate limit u otra cosa, mandamos la transcripción cruda pero con el icono de advertencia
          await sock.sendMessage(jid, { text: `⚠️ (Fallback a transcripción cruda)\n\n${transcription}` }, { quoted: msg });
        }
      }

    } catch (err) {
      console.error('Error en addon de transcripción:', err.message);
      await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
    }
  }
};
