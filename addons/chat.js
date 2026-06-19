// addons/chat.js
// Búsqueda web con You.com API gratuita (ydc-index.io) - 100 llamadas/día
// Estilo conversacional y natural, sin abuso de formatos

const cooldowns = new Map();

function getCooldown(jid) {
    const now = Date.now();
    const last = cooldowns.get(jid) || 0;
    const remaining = last + 5000 - now;
    if (remaining > 0) return remaining;
    cooldowns.set(jid, now);
    return 0;
}

function formatYouResults(results, maxLen = 400) {
    if (!results || results.length === 0) {
        return "No se encontraron resultados en la web para tu consulta.";
    }
    return results
        .slice(0, 5)
        .map((r) => {
            let snippet = r.description || '';
            if (!snippet && r.snippets && r.snippets.length) {
                snippet = r.snippets[0];
            }
            snippet = snippet.slice(0, maxLen);
            const title = r.title || "Sin título";
            const url = r.url || "#";
            // Usamos negritas solo para el título del resultado (es breve y útil)
            return `📌 *${title}*\n${snippet}\n🔗 ${url}`;
        })
        .join('\n\n');
}

module.exports = {
    commands: ['chat', 'c'],
    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;
        const userPrompt = args.join(' ').trim();

        if (!userPrompt) {
            await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
            return;
        }

        try {
            await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

            const messages = [
                {
                    role: 'system',
                    content: `Eres un asistente amigable y tranquilo para WhatsApp. Hablas de forma natural, como si estuvieras conversando con un amigo. No uses formatos excesivos; úsalos solo cuando ayuden a la claridad.

Formatos válidos en WhatsApp (úsalos con moderación):
- Negrita: *texto* (solo para énfasis puntual, no abuses)
- Cursiva: _texto_ (para matices o títulos)
- Tachado: ~texto~ (para correcciones o humor)
- Monoespaciado: \`código o comandos\`
- Listas: - elemento  (guión y espacio)
- Listas numeradas: 1. elemento
- Citas: > texto
- Bloque de código: \`\`\`código\`\`\` (si es multilínea)

Reglas de estilo:
- Prefiere el texto plano y natural. Usa negritas solo si es muy necesario (ej. resaltar un número o una palabra clave muy breve).
- No pongas asteriscos alrededor de palabras comunes ni frases largas.
- Sé cálido, relajado y directo. No uses lenguaje robótico ni formalidades exageradas.
- Si necesitas listar información, usa guiones o números, pero evita saturar.
- Cuando cites una fuente, simplemente menciona el dominio o título sin exagerar.
- Si no puedes buscar en internet, dilo de forma simple: "No pude buscar en línea ahora, pero según lo que sé..." y responde con tu conocimiento interno.

Recuerda: el objetivo es que la conversación fluya naturalmente, como entre personas.`
                },
                {
                    role: 'user',
                    content: userPrompt
                }
            ];

            const tools = [
                {
                    type: "function",
                    function: {
                        name: "buscar_en_internet",
                        description: "Busca en la web información actualizada usando You.com. Útil para noticias, fechas, definiciones, precios, etc.",
                        parameters: {
                            type: "object",
                            properties: {
                                query: { type: "string", description: "Términos de búsqueda optimizados (español o inglés)" }
                            },
                            required: ["query"]
                        }
                    }
                }
            ];

            let response = await fetch('https://api.deepseek.com/chat/completions', {
                method: 'POST',
                headers: {
                    'Authorization': `Bearer ${process.env.DEEPSEEK_API_KEY}`,
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({
                    model: 'deepseek-chat',
                    messages: messages,
                    tools: tools,
                    tool_choice: 'auto'
                })
            });

            if (!response.ok) throw new Error(`DeepSeek error: ${response.status}`);
            let data = await response.json();
            let responseMessage = data.choices[0].message;

            if (responseMessage.tool_calls) {
                await sock.sendMessage(jid, { react: { text: '🔍', key: msg.key } });

                const cooldown = getCooldown(jid);
                if (cooldown > 0) {
                    await new Promise(resolve => setTimeout(resolve, cooldown));
                }

                messages.push(responseMessage);

                for (const toolCall of responseMessage.tool_calls) {
                    if (toolCall.function.name === 'buscar_en_internet') {
                        const { query } = JSON.parse(toolCall.function.arguments);
                        let toolResponseContent;

                        try {
                            const youUrl = 'https://ydc-index.io/v1/search';
                            const youResponse = await fetch(youUrl, {
                                method: 'POST',
                                headers: {
                                    'X-API-Key': process.env.YOU_API_KEY,
                                    'Content-Type': 'application/json'
                                },
                                body: JSON.stringify({
                                    query: query,
                                    count: 5,
                                    language: 'es',
                                    freshness: 'month'
                                })
                            });

                            if (!youResponse.ok) {
                                throw new Error(`You.com API error: ${youResponse.status}`);
                            }

                            const searchData = await youResponse.json();
                            const webResults = searchData.results?.web || searchData.web?.results || [];
                            toolResponseContent = formatYouResults(webResults);
                        } catch (searchError) {
                            console.warn('⚠️ Error en búsqueda You.com:', searchError.message);
                            toolResponseContent = "🔍 No pude buscar en internet en este momento (límite diario o error de conexión). Responderé con mi conocimiento interno, que podría no estar actualizado.";
                        }

                        messages.push({
                            role: 'tool',
                            tool_call_id: toolCall.id,
                            content: toolResponseContent
                        });
                    }
                }

                response = await fetch('https://api.deepseek.com/chat/completions', {
                    method: 'POST',
                    headers: {
                        'Authorization': `Bearer ${process.env.DEEPSEEK_API_KEY}`,
                        'Content-Type': 'application/json'
                    },
                    body: JSON.stringify({
                        model: 'deepseek-chat',
                        messages: messages
                    })
                });

                data = await response.json();
                responseMessage = data.choices[0].message;
            }

            await sock.sendMessage(jid, { text: responseMessage.content.trim() }, { quoted: msg });
            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

        } catch (error) {
            console.error('❌ Error en addon .chat:', error.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
            await sock.sendMessage(jid, { text: "Ocurrió un error inesperado. Inténtalo de nuevo en unos segundos." }, { quoted: msg });
        }
    }
};
