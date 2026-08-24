// addons/help.js
const fs = require('fs-extra');
const path = require('path');
const crypto = require('crypto');

const CACHE_PATH = path.join(__dirname, '..', 'help_cache.json');
let helpData = null;          // { groups: [], aliasMap: {}, addonsHash }
let regenerating = null;      // promesa en curso (evita doble regeneración)

// ─── Hash del estado de los addons ──────────────────────────────
function computeAddonsHash() {
    const files = fs.readdirSync(__dirname)
        .filter(f => f.endsWith('.js') && f !== 'help.js')
        .sort();
    const hash = crypto.createHash('md5');
    for (const f of files) {
        const stat = fs.statSync(path.join(__dirname, f));
        hash.update(`${f}:${stat.mtimeMs}:${stat.size}`);
    }
    return hash.digest('hex');
}

// ─── Carga / generación del caché ───────────────────────────────
async function loadCache() {
    if (helpData) return helpData;
    if (await fs.pathExists(CACHE_PATH)) {
        helpData = await fs.readJson(CACHE_PATH);
        return helpData;
    }
    return null;
}

async function generateCache(force = false) {
    if (helpData && !force) return helpData;
    if (!process.env.DEEPSEEK_API_KEY) {
        throw new Error('DEEPSEEK_API_KEY no está configurada en .env');
    }

    const addonsHash = computeAddonsHash();
    if (helpData && helpData.addonsHash === addonsHash && !force) return helpData;

    const addonsDir = __dirname;
    const files = fs.readdirSync(addonsDir).filter(f => f.endsWith('.js') && f !== 'help.js');
    const groups = [];

    for (const file of files) {
        try {
            const code = await fs.readFile(path.join(addonsDir, file), 'utf8');
            const response = await fetch('https://api.deepseek.com/v1/chat/completions', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'Authorization': `Bearer ${process.env.DEEPSEEK_API_KEY}`
                },
                body: JSON.stringify({
                    model: 'deepseek-chat',
                    messages: [
                        {
                            role: 'system',
                            content: `Eres un asistente que analiza addons para un bot de WhatsApp. Agrupa los comandos que realizan la misma función (alias). Devuelve ÚNICAMENTE un array JSON de objetos con la estructura:
[
  {
    "commands": [".c", ".chat"],
    "preview": "Descripción corta (1 línea, emoji permitido).",
    "technical": "Explicación técnica detallada (uso, opciones, comportamiento)."
  },
  ...
]
Si un addon no tiene comandos, responde []. No incluyas bloques de markdown.`
                        },
                        {
                            role: 'user',
                            content: `Código del addon:\n\n${code}`
                        }
                    ],
                    temperature: 0.2
                })
            });

            const data = await response.json();
            let content = data.choices[0].message.content;
            content = content.replace(/```json\s*|```\s*/g, '').trim();
            const arr = JSON.parse(content);
            groups.push(...arr);
        } catch (err) {
            console.error(`❌ Error generando ayuda para ${file}: ${err.message}`);
        }
    }

    // Construir aliasMap: cada comando apunta a su grupo
    const aliasMap = {};
    for (const group of groups) {
        for (const cmd of group.commands) {
            // normalizar: sin punto inicial, minúsculas
            const key = cmd.replace(/^\./, '').toLowerCase();
            aliasMap[key] = group;
            // también con punto, por si buscan ".chat"
            aliasMap['.' + key] = group;
        }
    }

    helpData = { generatedAt: Date.now(), addonsHash, groups, aliasMap };
    await fs.writeJson(CACHE_PATH, helpData);
    return helpData;
}

// Regenera en segundo plano si los addons cambiaron desde la última vez
function regenerateIfStale() {
    if (regenerating) return regenerating;
    if (!process.env.DEEPSEEK_API_KEY) return null;
    try {
        const current = computeAddonsHash();
        if (helpData && helpData.addonsHash === current) return null;
    } catch (_) {}
    console.log('📝 Cambios detectados en addons — regenerando caché de ayuda...');
    regenerating = generateCache(true)
        .then(() => console.log('✅ Caché de ayuda actualizado.'))
        .catch(e => console.error('❌ Error regenerando ayuda:', e.message))
        .finally(() => { regenerating = null; });
    return regenerating;
}

// ─── Handler principal ──────────────────────────────────────────
module.exports = {
    commands: ['help', 'ayuda'],

    init: async (sock, store) => {
        const exists = await fs.pathExists(CACHE_PATH);
        if (!exists) {
            console.log('📝 Generando caché de ayuda con DeepSeek...');
            generateCache()
                .then(() => console.log('✅ Caché de ayuda creada.'))
                .catch(e => console.error('❌ Error generando ayuda:', e.message));
        } else {
            await loadCache();
            regenerateIfStale();
        }
    },

    handler: async (sock, msg, args, store) => {
        const jid = msg.key.remoteJid;
        const subCmd = args[0]?.toLowerCase();

        // ─── Actualización forzada ──────────────────────────
        if (subCmd === 'update' || subCmd === '--update' || subCmd === '-u') {
            await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });
            try {
                await fs.remove(CACHE_PATH).catch(() => {});
                helpData = null;
                await generateCache(true);
                await sock.sendMessage(jid, { text: '✅ Ayuda actualizada con éxito.' }, { quoted: msg });
                await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
            } catch (e) {
                console.error(e);
                await sock.sendMessage(jid, { text: '❌ Falló la actualización.' }, { quoted: msg });
                await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
            }
            return;
        }

        // Asegurar caché
        if (!helpData) {
            helpData = await loadCache();
            if (!helpData) {
                await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });
                try {
                    helpData = await generateCache();
                } catch {
                    return sock.sendMessage(jid, { text: '❌ Error generando ayuda. Intenta más tarde.' });
                }
            }
        }

        // Si hay una regeneración en curso (addons cambiados), esperar para responder al día
        if (regenerating) await regenerating.catch(() => {});

        const commandQuery = args[0]?.toLowerCase();

        // Sin argumentos → menú con grupos (orden alfabético)
        if (!commandQuery) {
            const grupos = [...helpData.groups].sort((a, b) =>
                String(a.commands?.[0] || '').localeCompare(String(b.commands?.[0] || ''), 'es'));
            const lines = grupos.map(group => {
                const cmdList = group.commands.map(c => `*${c}*`).join('/');
                return `• ${cmdList}: ${group.preview}`;
            });
            const menu = `📚 *Menú de Comandos* (${lines.length})\n\n${lines.join('\n')}\n\n💡 Usa *.help <comando>* para detalles.\n🔄 Usa *.help update* para regenerar la ayuda.`;
            await sock.sendMessage(jid, { text: menu }, { quoted: msg });
            await sock.sendMessage(jid, { react: { text: '📚', key: msg.key } });
            return;
        }

        // Con argumento → detalle del grupo
        let searchKey = commandQuery.startsWith('.') ? commandQuery : `.${commandQuery}`;
        let group = helpData.aliasMap[searchKey];

        // fallback sin punto
        if (!group) {
            const fallback = commandQuery.startsWith('.') ? commandQuery.slice(1) : commandQuery;
            group = helpData.aliasMap[fallback];
        }

        if (!group) {
            await sock.sendMessage(jid, { text: `❓ No encontré ayuda para "${commandQuery}".` });
            await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
            return;
        }

        const aliases = group.commands.join(', ');
        const detail = `⚙️ *${aliases}*\n${group.technical}`;
        await sock.sendMessage(jid, { text: detail }, { quoted: msg });
        await sock.sendMessage(jid, { react: { text: 'ℹ️', key: msg.key } });
    }
};
