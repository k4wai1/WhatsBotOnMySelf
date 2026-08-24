// addons/paste.js
// .paste <texto>  |  responde a un mensaje con .paste → sube el texto a paste.rs y devuelve el link

const MAX_CHARS = 100_000;

async function subirAPasteRs(texto) {
    const res = await fetch('https://paste.rs', {
        method: 'POST',
        body: texto,
        headers: { 'User-Agent': 'Mozilla/5.0' },
        signal: AbortSignal.timeout(20000)
    });
    if (!res.ok) throw new Error(`paste.rs HTTP ${res.status}`);
    const url = (await res.text()).trim();
    if (!/^https?:\/\//.test(url)) throw new Error('Respuesta inesperada de paste.rs');
    return url;
}

async function subirA0x0(texto) {
    const form = new FormData();
    form.append('file', new Blob([texto], { type: 'text/plain' }), 'paste.txt');
    const res = await fetch('https://0x0.st', {
        method: 'POST',
        body: form,
        headers: { 'User-Agent': 'Mozilla/5.0' },
        signal: AbortSignal.timeout(20000)
    });
    if (!res.ok) throw new Error(`0x0.st HTTP ${res.status}`);
    const url = (await res.text()).trim();
    if (!/^https?:\/\//.test(url)) throw new Error('Respuesta inesperada de 0x0.st');
    return url;
}

module.exports = {
    commands: ['paste'],

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;
        try {
            const quotedText = msg.message?.extendedTextMessage?.contextInfo?.quotedMessage?.conversation || '';
            const text = quotedText.trim() || args.join(' ').trim();

            if (!text) {
                await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
                await sock.sendMessage(jid, { text: '📤 *Paste*\n\n• `.paste <texto>`\n• Responde a un mensaje con `.paste`' }, { quoted: msg });
                return;
            }
            if (text.length > MAX_CHARS) {
                await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                await sock.sendMessage(jid, { text: `Máximo ${MAX_CHARS} caracteres.` }, { quoted: msg });
                return;
            }

            await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

            let url;
            try {
                url = await subirAPasteRs(text);
            } catch (e1) {
                console.error('⚠️ [paste] respaldo a 0x0.st:', e1.message);
                url = await subirA0x0(text);
            }

            await sock.sendMessage(jid, { text: `📤 ${url}` }, { quoted: msg });
            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

        } catch (error) {
            console.error('❌ [paste]:', error.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } }).catch(() => {});
        }
    }
};
