// addons/cordial.js

const fs = require('fs-extra');
const path = require('path');
const ffmpeg = require('fluent-ffmpeg');
const { downloadContentFromMessage } = require('@whiskeysockets/baileys');
const googleTTS = require('google-tts-api');
const crypto = require('crypto');

// Configuración de APIs (Asegúrate de tenerlas en tu .env)
const GROQ_API_KEY = process.env.GROQ_API_KEY;
const DEEPSEEK_API_KEY = process.env.DEEPSEEK_API_KEY;

// Helper para obtener la duración de un audio usando ffprobe
const getAudioDuration = (filePath) => {
  return new Promise((resolve, reject) => {
    ffmpeg.ffprobe(filePath, (err, metadata) => {
      if (err) return reject(err);
      resolve(metadata.format.duration);
    });
  });
};

// Helper para generar cadena de atempo
const buildAtempoChain = (ratio) => {
  let tempos = [];
  let currentRatio = ratio;

  while (currentRatio > 2.0) {
    tempos.push('atempo=2.0');
    currentRatio /= 2.0;
  }
  while (currentRatio < 0.5) {
    tempos.push('atempo=0.5');
    currentRatio /= 0.5;
  }
  if (currentRatio !== 1) {
    tempos.push(`atempo=${currentRatio.toFixed(2)}`);
  }

  return tempos.length > 0 ? tempos.join(',') : 'anull';
};

module.exports = {
  commands: ['cordial', 'cord'],
  handler: async (sock, msg, args, store) => {
    const jid = msg.key.remoteJid;
    const quotedMsg = msg.message?.extendedTextMessage?.contextInfo?.quotedMessage;
    const audioMsg = quotedMsg?.audioMessage;

    if (!audioMsg) {
      await sock.sendMessage(jid, { text: '❌ Debes responder a un mensaje de audio.' }, { quoted: msg });
      return;
    }

    if (audioMsg.seconds > 240) {
      await sock.sendMessage(jid, { text: '❌ El audio es muy largo. Máximo 4 minutos.' }, { quoted: msg });
      return;
    }

    const sessionId = crypto.randomBytes(4).toString('hex');
    const tempDir = path.join(__dirname, `../temp_cordial_${sessionId}`);
    await fs.ensureDir(tempDir);

    const rawAudioPath = path.join(tempDir, 'raw.ogg');
    const inputAudioPath = path.join(tempDir, 'input.mp3'); 
    const outputAudioPath = path.join(tempDir, 'output.ogg');

    try {
      await sock.sendMessage(jid, { react: { text: '👂', key: msg.key } });

      // 1. Descargar audio usando el método robusto (igual que en trans.js)
      const stream = await downloadContentFromMessage(audioMsg, 'audio');
      let buffer = Buffer.from([]);
      for await (const chunk of stream) {
        buffer = Buffer.concat([buffer, chunk]);
      }
      await fs.writeFile(rawAudioPath, buffer);

      // 1.5 Convertir a MP3 limpio
      await new Promise((resolve, reject) => {
        ffmpeg(rawAudioPath)
          .audioCodec('libmp3lame')
          .save(inputAudioPath)
          .on('end', resolve)
          .on('error', reject);
      });

      // 2. Transcribir con Groq usando Blob y FormData NATIVOS
      const mp3Buffer = await fs.readFile(inputAudioPath);
      const blob = new Blob([mp3Buffer], { type: 'audio/mpeg' });
      
      const formData = new FormData(); // FormData nativo global de Node
      formData.append('file', blob, 'audio.mp3');
      formData.append('model', 'whisper-large-v3');
      formData.append('response_format', 'verbose_json');
      formData.append('timestamp_granularities[]', 'word');

      const groqRes = await fetch('https://api.groq.com/openai/v1/audio/transcriptions', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${GROQ_API_KEY}`
          // El fetch nativo maneja el Content-Type y el boundary automáticamente
        },
        body: formData
      });

      const groqData = await groqRes.json();

      if (groqData.error) {
        console.error('Error de API Groq:', groqData.error);
        throw new Error(`Error de Groq: ${groqData.error.message}`);
      }

      if (!groqData.text || !groqData.words) {
        console.error('Respuesta anómala de Groq:', groqData);
        throw new Error('No se pudo transcribir el audio o no hay timestamps.');
      }

      await sock.sendMessage(jid, { react: { text: '🧠', key: msg.key } });

      // 3. Analizar con DeepSeek
      const systemPrompt = `Eres un filtro de cordialidad estricto. Recibirás una transcripción. Identifica insultos, palabras malsonantes o frases agresivas.
Genera reemplazos extremadamente amables solo para esas palabras, educados o poéticos para esas frases, invirtiendo la agresividad a bondad. .inetanta covervar un +50% del las palabras originales sn editarlas
Responde ÚNICAMENTE con un JSON con la siguiente estructura, sin markdown, sin texto adicional:
{
  "replacements": [
    {"original": "puto hocico", "replacement": "hermosa boquita"},
    {"original": "te rompo la cara", "replacement": "te doy una caricia"}
  ]
}`;

      const deepSeekRes = await fetch('https://api.deepseek.com/v1/chat/completions', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${DEEPSEEK_API_KEY}`
        },
        body: JSON.stringify({
          model: 'deepseek-chat',
          messages: [
            { role: 'system', content: systemPrompt },
            { role: 'user', content: groqData.text }
          ],
          temperature: 0.3
        })
      });

      const dsData = await deepSeekRes.json();
      let dsJsonStr = dsData.choices[0].message.content.replace(/```json/g, '').replace(/```/g, '').trim();
      let analysis = JSON.parse(dsJsonStr);

      if (!analysis.replacements || analysis.replacements.length === 0) {
        await sock.sendMessage(jid, { text: '✨ Este audio ya es bastante cordial. No hay nada que parchear.' }, { quoted: msg });
        await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
        await fs.remove(tempDir);
        return;
      }

      await sock.sendMessage(jid, { react: { text: '🛠️', key: msg.key } });

      // 4. Mapear reemplazos
      const whisperWords = groqData.words.map(w => ({
        ...w,
        cleanWord: w.word.toLowerCase().replace(/[^\w\sñáéíóú]/gi, '')
      }));

      const patches = [];

      for (const rep of analysis.replacements) {
        const targetWords = rep.original.toLowerCase().replace(/[^\w\sñáéíóú]/gi, '').split(' ');

        for (let i = 0; i <= whisperWords.length - targetWords.length; i++) {
          let match = true;
          for (let j = 0; j < targetWords.length; j++) {
            if (whisperWords[i + j].cleanWord !== targetWords[j]) {
              match = false;
              break;
            }
          }

          if (match) {
            const startT = whisperWords[i].start;
            const endT = whisperWords[i + targetWords.length - 1].end;
            patches.push({
              start: startT,
              end: endT,
              text: rep.replacement,
              duration: endT - startT
            });
            break;
          }
        }
      }

      if (patches.length === 0) {
         throw new Error('DeepSeek encontró insultos pero no pude mapearlos al audio.');
      }

      // 5. Descargar audios TTS y armar el script de FFmpeg
      let ffmpegInputs = ['-i', inputAudioPath]; 
      let complexFilter = '';
      let muteSections = patches.map(p => `between(t,${p.start},${p.end})`).join('+');

      complexFilter += `[0:a]volume=enable='${muteSections}':volume=0[muted];`;

      let mixInputs = ['[muted]'];

      for (let i = 0; i < patches.length; i++) {
        const patch = patches[i];
        const ttsUrl = googleTTS.getAudioUrl(patch.text, { lang: 'es', slow: false, host: 'https://translate.google.com' });

        const ttsPath = path.join(tempDir, `tts_${i}.mp3`);
        const ttsRes = await fetch(ttsUrl);
        const ttsBuffer = Buffer.from(await ttsRes.arrayBuffer());
        await fs.writeFile(ttsPath, ttsBuffer);

        ffmpegInputs.push('-i', ttsPath);
        const ttsRealDuration = await getAudioDuration(ttsPath);

        const speedRatio = ttsRealDuration / patch.duration;
        const tempoFilter = buildAtempoChain(speedRatio);

        const delayMs = Math.floor(patch.start * 1000);

        complexFilter += `[${i + 1}:a]${tempoFilter},adelay=${delayMs}|${delayMs}[patch${i}];`;
        mixInputs.push(`[patch${i}]`);
      }

      complexFilter += `${mixInputs.join('')}amix=inputs=${patches.length + 1}:duration=first:dropout_transition=0:normalize=0[out]`;

      // 6. Ejecutar FFmpeg
      await new Promise((resolve, reject) => {
        let cmd = ffmpeg();

        for (let i = 0; i < ffmpegInputs.length; i += 2) {
           cmd = cmd.input(ffmpegInputs[i+1]);
        }

        cmd.complexFilter(complexFilter, 'out')
           .outputOptions(['-c:a libopus', '-b:a 64k', '-vbr on'])
           .save(outputAudioPath)
           .on('end', resolve)
           .on('error', (err, stdout, stderr) => {
             console.error('FFmpeg stderr:', stderr);
             reject(err);
           });
      });

      // 7. Enviar Audio Resultante
      const finalAudioBuffer = await fs.readFile(outputAudioPath);
      await sock.sendMessage(jid, {
        audio: finalAudioBuffer,
        mimetype: 'audio/ogg; codecs=opus',
        ptt: true
      }, { quoted: msg });

      await sock.sendMessage(jid, { react: { text: '✨', key: msg.key } });

    } catch (err) {
      console.error('Error en addon /cordial:', err);
      await sock.sendMessage(jid, { text: `❌ Hubo un error de procesamiento: ${err.message}` }, { quoted: msg });
      await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
    } finally {
      try {
        await fs.remove(tempDir);
      } catch (e) {
        console.error('Error borrando temp dir:', e);
      }
    }
  }
};
