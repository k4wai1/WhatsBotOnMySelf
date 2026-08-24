// addons/definir.js
// .definir <palabra> → definición en español (rae-api.com) con respaldo en inglés (dictionaryapi.dev)

const MAX_SENTIDOS = 4;

function formatearRae(data, palabra) {
    const d = data.data || {};
    let out = `📖 *${d.word || palabra}*\n`;
    if (d.meanings?.[0]?.origin?.raw) out += `\n🌱 _${d.meanings[0].origin.raw}_\n`;

    let count = 0;
    for (const meaning of d.meanings || []) {
        if (!Array.isArray(meaning.senses)) continue;
        for (const sense of meaning.senses) {
            if (!sense.raw || count >= MAX_SENTIDOS) break;
            out += `\n${sense.raw}`;
            count++;
        }
        if (count >= MAX_SENTIDOS) break;
    }
    return count ? out.trim() : null;
}

async function definirEn(palabra) {
    const res = await fetch(`https://api.dictionaryapi.dev/api/v2/entries/en/${encodeURIComponent(palabra)}`, {
        signal: AbortSignal.timeout(15000)
    });
    if (!res.ok) return null;
    const json = await res.json();
    const entry = Array.isArray(json) ? json[0] : null;
    if (!entry) return null;

    let out = `📖 *${entry.word}* 🇬🇧\n`;
    if (entry.phonetic) out += `🔊 _${entry.phonetic}_\n`;

    let count = 0;
    for (const m of entry.meanings || []) {
        out += `\n*${m.partOfSpeech}*\n`;
        for (const def of (m.definitions || []).slice(0, 2)) {
            if (count >= 4) break;
            out += `• ${def.definition}\n`;
            count++;
        }
        if (count >= 4) break;
    }
    return count ? out.trim() : null;
}

module.exports = {
    commands: ['definir', 'def'],

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;
        try {
            let palabra = args.join(' ').trim();
            if (!palabra) {
                palabra = (msg.message?.extendedTextMessage?.contextInfo?.quotedMessage?.conversation || '').trim().split(/\s+/)[0] || '';
            }
            if (!palabra || /\s/.test(palabra)) {
                await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
                await sock.sendMessage(jid, { text: '📚 *Diccionario*\n\nUso: `.definir <palabra>`' }, { quoted: msg });
                return;
            }

            await sock.sendMessage(jid, { react: { text: '🔍', key: msg.key } });

            let resultado = null;
            try {
                const res = await fetch(`https://rae-api.com/api/words/${encodeURIComponent(palabra.toLowerCase())}`, {
                    headers: { 'User-Agent': 'Mozilla/5.0' },
                    signal: AbortSignal.timeout(15000)
                });
                if (res.ok) {
                    const json = await res.json();
                    if (json.ok !== false && json.data) resultado = formatearRae(json, palabra);
                }
            } catch (_) {}

            if (!resultado) resultado = await definirEn(palabra);

            if (!resultado) {
                await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                await sock.sendMessage(jid, { text: `No encontré "${palabra}" en el diccionario.` }, { quoted: msg });
                return;
            }

            await sock.sendMessage(jid, { text: resultado }, { quoted: msg });
            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

        } catch (error) {
            console.error('❌ [definir]:', error.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } }).catch(() => {});
        }
    }
};
