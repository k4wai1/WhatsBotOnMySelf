// addons/wallpaper.js
// .wallpaper <query> → busca en wallhaven.cc y envía un wallpaper aleatorio

const sharp = require('sharp');

const MAX_BYTES = 25 * 1024 * 1024;

async function fetchImageBuffer(url) {
    const res = await fetch(url, {
        headers: { 'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)' },
        signal: AbortSignal.timeout(30000)
    });
    if (!res.ok) throw new Error(`HTTP ${res.status} al descargar la imagen`);
    const buf = Buffer.from(await res.arrayBuffer());
    return buf.length && buf.length <= MAX_BYTES ? buf : null;
}

module.exports = {
    commands: ['wallpaper', 'wp'],

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;
        try {
            const query = args.join(' ').trim();
            await sock.sendMessage(jid, { react: { text: '🔍', key: msg.key } });

            const url = new URL('https://wallhaven.cc/api/v1/search');
            if (query) url.searchParams.set('q', query);
            url.searchParams.set('page', String(1 + Math.floor(Math.random() * 5)));

            const res = await fetch(url, {
                headers: { 'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)' },
                signal: AbortSignal.timeout(15000)
            });
            if (!res.ok) throw new Error(`HTTP ${res.status}`);
            const json = await res.json();

            const items = (json.data || []).filter(i => i.path);
            if (!items.length) {
                await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                await sock.sendMessage(jid, { text: 'Sin resultados para esa búsqueda.' }, { quoted: msg });
                return;
            }

            const pick = items[Math.floor(Math.random() * items.length)];
            await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

            let buffer = await fetchImageBuffer(pick.path);
            if (!buffer) throw new Error('Imagen vacía o demasiado grande');

            buffer = await sharp(buffer)
                .resize({ width: 1600, height: 1600, fit: 'inside', withoutEnlargement: true })
                .jpeg({ quality: 82 })
                .toBuffer();

            const caption = [
                `🖼️ *${pick.resolution}* · ${Math.round((pick.file_size || 0) / 1024 / 102.4) / 10} MB`,
                pick.source ? `🔗 ${pick.url}` : ''
            ].filter(Boolean).join('\n');

            await sock.sendMessage(jid, { image: buffer, caption }, { quoted: msg });
            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

        } catch (error) {
            console.error('❌ [wallpaper]:', error.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } }).catch(() => {});
        }
    }
};
