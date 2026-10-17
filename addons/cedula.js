// addons/cedula.js — Consulta de cédulas venezolanas vía cedula.com.ve
// Requiere variables de entorno: CEDULA_APP_ID y CEDULA_TOKEN
// Obtén tus credenciales gratis en https://cedula.com.ve (plan FREE: 200 consultas/hora)

const API_URL = 'https://api.cedula.com.ve/api/v1';

module.exports = {
    commands: ['cedula', 'ci'],

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;

        const appId = process.env.CEDULA_APP_ID;
        const token = process.env.CEDULA_TOKEN;

        if (!appId || !token) {
            await sock.sendMessage(jid, {
                text: '⚙️ *API de cédulas no configurada*\n\nPara usar este comando necesitas obtener credenciales gratis en:\nhttps://cedula.com.ve\n\nLuego agrega en tu archivo *.env*:\n```\nCEDULA_APP_ID=tu_app_id\nCEDULA_TOKEN=tu_token\n```'
            }, { quoted: msg });
            return;
        }

        let rawInput = args.join(' ');
        if (!rawInput) {
            await sock.sendMessage(jid, {
                text: '📋 *Consulta de Cédula*\n\nUso: `.cedula <número>`\nEjemplo: `.cedula 32832480`\n\nTambién puedes usar: `.ci <número>`'
            }, { quoted: msg });
            return;
        }

        // Limpiar input: quitar puntos, espacios, guiones, prefijos V- o E-
        let cleaned = rawInput
            .replace(/[.\-\s]/g, '')
            .replace(/^[vVeE]-?/i, '')
            .trim();

        // Determinar nacionalidad
        let nacionalidad = 'V';
        if (/^[vV]/.test(rawInput.trim())) {
            nacionalidad = 'V';
        } else if (/^[eE]/.test(rawInput.trim())) {
            nacionalidad = 'E';
        }

        // Validar que solo queden dígitos
        if (!/^\d+$/.test(cleaned) || cleaned.length < 3 || cleaned.length > 12) {
            await sock.sendMessage(jid, {
                text: '⚠️ Número de cédula inválido. Solo se permiten dígitos (3-12 caracteres).'
            }, { quoted: msg });
            return;
        }

        try {
            await sock.sendMessage(jid, { react: { text: '🔍', key: msg.key } });

            const url = `${API_URL}?app_id=${encodeURIComponent(appId)}&token=${encodeURIComponent(token)}&nacionalidad=${nacionalidad}&cedula=${cleaned}`;

            const response = await fetch(url);
            const data = await response.json();

            if (data.error || !data.data) {
                const errorMap = {
                    'INVALID_TOKEN': 'Token o App-ID inválido. Verifica tus credenciales en cedula.com.ve',
                    'NOT_FOUND': 'Cédula no encontrada en los registros',
                    'RECORD_NOT_FOUND': 'Cédula no encontrada en los registros',
                    'CEDULA_NOT_FOUND': 'Cédula no encontrada en los registros',
                    'INVALID_CEDULA': 'Número de cédula inválido',
                    'LIMIT_EXCEEDED': 'Límite de consultas alcanzado. Intenta más tarde'
                };
                const errorMsg = errorMap[data.error_str] || `Error: ${data.error_str || 'Desconocido'}`;

                await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                await sock.sendMessage(jid, {
                    text: `❌ *Consulta fallida*\n\n${errorMsg}`
                }, { quoted: msg });
                return;
            }

            const d = data.data;
            const nombre = [d.primer_nombre, d.segundo_nombre].filter(Boolean).join(' ');
            const apellido = [d.primer_apellido, d.segundo_apellido].filter(Boolean).join(' ');

            let text = `🪪 *Datos de Cédula*\n`;
            text += `━━━━━━━━━━━━━━━━━\n`;
            text += `📄 *Cédula:* ${nacionalidad}-${d.cedula}\n`;
            text += `👤 *Nombre:* ${nombre}\n`;
            text += `👥 *Apellido:* ${apellido}\n`;
            text += `🏢 *RIF:* ${d.rif || 'N/A'}\n`;
            if (d.fecha_nac) {
                text += `🎂 *Fecha Nac:* ${d.fecha_nac}\n`;
            }

            if (d.cne) {
                text += `\n🗳️ *Datos Electorales*\n`;
                text += `📍 Estado: ${d.cne.estado || 'N/A'}\n`;
                text += `🏘️ Municipio: ${d.cne.municipio || 'N/A'}\n`;
                text += `📌 Parroquia: ${d.cne.parroquia || 'N/A'}\n`;
                text += `🏛️ Centro: ${d.cne.centro_electoral || 'N/A'}\n`;
            }

            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
            await sock.sendMessage(jid, { text }, { quoted: msg });

        } catch (error) {
            console.error('❌ [cedula]:', error.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
            await sock.sendMessage(jid, {
                text: '❌ Error al consultar la API. Verifica tu conexión e intenta de nuevo.'
            }, { quoted: msg });
        }
    }
};
