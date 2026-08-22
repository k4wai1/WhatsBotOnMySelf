// addons/resu.js
const { downloadContentFromMessage } = require('@whiskeysockets/baileys');
const { execFile } = require('child_process');
const fs = require('fs');
const path = require('path');
const os = require('os');
const { promisify } = require('util');
const writeFile = promisify(fs.writeFile);
const readFile = promisify(fs.readFile);
const unlink = promisify(fs.unlink);
const execFilePromise = promisify(execFile);

module.exports = {
  commands: ['resu'],
  handler: async (sock, msg, args) => {
    const jid = msg.key.remoteJid;
    const userPrompt = args.join(' ').trim();

    const isAudio = msg.message?.audioMessage;
    const isQuotedAudio = msg.message?.extendedTextMessage?.contextInfo?.quotedMessage?.audioMessage;
    const isVideo = msg.message?.videoMessage;
    const isQuotedVideo = msg.message?.extendedTextMessage?.contextInfo?.quotedMessage?.videoMessage;

    if (!isAudio && !isQuotedAudio && !isVideo && !isQuotedVideo) {
      await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
      return;
    }

    try {
      await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

      let buffer, audioBlob, mimeType;

      if (isAudio || isQuotedAudio) {
        // --- AUDIO (existente) ---
        const audioMessage = isAudio ? msg.message.audioMessage : msg.message.extendedTextMessage.contextInfo.quotedMessage.audioMessage;
        const stream = await downloadContentFromMessage(audioMessage, 'audio');
        buffer = Buffer.from([]);
        for await (const chunk of stream) {
          buffer = Buffer.concat([buffer, chunk]);
        }
        audioBlob = new Blob([buffer], { type: 'audio/ogg' });
      } else {
        // --- VIDEO: descargar y extraer audio con ffmpeg ---
        const videoMessage = isVideo ? msg.message.videoMessage : msg.message.extendedTextMessage.contextInfo.quotedMessage.videoMessage;
        const stream = await downloadContentFromMessage(videoMessage, 'video');
        buffer = Buffer.from([]);
        for await (const chunk of stream) {
          buffer = Buffer.concat([buffer, chunk]);
        }

        // Guardar el vídeo en un archivo temporal
        const tmpDir = os.tmpdir();
        const inputPath = path.join(tmpDir, `video_${Date.now()}.mp4`);
        const outputPath = path.join(tmpDir, `audio_${Date.now()}.mp3`);

        await writeFile(inputPath, buffer);

        // Extraer audio con ffmpeg
        try {
          await execFilePromise('ffmpeg', [
            '-i', inputPath,
            '-vn',               // sin video
            '-acodec', 'libmp3lame',
            '-q:a', '2',         // calidad
            '-y',                // sobrescribir
            outputPath
          ]);
        } catch (ffmpegErr) {
          // Limpiar archivos temporales
          await unlink(inputPath).catch(() => {});
          throw new Error('Error al extraer el audio del video con ffmpeg');
        }

        // Leer el audio resultante
        let audioBuffer;
        try {
          audioBuffer = await readFile(outputPath);
        } catch (readErr) {
          await unlink(inputPath).catch(() => {});
          await unlink(outputPath).catch(() => {});
          throw new Error('El archivo de audio extraído no pudo ser leído');
        }

        // Limpiar temporales
        await unlink(inputPath).catch(() => {});
        await unlink(outputPath).catch(() => {});

        if (audioBuffer.length === 0) {
          throw new Error('El video no contiene pista de audio');
        }

        audioBlob = new Blob([audioBuffer], { type: 'audio/mpeg' });
      }

      // --- Transcripción con Groq (igual) ---
      const formData = new FormData();
      formData.append('file', audioBlob, isVideo || isQuotedVideo ? 'audio.mp3' : 'audio.ogg');
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

      // --- Resumen con DeepSeek (igual) ---
      let systemInstruction = 'Eres un asistente experto en síntesis. Tu única tarea es leer la transcripción proporcionada y resumir su mensaje principal en exactamente UNA sola oración. Es estrictamente necesario que el resumen generado esté en el mismo idioma en el que está escrita la transcripción original. No añadas introducciones, viñetas ni comentarios extra.';

      if (userPrompt) {
        systemInstruction += ` Adicionalmente, el usuario ha solicitado que apliques esta condición/enfoque a tu resumen: "${userPrompt}".`;
      }

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
      await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
    }
  }
}
