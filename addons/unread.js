// addons/unread.js
const fs = require('fs');
const path = require('path');

const HISTORY_FILE = path.join(__dirname, '..', 'assets', 'chatHistory.json');
const MAX_HISTORY = 1000;
const SAVE_INTERVAL = 200; // guardar cada 200 mensajes

let chatHistory = new Map();
let msgCounter = 0;

// ─── Cargar historial desde disco ─────────────────────────────────────────
function loadHistory() {
  try {
    if (fs.existsSync(HISTORY_FILE)) {
      const raw = fs.readFileSync(HISTORY_FILE, 'utf-8');
      chatHistory = new Map(Object.entries(JSON.parse(raw)));
      console.log('✅ [unread] Historial cargado desde disco.');
    }
  } catch (e) {
    console.error('❌ [unread] Error cargando historial:', e.message);
  }
}

function saveHistory() {
  try {
    fs.writeFileSync(HISTORY_FILE, JSON.stringify(Object.fromEntries(chatHistory), null, 2));
  } catch (e) {
    console.error('❌ [unread] Error guardando historial:', e.message);
  }
}

// ─── Función de inicialización (background: escucha mensajes) ────────────
function init(sock) {
  loadHistory();

  sock.ev.on('messages.upsert', async (m) => {
    if (m.type !== 'notify') return;
    const msg = m.messages[0];
    if (!msg.message || msg.key.fromMe) return;

    const jid = msg.key.remoteJid;
    const senderJid = msg.key.participant || msg.key.remoteJid;
    const sender = senderJid.split('@')[0];

    let text = msg.message?.conversation ||
               msg.message?.extendedTextMessage?.text ||
               msg.message?.imageMessage?.caption ||
               msg.message?.videoMessage?.caption || '';

    // ── Transcripción de audios con Groq ─────────────────────────────────
    const audioMsg = msg.message?.audioMessage;
    if (audioMsg && process.env.GROQ_API_KEY) {
      try {
        const { downloadContentFromMessage } = require('@whiskeysockets/baileys');
        const stream = await downloadContentFromMessage(audioMsg, 'audio');
        let buf = Buffer.from([]);
        for await (const chunk of stream) buf = Buffer.concat([buf, chunk]);

        const blob = new Blob([buf], { type: audioMsg.mimetype || 'audio/ogg' });
        const fd = new FormData();
        fd.append('file', blob, 'audio.ogg');
        fd.append('model', 'whisper-large-v3');
        fd.append('response_format', 'json');
        fd.append('language', 'es');

        const res = await fetch('https://api.groq.com/openai/v1/audio/transcriptions', {
          method: 'POST',
          headers: { 'Authorization': `Bearer ${process.env.GROQ_API_KEY}` },
          body: fd
        });
        if (res.ok) {
          const data = await res.json();
          text = `[Audio] ${data.text}`;
        }
      } catch (e) {
        console.error('❌ [unread] Error transcribiendo audio:', e.message);
        text = '[Audio ininteligible]';
      }
    }

    if (!text) return;

    // ── Almacenar en el historial ────────────────────────────────────────
    if (!chatHistory.has(jid)) chatHistory.set(jid, []);
    const history = chatHistory.get(jid);
    history.push({ text, sender, t: Date.now() });
    if (history.length > MAX_HISTORY) history.shift();

    // ── Autoguardado cada SAVE_INTERVAL mensajes ─────────────────────────
    msgCounter++;
    if (msgCounter >= SAVE_INTERVAL) {
      msgCounter = 0;
      saveHistory();
    }
  });
}

// ─── Comando: unread ────────────────────────────────────────────────────────
const commands = ['unread'];

async function handler(sock, msg, args) {
  const jid = msg.key.remoteJid;

  // Obtener la cantidad de mensajes a resumir
  const requested = parseInt(args[0], 10) || 500;
  const limit = Math.min(requested, MAX_HISTORY);

  if (limit < 10) {
    await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
    return;
  }

  const history = chatHistory.get(jid);
  if (!history || history.length === 0) {
    await sock.sendMessage(jid, { text: '📭 No hay historial para este chat aún.' });
    return;
  }

  if (!process.env.DEEPSEEK_API_KEY) {
    await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
    return;
  }

  try {
    await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

    const slice = history.slice(-limit);
    const formatted = slice.map((m, i) => {
      const ago = slice.length - i;
      return `[Hace ${ago} msgs] @${m.sender}: ${m.text}`;
    }).join('\n');

    const systemPrompt = `Eres un asistente experto en resumir conversaciones caóticas de grupos de WhatsApp.
Se te entregará un registro de chat. Cada línea empieza con "[Hace X msgs]" indicando la antigüedad del mensaje, seguido del remitente y el contenido. Algunos mensajes indican "[Audio]", dales la misma importancia.

Tu tarea:
1. Analizar el texto e identificar los temas principales y puntos más relevantes discutidos.
2. Generar un resumen estructurado estrictamente como una lista de viñetas (bullet points).
3. Al final de cada viñeta, DEBES incluir una estimación de hace cuántos mensajes ocurrió esto (ej: "(Aprox. hace 450 msgs)").
4. Ignora comandos de bots, spam corto o saludos irrelevantes. Ve al grano.
5. Mantén un tono neutral, claro y fácil de escanear. No inventes información.`;

    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 30_000);

    const response = await fetch('https://api.deepseek.com/chat/completions', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${process.env.DEEPSEEK_API_KEY}`
      },
      signal: controller.signal,
      body: JSON.stringify({
        model: 'deepseek-chat',
        messages: [
          { role: 'system', content: systemPrompt },
          { role: 'user', content: formatted }
        ],
        temperature: 0.3
      })
    });

    clearTimeout(timeout);

    if (!response.ok) throw new Error(`DeepSeek HTTP ${response.status}`);

    const data = await response.json();
    const summary = data.choices[0].message.content;

    await sock.sendMessage(jid, {
      text: `🧠 *Resumen de los últimos ${slice.length} mensajes:*\n\n${summary}`
    });

  } catch (error) {
    console.error('❌ [unread] Error:', error.message);
    let msg = '❌ Error al generar el resumen.';
    if (error.name === 'AbortError') msg += ' La API tardó demasiado.';
    else msg += ` ${error.message}`;
    await sock.sendMessage(jid, { text: msg });
  }
}

module.exports = { commands, handler, init };
