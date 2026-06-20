// addons/files.js
const fs = require('fs');
const path = require('path');
const { downloadContentFromMessage } = require('@whiskeysockets/baileys');

const ROOT = path.join(__dirname, '..', 'files');
const PAGE_SIZE = 10;
const SESSION_TTL = 5 * 60 * 1000; // 5 min sin actividad

// ─── Sesiones por chat ───────────────────────────────────────────────────────
const sessions = {};

function getSession(jid) {
  let s = sessions[jid];
  if (!s || Date.now() - s.t > SESSION_TTL) {
    s = { cwd: ROOT, t: Date.now(), items: [], page: 0, msgKey: null };
    sessions[jid] = s;
  }
  s.t = Date.now();
  return s;
}

// ─── Utilidades ──────────────────────────────────────────────────────────────
function insideRoot(p) {
  return path.resolve(p).startsWith(ROOT);
}

function sortedEntries(dir) {
  if (!fs.existsSync(dir)) return [];
  const e = fs.readdirSync(dir, { withFileTypes: true }).map(d => ({
    name: d.name,
    path: path.join(dir, d.name),
    isDir: d.isDirectory()
  }));
  e.sort((a, b) => {
    if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
    return a.name.localeCompare(b.name, 'es', { sensitivity: 'base' });
  });
  return e;
}

function prettySize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function formatListing(session) {
  const all = session.items;
  if (!all.length) return null;

  const total = Math.ceil(all.length / PAGE_SIZE);
  if (session.page >= total) session.page = total - 1;

  const start = session.page * PAGE_SIZE;
  const page = all.slice(start, start + PAGE_SIZE);
  const rel = path.relative(ROOT, session.cwd) || '.';

  let out = `📁 \`files/${rel}/\`  —  pág ${session.page + 1}/${total}\n`;
  out += `─────────────────\n`;

  for (let i = 0; i < page.length; i++) {
    const idx = start + i + 1;
    const e = page[i];
    if (e.isDir) {
      out += `[${idx}] 📂 ${e.name}/\n`;
    } else {
      let size = '';
      try { size = ` (${prettySize(fs.statSync(e.path).size)})`; } catch (_) {}
      out += `[${idx}] 📄 ${e.name}${size}\n`;
    }
  }

  out += `\nNavegar: \`.f N\`  \`.f back\`  \`.f root\``;
  if (total > 1) out += `  •  pág: \`.f n\` / \`.f p\``;
  out += `\nArchivos: \`.f get N\`  \`.f up\`  \`.f rm N\`  \`.f mv A B\``;
  out += `\nOtros: \`.f find q\`  \`.f mkdir nom\`  \`.f help\``;
  return out;
}

// ─── Enviar o editar el mensaje del explorador ──────────────────────────────
async function render(sock, jid, session) {
  session.items = sortedEntries(session.cwd);
  const out = formatListing(session);
  const text = out || '📂 Carpeta vacía.';

  if (session.msgKey) {
    try {
      await sock.sendMessage(jid, { text, edit: session.msgKey });
      return;
    } catch (_) {
      session.msgKey = null; // el mensaje ya no existe, enviar nuevo
    }
  }

  const sent = await sock.sendMessage(jid, { text });
  session.msgKey = sent?.key;
}

// ─── Comandos ────────────────────────────────────────────────────────────────
const commands = ['files', 'f'];

async function handler(sock, msg, args) {
  const jid = msg.key.remoteJid;
  const isOwner = msg.key.fromMe;

  if (!isOwner) {
    await sock.sendMessage(jid, { react: { text: '🚫', key: msg.key } });
    return;
  }

  const ses = getSession(jid);
  const sub = args[0];

  // ── Sin args → listar ────────────────────────────────────────────────────
  if (!sub) {
    await render(sock, jid, ses);
    return;
  }

  // ── Navegación: <N> / cd <N> / back / root ───────────────────────────────
  if (sub === 'cd' || /^\d+$/.test(sub)) {
    const idx = parseInt(sub === 'cd' ? args[1] : sub, 10) - 1;
    const target = ses.items[idx];
    if (!target || !target.isDir) {
      await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
      return;
    }
    if (!insideRoot(target.path)) {
      await sock.sendMessage(jid, { react: { text: '🚫', key: msg.key } });
      return;
    }
    ses.cwd = target.path;
    ses.page = 0;
    await render(sock, jid, ses);
    return;
  }

  if (sub === 'back') {
    const parent = path.dirname(ses.cwd);
    if (!insideRoot(parent) || parent === ses.cwd) {
      await render(sock, jid, ses);
      return;
    }
    ses.cwd = parent;
    ses.page = 0;
    await render(sock, jid, ses);
    return;
  }

  if (sub === 'root') {
    ses.cwd = ROOT;
    ses.page = 0;
    await render(sock, jid, ses);
    return;
  }

  // ── Paginación ───────────────────────────────────────────────────────────
  if (sub === 'n' || sub === 'next') {
    const total = Math.ceil(ses.items.length / PAGE_SIZE);
    if (ses.page + 1 < total) ses.page++;
    await render(sock, jid, ses);
    return;
  }

  if (sub === 'p' || sub === 'prev') {
    if (ses.page > 0) ses.page--;
    await render(sock, jid, ses);
    return;
  }

  // ── Descargar: get <idx1> [idx2...] ──────────────────────────────────────
  if (sub === 'get') {
    const indices = args.slice(1).map(x => parseInt(x, 10) - 1);
    for (const idx of indices) {
      const target = ses.items[idx];
      if (!target || target.isDir) {
        await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
        continue;
      }
      try {
        await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });
        const buffer = fs.readFileSync(target.path);
        await sock.sendMessage(jid, {
          document: buffer,
          fileName: target.name,
          mimetype: 'application/octet-stream'
        });
      } catch (e) {
        console.error(`❌ [files] Error enviando ${target.name}:`, e.message);
        await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
      }
    }
    return;
  }

  // ── Subir archivo: up (respondiendo a un multimedia) ──────────────────────
  if (sub === 'up') {
    const ctx = msg.message?.extendedTextMessage?.contextInfo;
    const targetMsg = ctx?.quotedMessage || msg.message;

    const mediaTypes = ['documentMessage', 'imageMessage', 'videoMessage', 'audioMessage', 'stickerMessage'];
    const mediaKey = mediaTypes.find(k => targetMsg[k]);
    if (!mediaKey) {
      await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
      return;
    }

    try {
      await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

      const stream = await downloadContentFromMessage(targetMsg[mediaKey], mediaKey.replace('Message', ''));
      let buf = Buffer.from([]);
      for await (const chunk of stream) buf = Buffer.concat([buf, chunk]);

      let fname = targetMsg[mediaKey]?.fileName || `file_${Date.now()}`;
      if (!path.extname(fname)) {
        const mt = targetMsg[mediaKey]?.mimetype || '';
        const ext = mt.split('/')[1]?.split(';')[0] || 'bin';
        fname += `.${ext}`;
      }

      // Garantizar que el directorio actual exista
      fs.mkdirSync(ses.cwd, { recursive: true });

      const dest = path.join(ses.cwd, fname);
      if (fs.existsSync(dest)) {
        await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
        return;
      }

      fs.writeFileSync(dest, buf);
      await render(sock, jid, ses); // auto-actualizar explorador
    } catch (e) {
      console.error(`❌ [files] Error subiendo:`, e.message);
      await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
    }
    return;
  }

  // ── Eliminar: rm <idx1> [idx2...] ────────────────────────────────────────
  if (sub === 'rm') {
    let deleted = [];
    for (const raw of args.slice(1)) {
      const idx = parseInt(raw, 10) - 1;
      const target = ses.items[idx];
      if (!target) continue;
      try {
        if (target.isDir) {
          fs.rmSync(target.path, { recursive: true, force: true });
        } else {
          fs.unlinkSync(target.path);
        }
        deleted.push(target.name);
      } catch (e) {
        console.error(`❌ [files] Error eliminando ${target.name}:`, e.message);
      }
    }
    if (!deleted.length) {
      await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
      return;
    }
    // Auto-actualizar explorador
    await sock.sendMessage(jid, { react: { text: '🗑️', key: msg.key } });
    await render(sock, jid, ses);
    return;
  }

  // ── Mover: mv <idx> <destIdx> ────────────────────────────────────────────
  if (sub === 'mv') {
    const srcIdx = parseInt(args[1], 10) - 1;
    const destIdx = parseInt(args[2], 10) - 1;
    const src = ses.items[srcIdx];
    const dest = ses.items[destIdx];

    if (!src || !dest || !dest.isDir) {
      await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
      return;
    }

    try {
      const newPath = path.join(dest.path, src.name);
      if (fs.existsSync(newPath)) {
        await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
        return;
      }
      fs.renameSync(src.path, newPath);
      await render(sock, jid, ses); // auto-actualizar explorador
    } catch (e) {
      console.error(`❌ [files] Error moviendo:`, e.message);
      await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
    }
    return;
  }

  // ── Buscar: find <palabra> ───────────────────────────────────────────────
  if (sub === 'find') {
    const q = args.slice(1).join(' ').toLowerCase();
    if (!q) {
      await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
      return;
    }
    const all = sortedEntries(ses.cwd);
    const filtered = all.filter(e => e.name.toLowerCase().includes(q));
    if (!filtered.length) {
      await sock.sendMessage(jid, { text: `🔍 Sin resultados para "${q}".` });
      return;
    }
    ses.items = filtered;
    ses.page = 0;
    await render(sock, jid, ses);
    return;
  }

  // ── Crear carpeta: mkdir <nombre> ────────────────────────────────────────
  if (sub === 'mkdir') {
    const name = args.slice(1).join(' ');
    if (!name) {
      await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
      return;
    }
    const dirPath = path.join(ses.cwd, name);
    if (fs.existsSync(dirPath)) {
      await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
      return;
    }
    try {
      fs.mkdirSync(dirPath, { recursive: true });
      await render(sock, jid, ses); // auto-actualizar explorador
    } catch (e) {
      console.error(`❌ [files] Error creando carpeta:`, e.message);
      await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
    }
    return;
  }

  // ── Ayuda ────────────────────────────────────────────────────────────────
  if (sub === 'help' || sub === 'h' || sub === '?') {
    const help = `📁 *files — explorador de archivos*
Un solo mensaje se auto-actualiza al navegar/modificar.

*Navegación*
\`.f\`          → listar directorio actual
\`.f <N>\`      → entrar a carpeta [N]
\`.f back\`     → subir un nivel
\`.f root\`     → ir a la raíz \`files/\`
\`.f n\` / \`.f p\` → paginar (next/prev)

*Archivos*
\`.f get <N...>\`   → descargar archivo(s)
\`.f up\`           → subir archivo (responde a un documento)
\`.f rm <N...>\`    → eliminar archivo(s)/carpeta(s)
\`.f mv <A> <B>\`   → mover ítem [A] a carpeta [B]
\`.f find <q>\`     → buscar por nombre
\`.f mkdir <nom>\`  → crear carpeta`;
    await sock.sendMessage(jid, { text: help });
    return;
  }

  // ── Comodín ──────────────────────────────────────────────────────────────
  await render(sock, jid, ses);
}

module.exports = { commands, handler };
