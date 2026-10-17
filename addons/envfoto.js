// addons/envfoto.js
// Utilidad para enviar imágenes a un canal/newsletter (NEWS_CHANNEL_ID en .env).
// Modos: 1 = imagen generada con canvas, 2 = descargar desde URL, 3 = imagen local.
// Portado del bot de rupa (img.js) y adaptado al router de addons.
const fs = require('fs');
const path = require('path');
const axios = require('axios');
const { createCanvas } = require('canvas');

const CHANNEL_ID = process.env.NEWS_CHANNEL_ID || '';

// Función de descarga con headers reales (evita 403)
async function descargarImagen(url) {
    try {
        const response = await axios.get(url, {
            responseType: 'arraybuffer',
            headers: {
                'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
                'Accept': 'text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8',
                'Accept-Encoding': 'gzip, deflate, br',
                'Accept-Language': 'es-ES,es;q=0.9',
                'Connection': 'keep-alive'
            },
            timeout: 30000
        });
        return Buffer.from(response.data);
    } catch (error) {
        console.error('❌ [envfoto] Error en descargarImagen:', error.message);
        throw new Error(`No se pudo descargar la imagen: ${error.message}`);
    }
}

module.exports = {
    commands: ['envfoto'],

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;

        if (!CHANNEL_ID) {
            await sock.sendMessage(jid, { text: '❌ Configura NEWS_CHANNEL_ID en el .env para usar este comando.' }, { quoted: msg });
            return;
        }

        const modo = args[0] || '1';

        // Reacción de "procesando"
        await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

        try {
            let buffer;
            let caption = '';

            // MODO 1: imagen generada con Canvas
            if (modo === '1') {
                const canvas = createCanvas(800, 400);
                const ctx = canvas.getContext('2d');
                ctx.fillStyle = '#ffffff';
                ctx.fillRect(0, 0, 800, 400);
                ctx.fillStyle = '#000000';
                ctx.font = '30px Arial';
                ctx.fillText('Prueba de imagen a canal', 50, 200);
                ctx.font = '20px Arial';
                ctx.fillText(`Enviado: ${new Date().toLocaleString()}`, 50, 300);
                buffer = canvas.toBuffer();
                caption = '🎨 Imagen generada con Canvas (modo 1)';
            }
            // MODO 2: descarga desde URL
            else if (modo === '2') {
                const url = args[1];
                if (!url) throw new Error('Falta la URL. Uso: envfoto 2 <url>');
                buffer = await descargarImagen(url);
                caption = `🌐 Imagen descargada: ${url.split('/').pop() || 'web'}`;
            }
            // MODO 3: imagen local (por defecto la gráfica de tasas)
            else if (modo === '3') {
                let ruta = args[1] || path.join(__dirname, '..', 'assets', 'statistics', 'rates_chart.png');
                if (!fs.existsSync(ruta)) throw new Error(`Archivo no encontrado: ${ruta}`);
                buffer = fs.readFileSync(ruta);
                caption = `📁 Imagen local: ${path.basename(ruta)}`;
            }
            else {
                throw new Error('Modo inválido. Usa envfoto [1|2|3] [url|ruta]');
            }

            // Envío directo al canal
            const resultado = await sock.sendMessage(CHANNEL_ID, {
                image: buffer,
                caption: caption,
                mimetype: 'image/png'
            });

            console.log(`✅ [envfoto] Imagen enviada al canal ${CHANNEL_ID} | ID: ${resultado?.key?.id}`);
            await sock.sendMessage(jid, { text: `✅ Imagen enviada al canal (modo ${modo})` }, { quoted: msg });

        } catch (err) {
            console.error('❌ [envfoto] Error:', err.message);
            await sock.sendMessage(jid, { text: `❌ Error: ${err.message}` }, { quoted: msg });
        } finally {
            await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
        }
    }
};
