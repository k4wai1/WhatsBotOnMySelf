// addons/chat.js
// v3 — Bucle agéntico con Parallel Search MCP (2do buscador), historial XML,
// mensajes citados y visión DeepSeek para imágenes.

const { downloadContentFromMessage } = require('@whiskeysockets/baileys');
const unread = require('./unread');

const cooldowns = new Map();
const HISTORY_LIMIT = 30;
const MAX_ITERATIONS = 5; // rondas máximas de herramientas por sesión

function getCooldown(jid) {
    const now = Date.now();
    const last = cooldowns.get(jid) || 0;
    const remaining = last + 5000 - now;
    if (remaining > 0) return remaining;
    cooldowns.set(jid, now);
    return 0;
}

const escXml = (s) => String(s)
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');

// ─── Parallel Search MCP: cliente JSON-RPC mínimo sobre Streamable HTTP ───
const MCP_URL = process.env.PARALLEL_MCP_URL || 'https://search.parallel.ai/mcp';
let mcpSessionId = null;
let mcpRpcId = 0;

function parseMcpPayload(text) {
    const trimmed = text.trim();
    if (trimmed.startsWith('{')) {
        try { return JSON.parse(trimmed); } catch { return null; }
    }
    let payload = null;
    for (const line of text.split('\n')) {
        const l = line.trim();
        if (l.startsWith('data:')) {
            try { payload = JSON.parse(l.slice(5).trim()); } catch {}
        }
    }
    return payload;
}

async function mcpRequest(method, params, isNotification = false) {
    const headers = {
        'Content-Type': 'application/json',
        'Accept': 'application/json, text/event-stream'
    };
    if (process.env.PARALLEL_API_KEY) headers['Authorization'] = `Bearer ${process.env.PARALLEL_API_KEY}`;
    if (mcpSessionId) headers['mcp-session-id'] = mcpSessionId;

    const body = { jsonrpc: '2.0', method };
    if (!isNotification) body.id = ++mcpRpcId;
    if (params !== undefined) body.params = params;

    const res = await fetch(MCP_URL, {
        method: 'POST',
        headers,
        body: JSON.stringify(body),
        signal: AbortSignal.timeout(45000)
    });

    const sid = res.headers.get('mcp-session-id');
    if (sid) mcpSessionId = sid;

    if (isNotification || res.status === 202) return null;

    const payload = parseMcpPayload(await res.text());
    if (!payload) throw new Error('MCP: respuesta ilegible');
    if (payload.error) throw new Error(`MCP: ${payload.error.message || 'error desconocido'}`);
    return payload.result;
}

async function ensureMcp(force = false) {
    if (mcpSessionId && !force) return;
    mcpSessionId = null;
    await mcpRequest('initialize', {
        protocolVersion: '2025-03-26',
        capabilities: {},
        clientInfo: { name: 'whatsbot-self', version: '1.0' }
    });
    await mcpRequest('notifications/initialized', {}, true);
}

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
    let result;
    try {
        await ensureMcp();
        result = await mcpRequest('tools/call', { name: 'web_search', arguments: args });
    } catch (e) {
        await ensureMcp(true); // sesión expirada → nuevo handshake y reintento único
        result = await mcpRequest('tools/call', { name: 'web_search', arguments: args });
    }
    const raw = (result?.content || []).filter(c => c.type === 'text').map(c => c.text).join('\n');
    try {
        return formatParallelResults(JSON.parse(raw));
    } catch {
        return raw.slice(0, 12000) || 'Sin resultados.';
    }
}

// ─── You.com ────────────────────────────────────────────────────────────────
function formatYouResults(results, maxLen = 400) {
    if (!results || results.length === 0) return "No se encontraron resultados.";
    return results.slice(0, 3).map(r => {
        let snippet = (r.description || (r.snippets && r.snippets[0]) || '').slice(0, maxLen);
        return `📌 ${r.title || "Web"}\n${snippet}\n🔗 ${r.url || ""}`;
    }).join('\n\n');
}

// ─── AniList ────────────────────────────────────────────────────────────────
async function fetchAniList(query) {
    const graphqlQuery = `
    query ($search: String) {
      Media (search: $search, type: ANIME) {
        title { romaji english native }
        status
        episodes
        averageScore
        description
        siteUrl
      }
    }`;
    const response = await fetch('https://graphql.anilist.co', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
        body: JSON.stringify({ query: graphqlQuery, variables: { search: query } })
    });
    if (!response.ok) throw new Error("Error en AniList");
    const data = await response.json();
    return JSON.stringify(data.data.Media);
}

// ─── Visión DeepSeek ────────────────────────────────────────────────────────
async function downloadImageBuffer(imageMsg) {
    const stream = await downloadContentFromMessage(imageMsg, 'image');
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
                { role: 'system', content: 'Eres un analista visual preciso. Describe lo esencial de la imagen en función de la consulta del usuario. Si la imagen contiene texto, transcríbelo completo.' },
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
        while ((p = dsmlParamRe.exec(m[2])) !== null) {
            args[p[1]] = p[2].trim();
        }
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

// Acepta chartConfig correcto o parámetros inventados (tipo/datos/titulo) y los normaliza
function normalizeChartArgs(a = {}) {
    let cfg = a.chartConfig ?? a.chart_config ?? null;
    if (cfg && typeof cfg === 'object') return JSON.stringify(cfg);
    if (typeof cfg === 'string' && cfg.trim().startsWith('{')) {
        try { JSON.parse(cfg); return cfg.trim(); } catch {}
    }
    const data = a.datos ?? a.data ?? a.datasets ?? null;
    if (!data) return null;
    let dataObj = data;
    if (typeof dataObj === 'string') {
        try { dataObj = JSON.parse(dataObj); } catch { return null; }
    }
    if (Array.isArray(dataObj)) dataObj = { labels: [], datasets: [{ label: '', data: dataObj }] };
    const type = String(a.tipo || a.type || 'bar').replace(/["'\\]/g, '') || 'bar';
    const chart = { type, data: dataObj };
    const title = a.titulo || a.title;
    if (title) chart.options = { title: { display: true, text: String(title) } };
    return JSON.stringify(chart);
}

module.exports = {
    commands: ['chat', 'c'],

    handler: async (sock, msg, args, store) => {
        const jid = msg.key.remoteJid;
        const cdRemaining = getCooldown(jid);
        if (cdRemaining > 0) {
            await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });
            return;
        }

        const contextInfo = msg.message?.extendedTextMessage?.contextInfo;
        const quotedMsg = contextInfo?.quotedMessage;

        // Texto del comando o caption de la imagen adjunta/citada
        let userPrompt = args.join(' ').trim()
            || quotedMsg?.imageMessage?.caption
            || msg.message?.imageMessage?.caption
            || '';

        // Imagen: directa o citada
        const imageMsg = msg.message?.imageMessage || quotedMsg?.imageMessage || null;
        const isImageQuoted = !msg.message?.imageMessage && !!quotedMsg?.imageMessage;

        if (!userPrompt && !imageMsg) {
            await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
            return;
        }

        try {
            await sock.sendMessage(jid, { react: { text: '🧠', key: msg.key } });

            const requesterPhone = (msg.key.participant || jid).split('@')[0].split(':')[0];
            const requesterName = resolveName(requesterPhone, store);

            // 1. Historial reciente del chat (últimos 30, con nombres cacheados)
            const history = unread.getRecent(jid, HISTORY_LIMIT);
            const histLines = history.map((m, i) =>
                `<msg de="${escXml(resolveName(m.sender, store))}" num="${escXml(m.sender)}" cuando="${timeAgo(m.t)}">${escXml(m.text.slice(0, 300))}</msg>`
            ).join('\n');

            // 2. Mensaje citado (texto)
            const quotedText = quotedMsg?.conversation
                || quotedMsg?.extendedTextMessage?.text
                || quotedMsg?.imageMessage?.caption
                || '';

            // 3. Visión: si hay imagen, análisis previo con el modelo vision
            let imageAnalysis = '';
            if (imageMsg) {
                await sock.sendMessage(jid, { react: { text: '📡', key: msg.key } });
                try {
                    const imgBuffer = await downloadImageBuffer(imageMsg);
                    if (!imgBuffer || imgBuffer.length === 0) throw new Error('imagen vacía/expirada');
                    const visionQuestion =
                        `Consulta del usuario "${requesterName}": ${userPrompt || 'Describe esta imagen.'}` +
                        (quotedText ? `\nContexto adicional del mensaje citado: ${quotedText.slice(0, 500)}` : '');
                    imageAnalysis = await analyzeImage(imgBuffer, imageMsg.mimetype || 'image/jpeg', visionQuestion);
                } catch (e) {
                    console.warn('⚠️ chat: visión falló:', e.message);
                    imageAnalysis = `(No se pudo analizar la imagen: ${e.message})`;
                }
            }

            // 4. Contexto XML para el modelo
            const contextXmlParts = [];
            if (histLines) {
                contextXmlParts.push(`<historial_chat ultimos="${history.length}" nota="Solo contexto. El autor relevante de esta petición es quien firma <peticion_actual>.">\n${histLines}\n</historial_chat>`);
            }
            if (quotedText.trim()) {
                const quotedAuthorPhone = contextInfo?.participant ? contextInfo.participant.split('@')[0] : requesterPhone;
                contextXmlParts.push(`<mensaje_citado de="${escXml(resolveName(quotedAuthorPhone, store))}" num="${escXml(quotedAuthorPhone)}">\n${escXml(quotedText.slice(0, 1500))}\n</mensaje_citado>`);
            }
            if (imageAnalysis) {
                contextXmlParts.push(`<analisis_imagen fuente="${isImageQuoted ? 'citada' : 'adjunta'}">\n${escXml(imageAnalysis.slice(0, 4000))}\n</analisis_imagen>`);
            }
            contextXmlParts.push(`<peticion_actual de="${escXml(requesterName)}" num="${escXml(requesterPhone)}">${escXml(userPrompt || 'Analiza la imagen y cuéntame qué ves.')}</peticion_actual>`);

            const userContent = `<contexto>\n${contextXmlParts.join('\n')}\n</contexto>`;

            const systemPrompt = `Eres el núcleo de razonamiento de un asistente avanzado de WhatsApp. Eres directo, lógico y amigable. No usas lenguaje robótico.

CONTEXTO ESTRUCTURADO:
Recibirás un bloque <contexto> con delimitadores XML. Dentro puede haber:
- <historial_chat>: últimos mensajes reales del chat (solo contexto de conversación).
- <mensaje_citado>: mensaje específico que el usuario citó al invocarte.
- <analisis_imagen>: descripción previa de una imagen adjunta o citada.
- <peticion_actual>: LA CONSULTA A RESPONDER. Tiene prioridad absoluta sobre todo lo demás.
El único autor relevante del contexto es quien aparece en <peticion_actual>; los demás mensajes son solo ruido de fondo útil para entender referencias ("eso", "el de ayer", etc.).

TUS HERRAMIENTAS:
- buscar_en_internet (You.com): búsqueda web clásica.
- buscar_web_parallel (Parallel Search MCP): búsqueda web alternativa con resultados recientes y extractos. Úsala como segunda opción o si You.com falla/devuelve poco.
- buscar_anime (AniList): datos exactos de anime/manga.
- generar_grafico (QuickChart): gráficos Chart.js; incluye un breve análisis en tu respuesta final pero NUNCA pegues la URL (la imagen se envía sola).

USO CORRECTO DE HERRAMIENTAS (CRÍTICO):
- Invoca las herramientas EXCLUSIVAMENTE mediante el mecanismo nativo de function calling del API. NUNCA escribas la llamada dentro de tu texto, ni en XML, ni en formato DSML, ni con etiquetas <invoke> o <parameter>.
- Para generar un gráfico usa SOLO el parámetro 'chartConfig': un STRING con UN objeto JSON válido de Chart.js completo. Ejemplo de llamada correcta:
  generar_grafico(chartConfig="{\\"type\\":\\"bar\\",\\"data\\":{\\"labels\\":[\\"A\\",\\"B\\"],\\"datasets\\":[{\\"label\\":\\"Ventas\\",\\"data\\":[10,20]}]},\\"options\\":{\\"title\\":{\\"display\\":true,\\"text\\":\\"Mi título\\"}}}")
  El JSON debe incluir 'type', 'data' (con labels y datasets) y opcionalmente 'options.title'. NO inventes parámetros adicionales como tipo/datos/titulo por separado: todo va DENTRO del JSON de chartConfig.

POLÍTICA DE BÚSQUEDA WEB:
- Si la petición NO necesita información actual ni verificación externa (charla, opiniones, código, mates, conocimiento estable), responde DIRECTO sin herramientas.
- Si necesita datos actuales, itera: busca, lee resultados, y vuelve a buscar con otras consultas si te falta información. Máximo ${MAX_ITERATIONS} rondas de herramientas.
- Cuando se acabe el límite, estás OBLIGADO a entregar una respuesta final con lo que tengas (indicando brevemente si quedó algo sin verificar).

REGLAS DE FORMATO:
- WhatsApp tiene formato limitado. Usa *negritas* solo para resaltar datos clave, nombres o títulos. No uses asteriscos en palabras comunes.
- Listas con guiones (-) o números para información densa.
- No saludes en cada mensaje ni des introducciones largas. Ve directo al grano.`;

            const messages = [
                { role: 'system', content: systemPrompt },
                { role: 'user', content: userContent }
            ];

            const tools = [
                {
                    type: "function",
                    function: {
                        name: "buscar_en_internet",
                        description: "Busca en internet noticias, definiciones o datos en tiempo real (You.com).",
                        parameters: {
                            type: "object",
                            properties: { query: { type: "string" } },
                            required: ["query"]
                        }
                    }
                },
                {
                    type: "function",
                    function: {
                        name: "buscar_web_parallel",
                        description: "Búsqueda web alternativa vía Parallel Search MCP. Devuelve resultados recientes con extractos. Segunda opción ante fallos de You.com.",
                        parameters: {
                            type: "object",
                            properties: { query: { type: "string", description: "Objetivo de búsqueda en lenguaje natural" } },
                            required: ["query"]
                        }
                    }
                },
                {
                    type: "function",
                    function: {
                        name: "buscar_anime",
                        description: "Busca datos exactos, sinopsis y puntuación de un anime.",
                        parameters: {
                            type: "object",
                            properties: { query: { type: "string", description: "Nombre del anime" } },
                            required: ["query"]
                        }
                    }
                },
                {
                    type: "function",
                    function: {
                        name: "generar_grafico",
                        description: "Crea un gráfico visual (barras, torta, radar, etc.) usando Chart.js y devuelve la URL de la imagen.",
                        parameters: {
                            type: "object",
                            properties: {
                                chartConfig: {
                                    type: "string",
                                    description: "Un objeto JSON en formato string válido para Chart.js (ej: {'type':'bar','data':{'labels':['A'],'datasets':[{'data':[1]}]}})."
                                }
                            },
                            required: ["chartConfig"]
                        }
                    }
                }
            ];

            const callDeepSeek = async (withTools) => {
                const bodyObj = {
                    model: 'deepseek-v4-flash',
                    messages,
                };
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
                    body: JSON.stringify(bodyObj)
                });
                if (!response.ok) throw new Error(`DeepSeek error: ${response.status}`);
                return (await response.json()).choices[0].message;
            };

            // 5. El bucle agéntico
            let isFinished = false;
            let iterations = 0;
            const imagesToSend = [];

            while (!isFinished && iterations < MAX_ITERATIONS) {
                const responseMessage = await callDeepSeek(true);

                // Rescate: a veces el modelo emite la llamada como texto DSML en vez de tool_calls nativas
                if (!responseMessage.tool_calls && typeof responseMessage.content === 'string' && responseMessage.content.includes('DSML')) {
                    const rescued = parseDsmlToolCalls(responseMessage.content);
                    if (rescued.length) {
                        console.warn(`⚠️ chat: ${rescued.length} tool-call(s) rescatada(s) de texto DSML`);
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
                        let toolResult = "";

                        try {
                            if (toolCall.function.name === 'buscar_en_internet') {
                                const youResponse = await fetch('https://ydc-index.io/v1/search', {
                                    method: 'POST',
                                    headers: { 'X-API-Key': process.env.YOU_API_KEY, 'Content-Type': 'application/json' },
                                    body: JSON.stringify({ query: toolArgs.query, count: 3, language: 'es' })
                                });
                                const searchData = await youResponse.json();
                                toolResult = formatYouResults(searchData.results?.web || searchData.web?.results || []);
                            } else if (toolCall.function.name === 'buscar_web_parallel') {
                                toolResult = await parallelWebSearch(toolArgs.query);
                            } else if (toolCall.function.name === 'buscar_anime') {
                                toolResult = await fetchAniList(toolArgs.query);
                            } else if (toolCall.function.name === 'generar_grafico') {
                                const chartConfig = normalizeChartArgs(toolArgs);
                                if (!chartConfig) {
                                    toolResult = 'Argumentos inválidos para generar_grafico. Debes pasar SOLO el parámetro chartConfig: un string con JSON válido de Chart.js (type, data.labels, data.datasets).';
                                } else {
                                    const chartUrl = `https://quickchart.io/chart?c=${encodeURIComponent(chartConfig)}&w=800&h=400&bkg=white`;
                                    imagesToSend.push(chartUrl);
                                    toolResult = `Gráfico generado exitosamente en: ${chartUrl}. Procede a dar la explicación al usuario.`;
                                }
                            } else {
                                toolResult = `Herramienta desconocida: ${toolCall.function.name}`;
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

            // 6. Respuesta final OBLIGATORIA si se agotaron las iteraciones
            let finalMessage = messages[messages.length - 1].content;
            if (!isFinished || !finalMessage || typeof finalMessage !== 'string' || !finalMessage.trim()) {
                messages.push({
                    role: 'user',
                    content: `[SISTEMA] Límite de herramientas alcanzado. Entrega AHORA tu respuesta final con la información reunida, sin llamar más herramientas.`
                });
                const forced = await callDeepSeek(false);
                finalMessage = forced.content || 'No pude completar el análisis, inténtalo de nuevo.';
            }

            for (const imageUrl of imagesToSend) {
                await sock.sendMessage(jid, {
                    image: { url: imageUrl },
                }, { quoted: msg });
            }

            if (finalMessage && finalMessage.trim()) {
                await sock.sendMessage(jid, { text: sanitizeDsmlText(finalMessage) }, { quoted: msg });
            }

            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

        } catch (error) {
            console.error('❌ Error en el Agentic Loop:', error.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
            await sock.sendMessage(jid, { text: "Mi razonamiento se atascó. Inténtalo de nuevo." }, { quoted: msg });
        }
    }
};
