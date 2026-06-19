// addons/ft.js
const { downloadMediaMessage } = require('@whiskeysockets/baileys');
const fs = require('fs');
const path = require('path');
const { exec } = require('child_process');
const { promisify } = require('util');

const execAsync = promisify(exec);

module.exports = {
  commands: ['ft'],
  handler: async (sock, msg, args) => {
    const jid = msg.key.remoteJid;
    const quotedMsg = msg.message?.extendedTextMessage?.contextInfo?.quotedMessage;

    // Discreción: Si no se cita un sticker, solo reacciona con duda
    if (!quotedMsg || !quotedMsg.stickerMessage) {
        await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
        return;
    }

    const CACHE_DIR = path.join(__dirname, '..', 'cache');
    if (!fs.existsSync(CACHE_DIR)) fs.mkdirSync(CACHE_DIR, { recursive: true });

    const filesToClean = [];

    try {
      await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

      // 1. Descargar Sticker 
      const buffer = await downloadMediaMessage({ message: quotedMsg }, 'buffer', {});
      const timestamp = Date.now();
      const inputPath = path.join(CACHE_DIR, `input_${timestamp}.webp`);
      fs.writeFileSync(inputPath, buffer);
      filesToClean.push(inputPath);

      const isAnimated = buffer.includes(Buffer.from('ANIM'));
      const outputExt = isAnimated ? 'gif' : 'png';
      const outputPath = path.join(CACHE_DIR, `output_${timestamp}.${outputExt}`);
      filesToClean.push(outputPath);

      // 2. Procesar con ImageMagick
      if (isAnimated) {
        const tempDir = path.join(CACHE_DIR, `temp_${timestamp}`);
        fs.mkdirSync(tempDir);
        filesToClean.push(tempDir); 

        await execAsync(`convert "${inputPath}" -coalesce "${tempDir}/frame_%04d.png"`, { timeout: 20000 });
        await execAsync(`convert -dispose background -delay 8 -loop 0 "${tempDir}/frame_*.png" -layers optimize "${outputPath}"`, { timeout: 20000 });
      } else {
        await execAsync(`convert "${inputPath}"[0] -background none "${outputPath}"`, { timeout: 20000 });
      }

      // 3. Enviar Resultado de forma discreta (Sin caption)
      const finalBuffer = fs.readFileSync(outputPath);
      await sock.sendMessage(jid, { 
        document: finalBuffer, 
        mimetype: isAnimated ? 'image/gif' : 'image/png',
        fileName: `file_${timestamp}.${outputExt}`
      }, { quoted: msg });

      await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

    } catch (err) {
      console.error('Error en addon .ft:', err.message);
      await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
    } finally {
      // Limpieza garantizada
      filesToClean.forEach(p => p && fs.existsSync(p) && fs.rmSync(p, { recursive: true, force: true }));
    }
  }
};
