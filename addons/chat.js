// addons/chat.js
// v4.1 — Agente hipervitaminado: contexto x3, caché de citados, truncado 2k,
// rate-limit con cola por usuario, Obscura MCP (nav/capturas), Open-Meteo,
// documentos (PDF/DOCX/XLSX/PPTX), edición de imágenes con sharp y gráficos múltiples.

const { downloadContentFromMessage } = require('@whiskeysockets/baileys');
const unread = require('./unread');

// ─── Owner (exento de rate-limit) ───────────────────────────────────────────
const OWNER_NUMBERS = (process.env.OWNER_NUMBERS || '')
    .split(',')
    .map(n => n.replace(/\D/g, ''))
    .filter(Boolean);

function isHost(msg) {
    if (msg.key.fromMe) return true;
    const sender = (msg.key.participant || msg.key.remoteJid || '')
        .split('@')[0].split(':')[0].replace(/\D/g, '');
    return OWNER_NUMBERS.includes(sender);
}

// ─── Configuración general ──────────────────────────────────────────────────
const HISTORY_LIMIT = 90;                 // antes 30 → x3
const MAX_USER_CHARS = 2000;              // límite por mensaje de usuario en contexto
const QUOTED_CACHE_TTL = 24 * 60 * 60 * 1000; // 24 h
const MAX_ITERATIONS = 5;
const RATE_LIMIT_WINDOW = 15_000;         // 15 s
const RATE_LIMIT_MAX = 3;                 // 3 peticiones por ventana
const QUEUE_MAX = 3;                      // máximo de peticiones en cola por usuario
const QUEUE_RESPONSE_DELAY = 10_000;      // reintentar la cola cada ≤10 s

// ─── Caché de mensajes citados ──────────────────────────────────────────────
// Evita que mensajes ya citados se repitan en el historial y los conserva
// aunque hayan salido fuera de HISTORY_LIMIT.
const quotedCache = new Map(); // key: sender::text → { text, sender, t, expiresAt }

function quotedKey(text, sender) {
    return `${sender || '?'}::${String(text || '').trim()}`;
}

function cacheQuoted(text, sender, t = Date.now()) {
    if (!text || !text.trim()) return;
    const key = quotedKey(text, sender);
    quotedCache.set(key, {
        text: String(text).trim(),
        sender: sender || '?',
        t,
        expiresAt: Date.now() + QUOTED_CACHE_TTL
    });
    // Limpieza oportunista
    if (quotedCache.size > 500) {
        const now = Date.now();
        for (const [k, v] of quotedCache) if (v.expiresAt < now) quotedCache.delete(k);
    }
}

function isQuotedCached(text, sender) {
    const key = quotedKey(text, sender);
    const hit = quotedCache.get(key);
    if (!hit) return false;
    if (hit.expiresAt < Date.now()) { quotedCache.delete(key); return false; }
    return true;
}

// ─── Truncado de mensajes de usuario ────────────────────────────────────────
function truncateUserText(text) {
    const s = String(text || '');
    if (s.length <= MAX_USER_CHARS) return s;
    return `${s.slice(0, MAX_USER_CHARS)}…\n\n[⚠️ Mensaje cortado a ${MAX_USER_CHARS} caracteres para no malgastar tokens de la API. Por seguridad, no se pudo leer el resto del mensaje.]`;
}

// ─── Rate-limit por usuario con cola ────────────────────────────────────────
const userRate = new Map(); // phone → { timestamps: number[], queue: Array }

function getRateState(phone) {
    if (!userRate.has(phone)) userRate.set(phone, { timestamps: [], queue: [] });
    return userRate.get(phone);
}

function cleanTimestamps(state) {
    const now = Date.now();
    state.timestamps = state.timestamps.filter(t => now - t < RATE_LIMIT_WINDOW);
}

function scheduleQueue(phone, sock, store) {
    const state = getRateState(phone);
    if (!state.queue.length) return;

    cleanTimestamps(state);
    const oldest = state.timestamps[0] || Date.now();
    const remaining = RATE_LIMIT_WINDOW - (Date.now() - oldest);
    const delay = Math.max(0, Math.min(remaining + 100, QUEUE_RESPONSE_DELAY));

    setTimeout(async () => {
        const st = getRateState(phone);
        cleanTimestamps(st);
        if (!st.queue.length) return;

        if (st.timestamps.length >= RATE_LIMIT_MAX) {
            scheduleQueue(phone, sock, store); // aún sin hueco: reprogramar
            return;
        }

        const item = st.queue.shift();
        st.timestamps.push(Date.now());
        try {
            await processChatRequest(sock, item.msg, item.args, store);
        } catch (e) {
            console.error('❌ chat: error procesando cola:', e.message);
        }
        if (st.queue.length) scheduleQueue(phone, sock, store);
    }, delay);
}

// ─── Cliente MCP genérico (JSON-RPC sobre Streamable HTTP) ──────────────────
function parseMcpPayload(text) {
    const trimmed = text.trim();
    if (trimmed.startsWith('{')) { try { return JSON.parse(trimmed); } catch { return null; } }
    let payload = null;
    for (const line of text.split('\n')) {
        const l = line.trim();
        if (l.startsWith('data:')) { try { payload = JSON.parse(l.slice(5).trim()); } catch {} }
    }
    return payload;
}

function createMcpClient({ url, token, name }) {
    let sessionId = null;
    let rpcId = 0;

    async function request(method, params, isNotification = false) {
        const headers = {
            'Content-Type': 'application/json',
            'Accept': 'application/json, text/event-stream'
        };
        if (token) headers['Authorization'] = `Bearer ${token}`;
        if (sessionId) headers['mcp-session-id'] = sessionId;

        const body = { jsonrpc: '2.0', method };
        if (!isNotification) body.id = ++rpcId;
        if (params !== undefined) body.params = params;

        const res = await fetch(url, {
            method: 'POST',
            headers,
            body: JSON.stringify(body),
            signal: AbortSignal.timeout(45_000)
        });

        const sid = res.headers.get('mcp-session-id');
        if (sid) sessionId = sid;

        if (isNotification || res.status === 202) return null;

        const payload = parseMcpPayload(await res.text());
        if (!payload) throw new Error(`MCP ${name}: respuesta ilegible`);
        if (payload.error) throw new Error(`MCP ${name}: ${payload.error.message || 'error'}`);
        return payload.result;
    }

    async function ensure(force = false) {
        if (sessionId && !force) return;
        sessionId = null;
        const initRes = await request('initialize', {
            protocolVersion: '2025-03-26',
            capabilities: {},
            clientInfo: { name: 'whatsbot-self', version: '4.1' }
        });
        const sidFromInit = initRes; // algunos servidores devuelven sid solo en headers
        await request('notifications/initialized', {}, true);
        return sidFromInit;
    }

    async function callTool(toolName, args = {}) {
        try {
            await ensure();
            return await request('tools/call', { name: toolName, arguments: args });
        } catch (e) {
            await ensure(true);
            return await request('tools/call', { name: toolName, arguments: args });
        }
    }

    return { request, ensure, callTool };
}

// ─── Obscura MCP ────────────────────────────────────────────────────────────
const obscura = createMcpClient({
    url: process.env.OBSCURA_MCP_URL || 'http://127.0.0.1:3000/mcp',
    token: process.env.OBSCURA_MCP_TOKEN || '',
    name: 'Obscura'
});

async function obscuraNavigate(url, extract = 'markdown') {
    await obscura.callTool('browser_navigate', { url });
    if (extract === 'snapshot') {
        const r = await obscura.callTool('browser_snapshot', { max_chars: 6000 });
        return extractTextContent(r);
    }
    const r = await obscura.callTool('browser_markdown', { max_chars: 8000 });
    return extractTextContent(r)
        || extractTextContent(await obscura.callTool('browser_snapshot', { max_chars: 6000 }));
}

async function obscuraScreenshot(url) {
    await obscura.callTool('browser_navigate', { url });
    const r = await obscura.callTool('browser_screenshot', {});
    return extractImageContent(r);
}

function extractTextContent(result) {
    return (result?.content || [])
        .filter(c => c.type === 'text')
        .map(c => c.text)
        .join('\n')
        .slice(0, 12000);
}

function extractImageContent(result) {
    const img = (result?.content || []).find(c => c.type === 'image');
    if (!img) return null;
    return { buffer: Buffer.from(img.data, 'base64'), mimetype: img.mimeType || 'image/png' };
}

// ─── Clima (Open-Meteo directo; MCP opcional si define WEATHER_MCP_URL) ─────
const weatherMcp = process.env.WEATHER_MCP_URL
    ? createMcpClient({ url: process.env.WEATHER_MCP_URL, name: 'Weather' })
    : null;

const VALENCIA = 'Valencia, Carabobo, Venezuela';
// Coordenadas correctas de Valencia (VE) para no depender del geocoder por defecto
const VALENCIA_SEED = { lat: 10.162, lon: -68.0077, nombre: 'Valencia (Carabobo, Venezuela)' };

const WCODES = {
    0: '☀️ despejado', 1: '🌤 mayormente despejado', 2: '⛅ parcialmente nublado', 3: '☁️ nublado',
    45: '🌫 niebla', 48: '🌫 niebla con escarcha',
    51: '🌦 llovizna ligera', 53: '🌦 llovizna', 55: '🌦 llovizna intensa',
    56: '🌧 llovizna helada', 57: '🌧 llovizna helada intensa',
    61: '🌧 lluvia ligera', 63: '🌧 lluvia', 65: '🌧 lluvia intensa',
    66: '🌧 lluvia helada', 67: '🌧 lluvia helada intensa',
    71: '🌨 nieve ligera', 73: '🌨 nieve', 75: '🌨 nieve intensa', 77: '🌨 granizo fino',
    80: '🌦 chubascos ligeros', 81: '🌦 chubascos', 82: '⛈ chubascos fuertes',
    85: '🌨 chubascos de nieve', 86: '🌨 chubascos de nieve fuertes',
    95: '⛈ tormenta', 96: '⛈ tormenta con granizo', 99: '⛈ tormenta con granizo fuerte'
};

const descWeather = (code) => WCODES[code] || `código ${code}`;

async function geocodeCity(city) {
    const res = await fetch(
        `https://geocoding-api.open-meteo.com/v1/search?name=${encodeURIComponent(city)}&count=1&language=es&format=json`,
        { signal: AbortSignal.timeout(15_000) }
    );
    const data = await res.json();
    const g = (data.results || [])[0];
    if (!g) return null;
    return {
        lat: g.latitude,
        lon: g.longitude,
        nombre: [g.name, g.admin1, g.country].filter(Boolean).join(', ')
    };
}

async function consultarClima(city = VALENCIA, days = 3) {
    let coords = VALENCIA_SEED;
    let nombre = VALENCIA_SEED.nombre;

    if (city && city.trim() && city.trim() !== VALENCIA) {
        const geo = await geocodeCity(city);
        if (geo) { coords = { lat: geo.lat, lon: geo.lon }; nombre = geo.nombre; }
    }

    // Ruta MCP opcional (por si en el futuro reactivan un servidor de clima)
    if (weatherMcp) {
        try {
            const result = await weatherMcp.callTool('get_forecast', {
                location: city, days: Math.min(Math.max(Number(days) || 3, 1), 7)
            });
            const txt = extractTextContent(result);
            if (txt) return txt;
        } catch (e) {
            console.warn('⚠️ chat: clima MCP falló, usando Open-Meteo:', e.message);
        }
    }

    const d = Math.min(Math.max(Number(days) || 3, 1), 7);
    const url = 'https://api.open-meteo.com/v1/forecast'
        + `?latitude=${coords.lat}&longitude=${coords.lon}`
        + '&current_weather=true'
        + '&hourly=temperature_2m,relative_humidity_2m,precipitation_probability'
        + `&daily=temperature_2m_max,temperature_2m_min,weathercode,precipitation_probability_max&sunset=${d > 3 ? 'true' : 'false'}`
        + '&timezone=auto'
        + `&forecast_days=${d}`;
    const res = await fetch(url, { signal: AbortSignal.timeout(20_000) });
    if (!res.ok) throw new Error(`Open-Meteo HTTP ${res.status}`);
    const data = await res.json();

    const cw = data.current_weather || {};
    const daily = data.daily || {};
    const hourly = data.hourly || {};
    const lines = [`🌤️ *Clima en ${nombre}*`, `Ahora: ${Math.round(cw.temperature)} °C · ${descWeather(cw.weathercode)} · viento ${cw.windspeed} km/h`, ''];

    if (daily.time?.length) {
        lines.push('*Pronóstico:*');
        for (let i = 0; i < daily.time.length; i++) {
            const fecha = new Date(daily.time[i] + 'T12:00:00');
            const dia = fecha.toLocaleDateString('es-VE', { weekday: 'short', day: 'numeric', month: 'short' });
            lines.push(`📅 ${dia}: ${Math.round(daily.temperature_2m_min[i])}–${Math.round(daily.temperature_2m_max[i])} °C · ${descWeather(daily.weathercode?.[i])} · lluvia ${daily.precipitation_probability_max?.[i] ?? 0}%`);
        }
    }

    if (hourly.precipitation_probability?.length && (cw.is_day !== undefined)) {
        const nowIdx = (hourly.time || []).findIndex(t => Date.now() - (new Date(t).getTime() + 15 * 60000) < 3600_000);
        if (nowIdx >= 0) {
            lines.push('', `💧 Humedad: ${hourly.relative_humidity_2m?.[nowIdx] ?? '?'}% · lluvia próxima ${hourly.precipitation_probability?.[nowIdx] ?? 0}%`);
        }
    }

    return lines.filter(Boolean).join('\n');
}

// ─── You.com ────────────────────────────────────────────────────────────────
function formatYouResults(results, maxLen = 400) {
    if (!results || results.length === 0) return 'No se encontraron resultados.';
    return results.slice(0, 3).map(r => {
        const snippet = (r.description || (r.snippets && r.snippets[0]) || '').slice(0, maxLen);
        return `📌 ${r.title || 'Web'}\n${snippet}\n🔗 ${r.url || ''}`;
    }).join('\n\n');
}

// ─── Parallel Search MCP ────────────────────────────────────────────────────
const parallelMcp = createMcpClient({
    url: process.env.PARALLEL_MCP_URL || 'https://search.parallel.ai/mcp',
    token: process.env.PARALLEL_API_KEY || '',
    name: 'Parallel'
});

function formatParallelResults(parsed, maxLen = 9000) {
    const results = parsed?.results || [];
    if (!results.length) return 'Sin resultados.';
    const lines = [];
    let total = 0;
    for (const r of results) {
        const excerpt = ((r.excerpts || []).join(' ')).slice(0, 400);
        const block = `📌 ${r.title || 'Web'}${r.publish_date ? ` (${r.publish_date})` : ''}\n${excerpt}\n🔗 ${r.url || ''}`;
        lines.push(block);
        total += block.length;
        if (total > maxLen) break;
    }
    return lines.join('\n\n');
}

async function parallelWebSearch(query) {
    const args = { objective: query, search_queries: [query] };
    const result = await parallelMcp.callTool('web_search', args);
    const raw = (result?.content || []).filter(c => c.type === 'text').map(c => c.text).join('\n');
    try { return formatParallelResults(JSON.parse(raw)); }
    catch { return raw.slice(0, 12000) || 'Sin resultados.'; }
}

// ─── AniList ────────────────────────────────────────────────────────────────
async function fetchAniList(query) {
    const graphqlQuery = `
    query ($search: String) {
      Media (search: $search, type: ANIME) {
        title { romaji english native }
        status episodes averageScore description siteUrl
      }
    }`;
    const response = await fetch('https://graphql.anilist.co', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
        body: JSON.stringify({ query: graphqlQuery, variables: { search: query } })
    });
    if (!response.ok) throw new Error('Error en AniList');
    const data = await response.json();
    return JSON.stringify(data.data.Media);
}

// ─── Descarga de media ──────────────────────────────────────────────────────
async function downloadImageBuffer(imageMsg) {
    const stream = await downloadContentFromMessage(imageMsg, 'image');
    let buf = Buffer.from([]);
    for await (const chunk of stream) buf = Buffer.concat([buf, chunk]);
    return buf;
}

async function downloadDocumentBuffer(docMsg) {
    const stream = await downloadContentFromMessage(docMsg, 'document');
    let buf = Buffer.from([]);
    for await (const chunk of stream) buf = Buffer.concat([buf, chunk]);
    return buf;
}

async function analyzeImage(buffer, mimetype, questionText) {
    const dataUrl = `data:${mimetype};base64,${buffer.toString('base64')}`;
    const res = await fetch('https://api.deepseek.com/chat/completions', {
        method: 'POST',
        headers: {
            'Authorization': `Bearer ${process.env.DEEPSEEK_API_KEY}`,
            'Content-Type': 'application/json'
        },
        body: JSON.stringify({
            model: 'deepseek-v4-flash-vision-exp',
            messages: [
                { role: 'system', content: 'Eres un analista visual preciso. Describe lo esencial de la imagen en función de la consulta del usuario. Si contiene texto, transcríbelo completo.' },
                {
                    role: 'user',
                    content: [
                        { type: 'text', text: questionText },
                        { type: 'image_url', image_url: { url: dataUrl } }
                    ]
                }
            ]
        })
    });
    if (!res.ok) throw new Error(`DeepSeek Vision HTTP ${res.status}`);
    const data = await res.json();
    return data.choices?.[0]?.message?.content || '';
}

// ─── Documentos ─────────────────────────────────────────────────────────────
async function extractDocumentText(buffer, mimetype, filename) {
    const name = (filename || '').toLowerCase();
    const mime = (mimetype || '').toLowerCase();

    try {
        if (mime.includes('pdf') || name.endsWith('.pdf')) {
            const { PDFParse } = require('pdf-parse');
            const parser = new PDFParse({ data: buffer });
            try {
                const data = await parser.getText();
                return data.text || '';
            } finally {
                await parser.destroy();
            }
        }
        if (mime.includes('wordprocessingml') || name.endsWith('.docx')) {
            const mammoth = require('mammoth');
            const result = await mammoth.extractRawText({ buffer });
            return result.value || '';
        }
        if (mime.includes('spreadsheetml') || name.endsWith('.xlsx')) {
            const XLSX = require('xlsx');
            const wb = XLSX.read(buffer, { type: 'buffer' });
            const lines = [];
            for (const sheetName of wb.SheetNames) {
                lines.push(`### Hoja: ${sheetName}`);
                lines.push(XLSX.utils.sheet_to_csv(wb.Sheets[sheetName]));
            }
            return lines.join('\n\n');
        }
        if (mime.includes('presentationml') || name.endsWith('.pptx')
            || mime.includes('officedocument') || name.endsWith('.ppt') || name.endsWith('.doc')) {
            const op = require('officeparser');
            const parsed = await op.parseOffice(buffer);
            const out = await parsed.to('text');
            return (out && typeof out.value === 'string') ? out.value : '';
        }
        if (mime.includes('text') || name.endsWith('.txt') || name.endsWith('.csv')) {
            return buffer.toString('utf-8');
        }
    } catch (e) {
        return `[No se pudo leer el documento (${filename || mimetype}): ${e.message}]`;
    }

    return `[Documento no soportado: ${filename || mimetype}. Formatos: PDF, DOCX, XLSX, PPTX, TXT, CSV.]`;
}

// ─── Manipulación de imágenes con sharp ─────────────────────────────────────
async function manipulateImage(buffer, operacion, params = {}) {
    const sharp = require('sharp');

    switch (String(operacion || '').toLowerCase()) {
        case 'resize':
            return { buffer: await sharp(buffer)
                .resize({ width: params.width || undefined, height: params.height || undefined, fit: params.fit || 'inside' })
                .png().toBuffer(), mimetype: 'image/png' };
        case 'crop':
            return { buffer: await sharp(buffer)
                .extract({
                    left: Math.max(0, params.left || 0),
                    top: Math.max(0, params.top || 0),
                    width: params.width || 256,
                    height: params.height || 256
                })
                .png().toBuffer(), mimetype: 'image/png' };
        case 'rotate':
            return { buffer: await sharp(buffer)
                .rotate(params.angle || 90, { background: params.background || '#00000000' })
                .png().toBuffer(), mimetype: 'image/png' };
        case 'grayscale':
        case 'gray':
            return { buffer: await sharp(buffer).grayscale().png().toBuffer(), mimetype: 'image/png' };
        case 'blur':
            return { buffer: await sharp(buffer).blur(params.sigma || 5).png().toBuffer(), mimetype: 'image/png' };
        case 'sharpen':
            return { buffer: await sharp(buffer).sharpen().png().toBuffer(), mimetype: 'image/png' };
        case 'flip':
            return { buffer: await sharp(buffer).flip().png().toBuffer(), mimetype: 'image/png' };
        case 'flop':
            return { buffer: await sharp(buffer).flop().png().toBuffer(), mimetype: 'image/png' };
        case 'negate':
            return { buffer: await sharp(buffer).negate().png().toBuffer(), mimetype: 'image/png' };
        case 'normalize':
            return { buffer: await sharp(buffer).normalize().png().toBuffer(), mimetype: 'image/png' };
        case 'tint':
            return { buffer: await sharp(buffer).tint(params.color || '#ff0000').png().toBuffer(), mimetype: 'image/png' };
        case 'texto': {
            const safeText = String(params.texto || 'Hola').replace(/[<>&"']/g, '');
            const color = String(params.color || '#ffffff').replace(/[<>&"']/g, '');
            const fontSize = Math.min(120, Math.max(24, Number(params.tamano || 48)));
            const meta = await sharp(buffer).metadata();
            const width = meta.width || 600;
            const svg = `<svg width="${width}" height="${Math.round(fontSize * 1.6)}">`
                + `<text x="50%" y="80%" font-size="${fontSize}" font-family="sans-serif" font-weight="bold" `
                + `fill="${color}" text-anchor="middle" stroke="#00000088" stroke-width="2">${safeText}</text></svg>`;
            return { buffer: await sharp(buffer)
                .composite([{ input: Buffer.from(svg), gravity: params.gravity || 'south' }])
                .png().toBuffer(), mimetype: 'image/png' };
        }
        default:
            throw new Error(`Operación no soportada: ${operacion}. Disponibles: resize, crop, rotate, grayscale, blur, sharpen, flip, flop, negate, normalize, tint, texto`);
    }
}

// ─── Utilidades de contexto ────────────────────────────────────────────────
function resolveName(phone, store) {
    const c = store?.contacts?.[phone + '@s.whatsapp.net'];
    return c?.notify || c?.name || phone;
}

function timeAgo(ts) {
    const mins = Math.max(1, Math.round((Date.now() - ts) / 60000));
    if (mins < 60) return `hace ${mins} min`;
    const hours = Math.round(mins / 60);
    if (hours < 24) return `hace ${hours} h`;
    return `hace ${Math.round(hours / 24)} d`;
}

// ─── Rescate de tool-calls emitidas como texto (formato DSML interno) ──────
const BAR = '[｜|]';
const dsmlInvokeRe = new RegExp(`<${BAR}*\\s*DSML${BAR}*\\s*invoke\\s+name=["']([^"']+)["']([\\s\\S]*?)<\\/${BAR}*\\s*DSML${BAR}*\\s*invoke>`, 'g');
const dsmlParamRe = new RegExp(`<${BAR}*\\s*DSML${BAR}*\\s*parameter\\s+name=["']([^"']+)["'][^>]*>([\\s\\S]*?)<\\/${BAR}*\\s*DSML${BAR}*\\s*parameter>`, 'g');
const dsmlTagRe = new RegExp(`<\\/?${BAR}*\\s*DSML${BAR}*\\s*[^>]*>`, 'g');

function parseDsmlToolCalls(content) {
    const calls = [];
    let m;
    dsmlInvokeRe.lastIndex = 0;
    while ((m = dsmlInvokeRe.exec(content)) !== null) {
        const args = {};
        let p;
        dsmlParamRe.lastIndex = 0;
        while ((p = dsmlParamRe.exec(m[2])) !== null) args[p[1]] = p[2].trim();
        calls.push({
            id: `dsml_${Date.now()}_${calls.length}`,
            type: 'function',
            function: { name: m[1], arguments: JSON.stringify(args) }
        });
    }
    return calls;
}

function sanitizeDsmlText(text) {
    return String(text || '').replace(dsmlTagRe, '').trim();
}

// Normaliza chartConfig correcto o parámetros inventados a un string JSON de Chart.js
function normalizeChartArgs(a = {}) {
    let cfg = a.chartConfig ?? a.chart_config ?? null;
    if (cfg && typeof cfg === 'object') return JSON.stringify(cfg);
    if (typeof cfg === 'string' && cfg.trim().startsWith('{')) {
        try { JSON.parse(cfg); return cfg.trim(); } catch {}
    }
    const data = a.datos ?? a.data ?? a.datasets ?? null;
    if (!data) return null;
    let dataObj = data;
    if (typeof dataObj === 'string') { try { dataObj = JSON.parse(dataObj); } catch { return null; } }
    if (Array.isArray(dataObj)) dataObj = { labels: [], datasets: [{ label: '', data: dataObj }] };
    const type = String(a.tipo || a.type || 'bar').replace(/["'\\]/g, '') || 'bar';
    const chart = { type, data: dataObj };
    const title = a.titulo || a.title;
    if (title) chart.options = { title: { display: true, text: String(title) } };
    return JSON.stringify(chart);
}

// ─── Envío de texto largo ───────────────────────────────────────────────────
async function sendLongText(sock, jid, text, quoted) {
    const MAX = 4000;
    const s = String(text || '').trim();
    if (!s) return;
    if (s.length <= MAX) {
        await sock.sendMessage(jid, { text: s }, { quoted });
        return;
    }
    const parts = [];
    let rest = s;
    while (rest.length > MAX) {
        let cut = rest.lastIndexOf('\n', MAX);
        if (cut < MAX * 0.5) cut = rest.lastIndexOf(' ', MAX);
        if (cut < MAX * 0.5) cut = MAX;
        parts.push(rest.slice(0, cut));
        rest = rest.slice(cut).trimStart();
    }
    if (rest) parts.push(rest);
    for (const part of parts) {
        await sock.sendMessage(jid, { text: part }, { quoted });
    }
}

// ═══════════════════════════════════════════════════════════════════════════
// NÚCLEO: procesamiento de una petición de chat
// ═══════════════════════════════════════════════════════════════════════════
async function processChatRequest(sock, msg, args, store) {
    const jid = msg.key.remoteJid;
    const contextInfo = msg.message?.extendedTextMessage?.contextInfo;
    const quotedMsg = contextInfo?.quotedMessage;

    // Texto del comando / caption / documento
    let userPrompt = args.join(' ').trim()
        || quotedMsg?.imageMessage?.caption
        || quotedMsg?.documentMessage?.caption
        || quotedMsg?.videoMessage?.caption
        || msg.message?.imageMessage?.caption
        || msg.message?.documentMessage?.caption
        || msg.message?.videoMessage?.caption
        || '';

    const imageMsg = msg.message?.imageMessage || quotedMsg?.imageMessage || null;
    const docMsg = msg.message?.documentMessage || quotedMsg?.documentMessage || null;
    const isImageQuoted = !msg.message?.imageMessage && !!quotedMsg?.imageMessage;

    if (!userPrompt && !imageMsg && !docMsg) {
        await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
        return;
    }

    try {
        await sock.sendMessage(jid, { react: { text: '🧠', key: msg.key } });

        const requesterPhone = (msg.key.participant || jid).split('@')[0].split(':')[0];
        const requesterName = resolveName(requesterPhone, store);

        // 1. Historial reciente, sin duplicar citados cacheados ni el mensaje actual
        const rawHistory = unread.getRecent(jid, HISTORY_LIMIT);
        const history = rawHistory.filter(m => {
            const senderNorm = String(m.sender || '').split(':')[0];
            if (isQuotedCached(m.text, senderNorm)) return false;
            // Excluir el mensaje actual si unread ya lo indexó
            if (senderNorm === requesterPhone && m.text === userPrompt &&
                Math.abs(Date.now() - m.t) < 5000) return false;
            return true;
        });

        const histLines = history.map(m =>
            `<msg de="${escXml(resolveName(String(m.sender).split(':')[0], store))}" num="${escXml(String(m.sender).split(':')[0])}" cuando="${timeAgo(m.t)}">${escXml(truncateUserText(m.text))}</msg>`
        ).join('\n');

        // 2. Mensaje citado (texto) → cachear y truncar
        const quotedText = quotedMsg?.conversation
            || quotedMsg?.extendedTextMessage?.text
            || quotedMsg?.imageMessage?.caption
            || quotedMsg?.documentMessage?.caption
            || quotedMsg?.videoMessage?.caption
            || '';
        if (quotedText.trim()) {
            const senderNorm = String(contextInfo?.participant || requesterPhone).split('@')[0].split(':')[0];
            cacheQuoted(quotedText, senderNorm);
        }

        // 3. Documento adjunto/citado → extraer texto
        let documentText = '';
        if (docMsg) {
            await sock.sendMessage(jid, { react: { text: '📄', key: msg.key } });
            try {
                const docBuffer = await downloadDocumentBuffer(docMsg);
                documentText = await extractDocumentText(docBuffer, docMsg.mimetype, docMsg.fileName);
                if (documentText.length > 4000) {
                    documentText = documentText.slice(0, 4000) +
                        '\n\n[📄 Documento truncado a 4000 caracteres para no malgastar tokens.]';
                }
            } catch (e) {
                documentText = `[No se pudo procesar el documento: ${e.message}]`;
            }
        }

        // 4. Imagen → buffer + análisis visual previo
        let imageBuffer = null;
        let imageMimetype = 'image/jpeg';
        let imageAnalysis = '';
        if (imageMsg) {
            await sock.sendMessage(jid, { react: { text: '📡', key: msg.key } });
            try {
                imageBuffer = await downloadImageBuffer(imageMsg);
                imageMimetype = imageMsg.mimetype || 'image/jpeg';
                if (!imageBuffer || imageBuffer.length === 0) throw new Error('imagen vacía/expirada');
                const visionQuestion =
                    `Consulta del usuario "${requesterName}": ${userPrompt || 'Describe esta imagen.'}` +
                    (quotedText ? `\nContexto del citado: ${quotedText.slice(0, 500)}` : '');
                imageAnalysis = await analyzeImage(imageBuffer, imageMimetype, visionQuestion);
            } catch (e) {
                console.warn('⚠️ chat: visión falló:', e.message);
                imageAnalysis = `(No se pudo analizar la imagen: ${e.message})`;
            }
        }

        // 5. Contexto XML
        const contextXmlParts = [];
        if (histLines) {
            contextXmlParts.push(
                `<historial_chat ultimos="${history.length}" nota="Solo contexto. El autor relevante es quien firma <peticion_actual>.">\n${histLines}\n</historial_chat>`
            );
        }
        if (quotedText.trim()) {
            const quotedAuthorPhone = String(contextInfo?.participant || requesterPhone).split('@')[0].split(':')[0];
            contextXmlParts.push(
                `<mensaje_citado de="${escXml(resolveName(quotedAuthorPhone, store))}" num="${escXml(quotedAuthorPhone)}">\n${escXml(truncateUserText(quotedText))}\n</mensaje_citado>`
            );
        }
        if (documentText) {
            contextXmlParts.push(
                `<documento_adjunto nombre="${escXml(docMsg?.fileName || 'documento')}" tipo="${escXml(docMsg?.mimetype || '')}">\n${escXml(documentText)}\n</documento_adjunto>`
            );
        }
        if (imageAnalysis) {
            contextXmlParts.push(
                `<analisis_imagen fuente="${isImageQuoted ? 'citada' : 'adjunta'}">\n${escXml(imageAnalysis.slice(0, 4000))}\n</analisis_imagen>`
            );
        }
        contextXmlParts.push(
            `<peticion_actual de="${escXml(requesterName)}" num="${escXml(requesterPhone)}">${escXml(truncateUserText(userPrompt || 'Analiza el contenido y cuéntame qué ves.'))}</peticion_actual>`
        );

        const userContent = `<contexto>\n${contextXmlParts.join('\n')}\n</contexto>`;

        const systemPrompt = `Eres el núcleo de razonamiento de un asistente avanzado de WhatsApp. Eres directo, lógico y amigable. No usas lenguaje robótico.

CONTEXTO ESTRUCTURADO:
Recibirás un bloque <contexto> con XML. Dentro puede haber:
- <historial_chat>: últimos mensajes reales del chat (solo contexto). Los mensajes están truncados a ${MAX_USER_CHARS} caracteres por seguridad y ahorro de tokens.
- <mensaje_citado>: mensaje específico citado al invocarte.
- <documento_adjunto>: texto extraído de un PDF/Word/Excel/PPT.
- <analisis_imagen>: descripción visual de una imagen.
- <peticion_actual>: LA CONSULTA A RESPONDER. Tiene prioridad absoluta.
El único autor relevante es quien firma <peticion_actual>.

HERRAMIENTAS:
- buscar_en_internet (You.com)
- buscar_web_parallel (Parallel Search MCP)
- buscar_anime (AniList)
- navegar_web (Obscura MCP): navega una URL y devuelve markdown/texto legible (JS incluido).
- captura_web (Obscura MCP): navega una URL y envía una captura de pantalla al chat.
- consultar_clima: clima actual y pronóstico vía Open-Meteo. Por defecto Valencia, Carabobo, Venezuela.
- generar_grafico (QuickChart): gráficos Chart.js. 'chartConfig' (string JSON) o 'charts' (array de hasta 3 strings JSON).
- manipular_imagen (sharp): edita la imagen adjunta/citada. Operaciones: resize, crop, rotate, grayscale, blur, sharpen, flip, flop, negate, normalize, tint, texto.

REGLAS CRÍTICAS:
- Usa SOLO function calling nativo. NUNCA escribas llamadas en XML/DSML/texto.
- Para generar_grafico usa 'chartConfig' (JSON string de Chart.js completo) o 'charts' (array). No pegues URLs en la respuesta final.
- Si un mensaje fue truncado y la petición requiere ese contenido, di: "Por seguridad, no se pudo leer el resto del mensaje".
- No inventes datos. Si no sabes, busca o di que no lo sabes.
- Máximo ${MAX_ITERATIONS} rondas de herramientas; si no necesitas datos actuales, responde directo sin herramientas.

FORMATO WHATSAPP:
- *negritas* solo para datos clave.
- Listas con guiones.
- Sé breve; ve directo al grano.`;

        const messages = [
            { role: 'system', content: systemPrompt },
            { role: 'user', content: userContent }
        ];

        const tools = [
            {
                type: 'function',
                function: {
                    name: 'buscar_en_internet',
                    description: 'Busca en internet noticias, definiciones o datos en tiempo real (You.com).',
                    parameters: { type: 'object', properties: { query: { type: 'string' } }, required: ['query'] }
                }
            },
            {
                type: 'function',
                function: {
                    name: 'buscar_web_parallel',
                    description: 'Búsqueda web alternativa vía Parallel Search MCP. Segunda opción ante fallos de You.com.',
                    parameters: { type: 'object', properties: { query: { type: 'string' } }, required: ['query'] }
                }
            },
            {
                type: 'function',
                function: {
                    name: 'buscar_anime',
                    description: 'Datos exactos, sinopsis y puntuación de un anime.',
                    parameters: { type: 'object', properties: { query: { type: 'string' } }, required: ['query'] }
                }
            },
            {
                type: 'function',
                function: {
                    name: 'navegar_web',
                    description: 'Navega una URL con el navegador Obscura y devuelve el contenido legible (markdown/texto). Útil para páginas que requieren JS.',
                    parameters: {
                        type: 'object',
                        properties: {
                            url: { type: 'string' },
                            extract: { type: 'string', enum: ['markdown', 'snapshot'] }
                        },
                        required: ['url']
                    }
                }
            },
            {
                type: 'function',
                function: {
                    name: 'captura_web',
                    description: 'Navega una URL con Obscura y envía una captura de pantalla al chat.',
                    parameters: { type: 'object', properties: { url: { type: 'string' } }, required: ['url'] }
                }
            },
            {
                type: 'function',
                function: {
                    name: 'consultar_clima',
                    description: 'Clima actual y pronóstico (Open-Meteo). Por defecto Valencia, Carabobo, Venezuela.',
                    parameters: {
                        type: 'object',
                        properties: {
                            ciudad: { type: 'string', description: 'Ciudad. Por defecto Valencia, Carabobo, Venezuela.' },
                            dias: { type: 'number', description: 'Días de pronóstico (1-7)' }
                        }
                    }
                }
            },
            {
                type: 'function',
                function: {
                    name: 'generar_grafico',
                    description: 'Genera uno o varios gráficos Chart.js y los envía como imágenes.',
                    parameters: {
                        type: 'object',
                        properties: {
                            chartConfig: { type: 'string', description: 'JSON string de Chart.js para un gráfico.' },
                            charts: { type: 'array', items: { type: 'string' }, description: 'Array de configs JSON de Chart.js (máx 3).' },
                            width: { type: 'number' },
                            height: { type: 'number' },
                            backgroundColor: { type: 'string' }
                        }
                    }
                }
            },
            {
                type: 'function',
                function: {
                    name: 'manipular_imagen',
                    description: 'Edita la imagen adjunta o citada (sharp). Operaciones: resize, crop, rotate, grayscale, blur, sharpen, flip, flop, negate, normalize, tint, texto.',
                    parameters: {
                        type: 'object',
                        properties: {
                            operacion: { type: 'string' },
                            parametros: {
                                type: 'object',
                                description: 'Parámetros según operación: width, height, angle, sigma, color, texto, tamano, gravity, left, top, fit, background.'
                            }
                        },
                        required: ['operacion']
                    }
                }
            }
        ];

        const callDeepSeek = async (withTools) => {
            const bodyObj = { model: 'deepseek-v4-flash', messages };
            if (withTools) {
                bodyObj.tools = tools;
                bodyObj.tool_choice = 'auto';
            }
            const response = await fetch('https://api.deepseek.com/chat/completions', {
                method: 'POST',
                headers: {
                    'Authorization': `Bearer ${process.env.DEEPSEEK_API_KEY}`,
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(bodyObj),
                signal: AbortSignal.timeout(90_000)
            });
            if (!response.ok) throw new Error(`DeepSeek error: ${response.status}`);
            return (await response.json()).choices[0].message;
        };

        // 6. Bucle agéntico
        let isFinished = false;
        let iterations = 0;
        const imagesToSend = []; // { buffer?, url?, mimetype?, caption? }

        while (!isFinished && iterations < MAX_ITERATIONS) {
            const responseMessage = await callDeepSeek(true);

            // Rescate de tool-calls emitidas como texto DSML
            if (!responseMessage.tool_calls && typeof responseMessage.content === 'string' && responseMessage.content.includes('DSML')) {
                const rescued = parseDsmlToolCalls(responseMessage.content);
                if (rescued.length) {
                    console.warn(`⚠️ chat: ${rescued.length} tool-call(s) rescatada(s) de DSML`);
                    responseMessage.tool_calls = rescued;
                    responseMessage.content = sanitizeDsmlText(responseMessage.content.replace(dsmlInvokeRe, ''));
                }
            }

            messages.push(responseMessage);

            if (responseMessage.tool_calls) {
                iterations++;
                await sock.sendMessage(jid, { react: { text: '⚙️', key: msg.key } });

                for (const toolCall of responseMessage.tool_calls) {
                    let toolArgs = {};
                    try { toolArgs = JSON.parse(toolCall.function.arguments || '{}'); } catch {}
                    let toolResult = '';

                    try {
                        const fn = toolCall.function.name;

                        if (fn === 'buscar_en_internet') {
                            const youResponse = await fetch('https://ydc-index.io/v1/search', {
                                method: 'POST',
                                headers: { 'X-API-Key': process.env.YOU_API_KEY, 'Content-Type': 'application/json' },
                                body: JSON.stringify({ query: toolArgs.query, count: 3, language: 'es' })
                            });
                            const searchData = await youResponse.json();
                            toolResult = formatYouResults(searchData.results?.web || searchData.web?.results || []);
                        } else if (fn === 'buscar_web_parallel') {
                            toolResult = await parallelWebSearch(toolArgs.query);
                        } else if (fn === 'buscar_anime') {
                            toolResult = await fetchAniList(toolArgs.query);
                        } else if (fn === 'navegar_web') {
                            toolResult = await obscuraNavigate(toolArgs.url, toolArgs.extract || 'markdown');
                        } else if (fn === 'captura_web') {
                            const cap = await obscuraScreenshot(toolArgs.url);
                            if (!cap) throw new Error('Obscura no devolvió imagen');
                            imagesToSend.push({ buffer: cap.buffer, mimetype: cap.mimetype, caption: `📸 ${toolArgs.url}` });
                            toolResult = `Captura de ${toolArgs.url} enviada al chat.`;
                        } else if (fn === 'consultar_clima') {
                            toolResult = await consultarClima(toolArgs.ciudad || VALENCIA, toolArgs.dias || 3);
                        } else if (fn === 'generar_grafico') {
                            const configs = [];
                            if (toolArgs.charts && Array.isArray(toolArgs.charts)) {
                                for (const c of toolArgs.charts.slice(0, 3)) {
                                    const norm = normalizeChartArgs({ chartConfig: c });
                                    if (norm) configs.push(norm);
                                }
                            } else {
                                const norm = normalizeChartArgs(toolArgs);
                                if (norm) configs.push(norm);
                            }
                            if (!configs.length) {
                                toolResult = 'Argumentos inválidos. Usa chartConfig (string JSON de Chart.js) o charts (array de strings).';
                            } else {
                                for (const cfg of configs) {
                                    const url = `https://quickchart.io/chart?c=${encodeURIComponent(cfg)}&w=${toolArgs.width || 800}&h=${toolArgs.height || 400}&bkg=${encodeURIComponent(toolArgs.backgroundColor || 'white')}`;
                                    imagesToSend.push({ url, caption: '📊 Gráfico generado' });
                                }
                                toolResult = `Se generaron ${configs.length} gráfico(s). Explica brevemente los datos; no pegues la URL.`;
                            }
                        } else if (fn === 'manipular_imagen') {
                            if (!imageBuffer) {
                                toolResult = 'No hay imagen adjunta o citada para manipular.';
                            } else {
                                const edited = await manipulateImage(imageBuffer, toolArgs.operacion, toolArgs.parametros || {});
                                imagesToSend.push({ buffer: edited.buffer, mimetype: edited.mimetype, caption: `🖼️ Imagen editada (${toolArgs.operacion})` });
                                toolResult = `Imagen editada con "${toolArgs.operacion}" y enviada al chat.`;
                            }
                        } else {
                            toolResult = `Herramienta desconocida: ${fn}`;
                        }
                    } catch (err) {
                        console.warn(`⚠️ chat: error en herramienta ${toolCall.function.name}:`, err.message);
                        toolResult = `Error al ejecutar la herramienta: ${err.message}`;
                    }

                    messages.push({
                        role: 'tool',
                        tool_call_id: toolCall.id,
                        content: String(toolResult)
                    });
                }
            } else {
                isFinished = true;
            }
        }

        // 7. Respuesta final obligatoria
        let finalMessage = messages[messages.length - 1].content;
        if (!isFinished || !finalMessage || typeof finalMessage !== 'string' || !finalMessage.trim()) {
            messages.push({
                role: 'user',
                content: '[SISTEMA] Límite de herramientas alcanzado. Entrega AHORA tu respuesta final con la información reunida, sin llamar más herramientas.'
            });
            const forced = await callDeepSeek(false);
            finalMessage = forced.content || 'No pude completar el análisis, inténtalo de nuevo.';
        }

        // 8. Enviar imágenes y texto
        for (const img of imagesToSend) {
            if (img.buffer) {
                await sock.sendMessage(jid, { image: img.buffer, caption: img.caption || '' }, { quoted: msg });
            } else if (img.url) {
                await sock.sendMessage(jid, { image: { url: img.url }, caption: img.caption || '' }, { quoted: msg });
            }
        }

        if (finalMessage && finalMessage.trim()) {
            await sendLongText(sock, jid, sanitizeDsmlText(finalMessage), msg);
        }

        await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
    } catch (error) {
        console.error('❌ Error en el Agentic Loop:', error.message);
        await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
        await sock.sendMessage(jid, { text: 'Mi razonamiento se atascó. Inténtalo de nuevo.' }, { quoted: msg });
    }
}

// ─── Exportación del addon ──────────────────────────────────────────────────
module.exports = {
    commands: ['chat', 'c'],

    handler: async (sock, msg, args, store) => {
        const jid = msg.key.remoteJid;

        const contextInfo = msg.message?.extendedTextMessage?.contextInfo;
        const quotedMsg = contextInfo?.quotedMessage;
        const userPrompt = args.join(' ').trim()
            || msg.message?.imageMessage?.caption
            || msg.message?.documentMessage?.caption
            || msg.message?.videoMessage?.caption
            || '';
        const hasMedia = !!(msg.message?.imageMessage || msg.message?.documentMessage
            || quotedMsg?.imageMessage || quotedMsg?.documentMessage);

        if (!userPrompt && !hasMedia) {
            await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
            return;
        }

        // Host: sin restricciones
        if (isHost(msg)) {
            return processChatRequest(sock, msg, args, store);
        }

        const requesterPhone = (msg.key.participant || jid).split('@')[0].split(':')[0];
        const state = getRateState(requesterPhone);
        cleanTimestamps(state);

        // Duplicado en cola (por id de mensaje)
        if (state.queue.some(q => q.msg.key.id === msg.key.id)) {
            await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });
            return;
        }

        // Hueco disponible → procesar ya
        if (state.timestamps.length < RATE_LIMIT_MAX) {
            state.timestamps.push(Date.now());
            return processChatRequest(sock, msg, args, store);
        }

        // Cola llena
        if (state.queue.length >= QUEUE_MAX) {
            await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
            await sock.sendMessage(jid, {
                text: '⚠️ Demasiadas peticiones en cola. Espera unos segundos e intenta de nuevo.'
            }, { quoted: msg });
            return;
        }

        // Encolar
        state.queue.push({ msg, args });
        await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });
        scheduleQueue(requesterPhone, sock, store);
    }
};

function escXml(s) {
    return String(s)
        .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
}
