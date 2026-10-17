// addons/channel.js
// Reporte de tasas BCV + Binance con gráfica: comando .graf y publicación
// automática a un canal/newsletter (NEWS_CHANNEL_ID en .env, opcional).
// Portado del bot de rupa y adaptado al router de addons.
const fs = require('fs');
const path = require('path');
const chartGenerator = require('./chart_generator');

// --- CONFIGURACIÓN ---
const TARGET_CHANNEL_ID = process.env.NEWS_CHANNEL_ID || '';
const STATS_DIR = path.join(__dirname, '..', 'assets', 'statistics');
const STATE_FILE = path.join(STATS_DIR, 'channel_state.json');
const HISTORY_FILE = path.join(STATS_DIR, 'rates_history.json');

if (!fs.existsSync(STATS_DIR)) fs.mkdirSync(STATS_DIR, { recursive: true });

const SCHEDULE_HOURS = [6, 12, 15, 18];

// --- FUNCIONES AUXILIARES ---
function guardarHistorial(bcv, binance) {
    if (!bcv || !binance) return;
    try {
        let history = { daily: [], intraday: [] };
        if (fs.existsSync(HISTORY_FILE)) {
            history = JSON.parse(fs.readFileSync(HISTORY_FILE, 'utf8'));
        }

        const now = new Date();
        const dateStr = now.toISOString().split('T')[0];

        history.intraday.push({
            timestamp: now.toISOString(),
            date: dateStr,
            bcv_usd: bcv,
            binance_usdt: binance
        });

        fs.writeFileSync(HISTORY_FILE, JSON.stringify(history, null, 2));
    } catch (e) {
        console.error('[CHANNEL] Error guardando historial:', e.message);
    }
}

function getLastPostId() {
    try {
        if (!fs.existsSync(STATE_FILE)) return '';
        return JSON.parse(fs.readFileSync(STATE_FILE)).lastId;
    } catch { return ''; }
}

function saveLastPostId(id) {
    fs.writeFileSync(STATE_FILE, JSON.stringify({ lastId: id }));
}

// --- FUNCIÓN MAESTRA ---
async function obtenerMensajeTasas() {
    const prevTLS = process.env.NODE_TLS_REJECT_UNAUTHORIZED;
    try {
        process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0';

        const controller = new AbortController();
        const timer = setTimeout(() => controller.abort(), 15000);
        const res = await fetch('https://www.bcv.org.ve/', { headers: { 'User-Agent': 'Mozilla/5.0' }, signal: controller.signal });
        clearTimeout(timer);
        if (!res.ok) throw new Error('BCV caído');
        const html = await res.text();

        const monedas = { 'dolar': { codigo: 'USD', nombre: 'Dólar' }, 'euro': { codigo: 'EUR', nombre: 'Euro' } };
        const valores = {};
        let bcvUSD = null;

        for (const [id, info] of Object.entries(monedas)) {
            const regex = new RegExp(`id=["']${id}["'][\\s\\S]*?<strong[^>]*>\\s*([\\d.,]+)\\s*<\\/strong>`, 'i');
            const match = html.match(regex);
            if (match && match[1]) {
                const val = match[1].replace(/\s/g, '');
                valores[id] = { valor: val, ...info };
                if (id === 'dolar') bcvUSD = parseFloat(val.replace(',', '.'));
            }
        }

        if (Object.keys(valores).length === 0) return null;

        let precioUSDT = null;
        try {
            const binRes = await fetch("https://p2p.binance.com/bapi/c2c/v2/friendly/c2c/adv/search", {
                method: "POST",
                headers: { "Content-Type": "application/json", "User-Agent": "Mozilla/5.0" },
                body: JSON.stringify({ "fiat": "VES", "page": 1, "rows": 1, "tradeType": "BUY", "asset": "USDT", "payTypes": [], "publisherType": "merchant" })
            });
            const data = await binRes.json();
            if (data.data && data.data.length > 0) precioUSDT = parseFloat(data.data[0].adv.price);
        } catch (e) { console.error('[CHANNEL] Error Binance:', e.message); }

        const fechaMatch = html.match(/Fecha Valor:[^>]*>([^<]+)</);
        let mensaje = `💵 *Actualización del Dólar & Euro*\n📅 ${fechaMatch ? fechaMatch[1].trim() : 'Hoy'}\n\n`;

        for (const key in valores) {
            const dato = valores[key];
            mensaje += `🌍 *${dato.nombre} (${dato.codigo})*\n🏛 BCV: ${dato.valor}\n`;
            if (precioUSDT) {
                const pBCV = parseFloat(dato.valor.replace(',', '.'));
                mensaje += `🔸 Binance (${key === 'euro' ? 'Ref. USDT' : 'USDT'}): ${precioUSDT.toFixed(2)}\n`;
                mensaje += `📊 Brecha: ${(((precioUSDT - pBCV) / pBCV) * 100).toFixed(2)}%\n\n`;
            } else {
                mensaje += `🔸 Binance: No disponible\n\n`;
            }
        }

        return { mensaje, bcvUSD, binanceUSDT: precioUSDT };

    } catch (e) {
        console.error('[CHANNEL-USD] Error:', e.message);
        return null;
    } finally {
        if (prevTLS === undefined) delete process.env.NODE_TLS_REJECT_UNAUTHORIZED;
        else process.env.NODE_TLS_REJECT_UNAUTHORIZED = prevTLS;
    }
}

module.exports = {
    commands: ['graf'],

    // Tarea programada (loop automático para el canal)
    init: (sock) => {
        if (!TARGET_CHANNEL_ID) {
            console.log('[CHANNEL] ℹ️ NEWS_CHANNEL_ID no configurado: auto-publicación desactivada (.graf sigue disponible).');
            return;
        }

        setInterval(async () => {
            const now = new Date();
            const currentHour = now.getHours();

            let targetHour = -1;
            for (let h of SCHEDULE_HOURS) { if (currentHour >= h) targetHour = h; }
            if (targetHour === -1) return;

            const eventId = `${now.toISOString().split('T')[0]}-${targetHour}`;
            if (getLastPostId() !== eventId) {

                let isLate = currentHour > targetHour || (currentHour === targetHour && now.getMinutes() > 20);
                const data = await obtenerMensajeTasas();

                if (data && data.mensaje) {
                    guardarHistorial(data.bcvUSD, data.binanceUSDT); // Solo la tarea programada guarda historial

                    let msjFinal = isLate
                        ? `🙇🏻‍♂️ *Disculpa la demora*, aquí tienes el reporte correspondiente a las ${targetHour}:00.\n\n${data.mensaje}`
                        : data.mensaje;

                    try {
                        if (targetHour === 18) { // A las 18:00 genera y envía la gráfica
                            const chartPath = await chartGenerator.generateChart();
                            if (chartPath) {
                                // 1. Extraer el JID del propio bot
                                const botJid = sock.user.id.split(':')[0] + '@s.whatsapp.net';

                                // 2. Enviar la imagen al chat privado del bot primero
                                console.log('[CHANNEL] 📤 Subiendo imagen al chat del bot como puente...');
                                const msgEnviado = await sock.sendMessage(botJid, {
                                    image: fs.readFileSync(chartPath),
                                    caption: msjFinal
                                });

                                // 3. Reenviar ese mismo mensaje al canal (forward nativo)
                                console.log('[CHANNEL] 🔄 Reenviando imagen al canal...');
                                await sock.sendMessage(TARGET_CHANNEL_ID, { forward: msgEnviado });
                            } else {
                                await sock.sendMessage(TARGET_CHANNEL_ID, { text: msjFinal });
                            }
                        } else {
                            await sock.sendMessage(TARGET_CHANNEL_ID, { text: msjFinal });
                        }

                        saveLastPostId(eventId); // Registra que ya se envió
                        console.log(`[CHANNEL] ✅ Enviado exitosamente al canal: ${eventId}`);
                    } catch (err) {
                        console.error('[CHANNEL] ❌ Error enviando mensaje:', err.message);
                    }
                }
            }
        }, 60 * 1000);
    },

    // Comando a demanda (.graf)
    handler: async (sock, msg) => {
        const jid = msg.key.remoteJid;

        try {
            console.log(`[CHANNEL] 📊 Generando reporte a demanda (.graf) para ${jid}`);
            await sock.sendMessage(jid, { react: { text: '📊', key: msg.key } });

            const data = await obtenerMensajeTasas();

            if (data && data.mensaje) {
                const chartPath = await chartGenerator.generateChart();

                if (chartPath) {
                    await sock.sendMessage(jid, {
                        image: fs.readFileSync(chartPath),
                        caption: data.mensaje
                    }, { quoted: msg });
                } else {
                    await sock.sendMessage(jid, { text: data.mensaje }, { quoted: msg });
                }
            } else {
                await sock.sendMessage(jid, { text: "❌ Error: No pude obtener las tasas en este momento." }, { quoted: msg });
            }

        } catch (err) {
            console.error('❌ [channel] Error en comando graf:', err.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
        }
    }
};
