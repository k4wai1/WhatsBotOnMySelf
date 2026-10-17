// addons/love.js
// Medidor de amor determinista con imagen (canvas): avatares + corazón.
// Portado del bot de rupa y adaptado al router de addons.
const { createCanvas, loadImage } = require('canvas');

module.exports = {
    commands: ['love'],

    handler: async (sock, msg, args, store) => {
        const jid = msg.key.remoteJid;

        try {
            const sender = msg.key.participant || jid;
            const senderName = msg.pushName || '~';

            // Resolver nombre desde el store de contactos (si existe)
            const storeName = (targetJid) => {
                try {
                    const c = store?.contacts?.[targetJid];
                    return c?.notify || c?.name || '~';
                } catch { return '~'; }
            };

            let user1 = sender;
            let user2 = null;
            let name1 = senderName;
            let name2 = '~';

            const contextInfo = msg.message?.extendedTextMessage?.contextInfo;
            const mentions = contextInfo?.mentionedJid || [];
            const quoted = contextInfo?.participant;

            // Lógica para determinar a quiénes estamos midiendo
            if (mentions.length >= 2) {
                user1 = mentions[0];
                user2 = mentions[1];
                name1 = storeName(user1);
                name2 = storeName(user2);
            } else if (mentions.length === 1) {
                user1 = sender;
                user2 = mentions[0];
                name1 = senderName;
                name2 = storeName(user2);
            } else if (quoted) {
                user1 = sender;
                user2 = quoted;
                name1 = senderName;
                name2 = storeName(user2);
            }

            if (!user2) {
                return sock.sendMessage(jid, { text: '💕 Menciona a alguien o responde a su mensaje para medir el amor.' }, { quoted: msg });
            }

            await sock.sendMessage(jid, { react: { text: '💖', key: msg.key } });

            // Cálculo determinista del porcentaje (siempre da el mismo resultado para la misma pareja)
            const pair = [user1, user2].sort();
            const seed = pair[0] + pair[1];
            let hash = 0;
            for (let i = 0; i < seed.length; i++) {
                hash = seed.charCodeAt(i) + ((hash << 5) - hash);
            }
            const percent = Math.abs(hash) % 101; // De 0 a 100

            // Función para obtener la foto de perfil o generar la inicial
            const getAvatar = async (targetJid, fallbackName) => {
                try {
                    const url = await sock.profilePictureUrl(targetJid, 'image');
                    return await loadImage(url);
                } catch {
                    // Generar avatar gris con la inicial si no tiene foto pública
                    const size = 200;
                    const c = createCanvas(size, size);
                    const ctx = c.getContext('2d');

                    ctx.fillStyle = '#888888';
                    ctx.fillRect(0, 0, size, size);

                    const initial = (fallbackName && fallbackName !== '~') ? fallbackName.charAt(0).toUpperCase() : '~';
                    ctx.fillStyle = '#ffffff';
                    ctx.font = 'bold 100px Arial';
                    ctx.textAlign = 'center';
                    ctx.textBaseline = 'middle';
                    ctx.fillText(initial, size / 2, size / 2 + 5);

                    return c;
                }
            };

            // Descargamos ambas imágenes (o generamos las iniciales)
            const [img1, img2] = await Promise.all([
                getAvatar(user1, name1),
                getAvatar(user2, name2)
            ]);

            // --- CREACIÓN DEL BANNER CON CANVAS ---
            const width = 900;
            const height = 300; // Ratio 3x1
            const canvas = createCanvas(width, height);
            const ctx = canvas.getContext('2d');

            // Fondo rosa pastel
            ctx.fillStyle = '#ffdde1';
            ctx.fillRect(0, 0, width, height);

            // Función para recortar en círculo y dibujar el avatar
            const drawAvatar = (img, x, y, size) => {
                ctx.save();
                ctx.beginPath();
                ctx.arc(x + size / 2, y + size / 2, size / 2, 0, Math.PI * 2, true);
                ctx.closePath();
                ctx.clip();
                ctx.drawImage(img, x, y, size, size);
                ctx.restore();

                // Borde blanco alrededor del círculo
                ctx.beginPath();
                ctx.arc(x + size / 2, y + size / 2, size / 2, 0, Math.PI * 2, true);
                ctx.lineWidth = 10;
                ctx.strokeStyle = '#ffffff';
                ctx.stroke();
            };

            // Dibujar avatares (izquierdo y derecho)
            drawAvatar(img1, 100, 50, 200);
            drawAvatar(img2, 600, 50, 200);

            // Función para dibujar el corazón
            const drawHeart = (c, x, y, w, h) => {
                c.save();
                c.beginPath();
                const topCurveHeight = h * 0.3;
                c.moveTo(x, y + topCurveHeight);
                c.bezierCurveTo(x, y, x - w / 2, y, x - w / 2, y + topCurveHeight);
                c.bezierCurveTo(x - w / 2, y + (h + topCurveHeight) / 2, x, y + (h + topCurveHeight) / 2, x, y + h);
                c.bezierCurveTo(x, y + (h + topCurveHeight) / 2, x + w / 2, y + (h + topCurveHeight) / 2, x + w / 2, y + topCurveHeight);
                c.bezierCurveTo(x + w / 2, y, x, y, x, y + topCurveHeight);
                c.closePath();

                c.fillStyle = '#ff1493'; // Color rosa fuerte
                c.fill();

                c.lineWidth = 5;
                c.strokeStyle = '#ffffff';
                c.stroke();
                c.restore();
            };

            // Dibujar corazón en el centro
            drawHeart(ctx, 450, 70, 200, 180);

            // Texto del porcentaje dentro del corazón
            ctx.fillStyle = '#ffffff';
            ctx.font = 'bold 50px Arial';
            ctx.textAlign = 'center';
            ctx.textBaseline = 'middle';
            ctx.fillText(`${percent}%`, 450, 150);

            // Generar la imagen final
            const buffer = canvas.toBuffer('image/png');

            // Textos dinámicos basados en el porcentaje
            let caption = '';
            if (percent < 20) {
                caption = `💔 *${percent}%* | Esto está más frío que el polo norte. Mejor quédense como amigos.`;
            } else if (percent < 50) {
                caption = `🥀 *${percent}%* | Hay algo, pero falta chispa. ¡Tienen que trabajar en ello!`;
            } else if (percent < 80) {
                caption = `💖 *${percent}%* | ¡Hacen bonita pareja! Tienen bastante potencial juntos.`;
            } else if (percent < 100) {
                caption = `🔥 *${percent}%* | ¡Amor del bueno! Están hechos el uno para el otro.`;
            } else {
                caption = `✨ *100%* | ¡ALMAS GEMELAS! ¡Cásense ya! ✨`;
            }

            // Enviar la imagen resultante
            await sock.sendMessage(jid, {
                image: buffer,
                caption: caption,
                mentions: [user1, user2]
            }, { quoted: msg });

        } catch (err) {
            console.error('❌ [love] Error:', err.message);
            await sock.sendMessage(msg.key.remoteJid, { react: { text: '⚠️', key: msg.key } });
        }
    }
};
