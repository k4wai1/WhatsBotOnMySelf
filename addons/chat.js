// addons/chat.js
// Integración de MCP (You.com, AniList, QuickChart) con Bucle de Razonamiento

const cooldowns = new Map();

function getCooldown(jid) {
    const now = Date.now();
    const last = cooldowns.get(jid) || 0;
    const remaining = last + 5000 - now;
    if (remaining > 0) return remaining;
    cooldowns.set(jid, now);
    return 0;
}

// Formateador de You.com
function formatYouResults(results, maxLen = 400) {
    if (!results || results.length === 0) return "No se encontraron resultados.";
    return results.slice(0, 3).map(r => {
        let snippet = (r.description || (r.snippets && r.snippets[0]) || '').slice(0, maxLen);
        return `📌 ${r.title || "Web"}\n${snippet}\n🔗 ${r.url || ""}`;
    }).join('\n\n');
}

// Fetch a AniList GraphQL
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

module.exports = {
    commands: ['chat', 'c'],
    handler: async (sock, msg, args, store) => {
        const jid = msg.key.remoteJid;
        const userPrompt = args.join(' ').trim();

        if (!userPrompt) {
            await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
            return;
        }

        try {
            await sock.sendMessage(jid, { react: { text: '🧠', key: msg.key } });

            // 1. El Prompt Súper Descriptivo
            const systemPrompt = `Eres el núcleo de razonamiento de un asistente avanzado de WhatsApp. Eres directo, lógico y amigable. No usas lenguaje robótico.

TUS CAPACIDADES (AGENTIC LOOP):
- Tienes herramientas para buscar en internet (You.com), buscar información de Anime/Manga (AniList) y generar gráficos visuales (QuickChart).
- Puedes pensar paso a paso. Si el usuario pide algo complejo, puedes llamar a una herramienta, leer la respuesta, y si te falta información, llamar a otra herramienta antes de darle la respuesta final al usuario.
- Si generas un gráfico, la herramienta te devolverá una URL. INCLUYE un breve análisis de ese gráfico en tu respuesta final, pero NO incluyas la URL en texto (el sistema enviará la imagen automáticamente).

REGLAS DE FORMATO Y CONTEXTO:
- Prioridad absoluta: El último mensaje del usuario. El contexto previo es solo soporte.
- WhatsApp tiene formato limitado. Usa *negritas* solo para resaltar datos clave, nombres o títulos. No uses asteriscos en palabras comunes.
- Usa listas con guiones (-) o números para organizar información densa.
- No saludes en cada mensaje ni des introducciones largas. Ve directo al grano.
- Si te piden un gráfico, diseña un JSON válido para Chart.js.

Misión actual: Asiste al usuario resolviendo su consulta de la manera más eficiente posible.`;

            // Aquí en un futuro puedes inyectar el historial breve sacándolo del 'store'
            const messages = [
                { role: 'system', content: systemPrompt },
                { role: 'user', content: userPrompt }
            ];

            const tools = [
                {
                    type: "function",
                    function: {
                        name: "buscar_en_internet",
                        description: "Busca en internet noticias, definiciones o datos en tiempo real.",
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

            let isFinished = false;
            let iterations = 0;
            const MAX_ITERATIONS = 4; // Límite para evitar bucles infinitos
            const imagesToSend = []; // Almacenará las URLs de los gráficos

            // 2. El Bucle de Razonamiento
            while (!isFinished && iterations < MAX_ITERATIONS) {
                const response = await fetch('https://api.deepseek.com/chat/completions', {
                    method: 'POST',
                    headers: {
                        'Authorization': `Bearer ${process.env.DEEPSEEK_API_KEY}`,
                        'Content-Type': 'application/json'
                    },
                    body: JSON.stringify({
                        model: 'deepseek-v4-flash',
                        messages: messages,
                        tools: tools,
                        tool_choice: 'auto'
                    })
                });

                if (!response.ok) throw new Error(`DeepSeek error: ${response.status}`);
                const data = await response.json();
                const responseMessage = data.choices[0].message;

                messages.push(responseMessage); // Guardar el paso actual en el historial

                if (responseMessage.tool_calls) {
                    iterations++;
                    await sock.sendMessage(jid, { react: { text: '⚙️', key: msg.key } }); // Indicador de que está procesando herramientas

                    // Procesar todas las llamadas a herramientas en paralelo
                    for (const toolCall of responseMessage.tool_calls) {
                        const args = JSON.parse(toolCall.function.arguments);
                        let toolResult = "";

                        try {
                            if (toolCall.function.name === 'buscar_en_internet') {
                                const youResponse = await fetch('https://ydc-index.io/v1/search', {
                                    method: 'POST',
                                    headers: { 'X-API-Key': process.env.YOU_API_KEY, 'Content-Type': 'application/json' },
                                    body: JSON.stringify({ query: args.query, count: 3, language: 'es' })
                                });
                                const searchData = await youResponse.json();
                                toolResult = formatYouResults(searchData.results?.web || searchData.web?.results || []);
                            
                            } else if (toolCall.function.name === 'buscar_anime') {
                                toolResult = await fetchAniList(args.query);
                            
                            } else if (toolCall.function.name === 'generar_grafico') {
                                // Construir URL de QuickChart con el JSON escapado
                                const chartUrl = `https://quickchart.io/chart?c=${encodeURIComponent(args.chartConfig)}&w=800&h=400&bkg=white`;
                                imagesToSend.push(chartUrl);
                                toolResult = `Gráfico generado exitosamente en: ${chartUrl}. Procede a dar la explicación al usuario.`;
                            }
                        } catch (err) {
                            console.warn(`⚠️ Error en herramienta ${toolCall.function.name}:`, err.message);
                            toolResult = `Error al ejecutar la herramienta: ${err.message}`;
                        }

                        // Entregar el resultado de la herramienta de vuelta a DeepSeek
                        messages.push({
                            role: 'tool',
                            tool_call_id: toolCall.id,
                            content: toolResult
                        });
                    }
                } else {
                    // Si no hay tool_calls, el modelo ha decidido su respuesta final
                    isFinished = true;
                }
            }

            // 3. Entrega Final
            const finalMessage = messages[messages.length - 1].content.trim();

            // Si el modelo generó gráficos, enviarlos primero
            for (const imageUrl of imagesToSend) {
                await sock.sendMessage(jid, { 
                    image: { url: imageUrl }, 
                }, { quoted: msg });
            }

            // Enviar el texto final
            if (finalMessage) {
                await sock.sendMessage(jid, { text: finalMessage }, { quoted: msg });
            }
            
            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

        } catch (error) {
            console.error('❌ Error en el Agentic Loop:', error.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
            await sock.sendMessage(jid, { text: "Mi razonamiento se atascó. Inténtalo de nuevo." }, { quoted: msg });
        }
    }
};
