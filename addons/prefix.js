// addons/prefix.js
const fs = require('fs');
const path = require('path');

const CONFIG_PATH = path.join(__dirname, '..', 'assets', 'config.json');

function loadPrefixes() {
    try {
        const raw = fs.readFileSync(CONFIG_PATH, 'utf-8');
        const cfg = JSON.parse(raw);
        if (Array.isArray(cfg.prefixes) && cfg.prefixes.length) return cfg.prefixes;
    } catch (_) {}
    return [',', '!', '/'];
}

function savePrefixes(list) {
    fs.writeFileSync(CONFIG_PATH, JSON.stringify({ prefixes: list }, null, 2));
}

const commands = ['prefix', 'setprefix'];

async function handler(sock, msg, args) {
    const jid = msg.key.remoteJid;
    const isOwner = msg.key.fromMe;

    // Solo el host puede gestionar prefixes
    if (!isOwner) {
        await sock.sendMessage(jid, { react: { text: '🚫', key: msg.key } });
        return;
    }

    const prefixes = loadPrefixes();
    const sub = args[0];

    // ── prefix (sin args) → mostrar lista ───────────────────────────────────
    if (!sub) {
        const pretty = prefixes.map(p => `\`${p}\``).join('  ');
        await sock.sendMessage(jid, { text: `📌 *Prefixes activos:*\n${pretty}\n\nTotal: ${prefixes.length}` });
        return;
    }

    // ── prefix add <p> ──────────────────────────────────────────────────────
    if (sub === 'add') {
        const p = args[1];
        if (!p || p.length > 3) {
            await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
            return;
        }
        if (prefixes.includes(p)) {
            await sock.sendMessage(jid, { text: `⚠️ \`${p}\` ya existe.` });
            return;
        }
        prefixes.push(p);
        savePrefixes(prefixes);
        await sock.sendMessage(jid, { text: `✅ \`${p}\` añadido.` });
        return;
    }

    // ── prefix remove <p> / rm <p> ─────────────────────────────────────────
    if (sub === 'remove' || sub === 'rm') {
        const p = args[1];
        if (!p) {
            await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
            return;
        }
        const idx = prefixes.indexOf(p);
        if (idx === -1) {
            await sock.sendMessage(jid, { text: `❓ \`${p}\` no está en la lista.` });
            return;
        }
        if (prefixes.length <= 1) {
            await sock.sendMessage(jid, { text: '⚠️ Debe haber al menos 1 prefix.' });
            return;
        }
        prefixes.splice(idx, 1);
        savePrefixes(prefixes);
        await sock.sendMessage(jid, { text: `🗑️ \`${p}\` eliminado.` });
        return;
    }

    // ── prefix default ──────────────────────────────────────────────────────
    if (sub === 'default') {
        savePrefixes([',', '!', '/']);
        await sock.sendMessage(jid, { text: '🔄 Prefixes restaurados: `,` `!` `/`' });
        return;
    }

    // ── Comodín ─────────────────────────────────────────────────────────────
    const help = `📌 *Comandos de prefix*
─────────────
\`prefix\`              → ver lista actual
\`prefix add <p>\`      → añadir prefix
\`prefix remove <p>\`   → eliminar prefix
\`prefix rm <p>\`       → eliminar prefix (alias)
\`prefix default\`      → restaurar valores por defecto`;
    await sock.sendMessage(jid, { text: help });
}

module.exports = { commands, handler };
