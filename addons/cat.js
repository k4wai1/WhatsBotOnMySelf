// addons/cat.js
// Fotos de gatos aleatorias vía TheCatAPI, 4 usos / 10 min.
// Portado del bot de rupa y adaptado al router de addons.
// Requiere (opcional) CAT_API_KEY en .env — https://thecatapi.com
const CAT_API_KEY = process.env.CAT_API_KEY || '';

function fetchWithTimeout(url, options = {}, timeoutMs = 15000) {
    const controller = new AbortController();
    const id = setTimeout(() => controller.abort(), timeoutMs);
    return fetch(url, { ...options, signal: controller.signal })
        .finally(() => clearTimeout(id));
}

async function fetchJson(url, headers = {}, timeoutMs = 15000) {
    const res = await fetchWithTimeout(url, { headers: { Accept: 'application/json', ...headers } }, timeoutMs);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return res.json();
}

async function fetchBuffer(url, headers = {}, timeoutMs = 20000) {
    const res = await fetchWithTimeout(url, { headers }, timeoutMs);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const arrayBuffer = await res.arrayBuffer();
    return Buffer.from(arrayBuffer);
}

async function getRandomCatImageUrl() {
    const endpoint = 'https://api.thecatapi.com/v1/images/search?limit=1';
    const headers = {};
    if (CAT_API_KEY) headers['x-api-key'] = CAT_API_KEY;
    const data = await fetchJson(endpoint, headers, 15000);
    if (!Array.isArray(data) || !data[0]?.url) throw new Error('Sin URL');
    return data[0].url;
}

const LIMIT = 4;
const WINDOW_MS = 10 * 60 * 1000;
const userRequests = new Map();

module.exports = {
    commands: ['cat'],

    handler: async (sock, msg) => {
        const jid = msg.key.remoteJid;
        const userId = msg.key.participant || jid;
        const count = userRequests.get(userId) || 0;

        if (count >= LIMIT) {
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
            return;
        }
        userRequests.set(userId, count + 1);

        try {
            await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

            const imgUrl = await getRandomCatImageUrl();
            const buffer = await fetchBuffer(imgUrl, {}, 20000);

            await sock.sendMessage(jid, { image: buffer, caption: 'Meow! 🐱' }, { quoted: msg });
            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

        } catch (err) {
            console.error('❌ [cat] Error:', err.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
            const isAbort = /aborted|abort/i.test(err.message || '');
            const replyMsg = isAbort ? 'Se tardó mucho 😿 Intenta de nuevo.' : 'Error obteniendo el michi 😿';
            await sock.sendMessage(jid, { text: replyMsg }, { quoted: msg });
        } finally {
            setTimeout(() => {
                const current = userRequests.get(userId) || 0;
                if (current <= 1) userRequests.delete(userId);
                else userRequests.set(userId, current - 1);
            }, WINDOW_MS);
        }
    }
};
