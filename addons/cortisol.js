// addons/cortisol.js
// Medidor de cortisol (broma) con velocímetro dibujado en canvas.
// Portado del bot de rupa y adaptado al router de addons.
const { createCanvas, loadImage } = require('canvas');

module.exports = {
    commands: ['cor'],

    handler: async (sock, msg, args, store) => {
        const jid = msg.key.remoteJid;

        try {
            const sender = msg.key.participant || jid;

            // Resolver nombre desde el store de contactos (si existe)
            const storeName = (targetJid) => {
                try {
                    const c = store?.contacts?.[targetJid];
                    return c?.notify || c?.name || 'Usuario';
                } catch { return 'Usuario'; }
            };

            const contextInfo = msg.message?.extendedTextMessage?.contextInfo;
            const mentions = contextInfo?.mentionedJid || [];
            const quoted = contextInfo?.participant;

            // Determinar a quién estamos midiendo y su nombre
            let targetUser = sender;
            let targetName = msg.pushName || 'Usuario';

            if (mentions.length > 0) {
                targetUser = mentions[0];
                targetName = storeName(targetUser);
            } else if (quoted) {
                targetUser = quoted;
                targetName = storeName(targetUser);
            }

            await sock.sendMessage(jid, { react: { text: '🧠', key: msg.key } });

            // --- OBTENER FECHA ACTUAL (yyyy:mm:dd) ---
            const date = new Date();
            const yyyy = date.getFullYear();
            const mm = String(date.getMonth() + 1).padStart(2, '0');
            const dd = String(date.getDate()).padStart(2, '0');
            const dateString = `${yyyy}:${mm}:${dd}`;

            // Combinar usuario y fecha para el cálculo diario
            const hashInput = targetUser + dateString;

            // Cálculo determinista del porcentaje (cambia diariamente)
            let hash = 0;
            for (let i = 0; i < hashInput.length; i++) {
                hash = hashInput.charCodeAt(i) + ((hash << 5) - hash);
            }
            const percent = Math.abs(hash) % 101;

            // --- OBTENER FOTO DE PERFIL ---
            let avatarImg;
            try {
                const url = await sock.profilePictureUrl(targetUser, 'image');
                avatarImg = await loadImage(url);
            } catch {
                // Generar un avatar gris con la inicial si no tiene foto pública
                const size = 120;
                const tempCanvas = createCanvas(size, size);
                const tCtx = tempCanvas.getContext('2d');
                tCtx.fillStyle = '#cccccc';
                tCtx.fillRect(0, 0, size, size);
                tCtx.fillStyle = '#ffffff';
                tCtx.font = 'bold 60px Arial';
                tCtx.textAlign = 'center';
                tCtx.textBaseline = 'middle';
                tCtx.fillText(targetName.charAt(0).toUpperCase(), size / 2, size / 2 + 5);
                avatarImg = tempCanvas;
            }

            // --- CREACIÓN DEL VELOCÍMETRO CON CANVAS ---
            const width = 800;
            const height = 500;
            const canvas = createCanvas(width, height);
            const ctx = canvas.getContext('2d');

            // Fondo blanco
            ctx.fillStyle = '#ffffff';
            ctx.fillRect(0, 0, width, height);

            const centerX = width / 2;
            const centerY = 400;
            const radius = 220;
            const thickness = 90;

            // Título principal
            ctx.fillStyle = '#000000';
            ctx.font = 'bold 45px Arial';
            ctx.textAlign = 'center';
            ctx.fillText('CORTISOL LEVEL', centerX, 60);

            // Segmentos del medidor
            const segments = [
                { color: '#00b050', start: 1.0, end: 1.2 },
                { color: '#92d050', start: 1.2, end: 1.4 },
                { color: '#ffc000', start: 1.4, end: 1.6 },
                { color: '#ff7000', start: 1.6, end: 1.8 },
                { color: '#ff0000', start: 1.8, end: 2.0 }
            ];

            ctx.lineWidth = thickness;

            for (const seg of segments) {
                ctx.beginPath();
                ctx.arc(centerX, centerY, radius, seg.start * Math.PI, seg.end * Math.PI);
                ctx.strokeStyle = seg.color;
                ctx.stroke();
            }

            // Líneas separadoras
            ctx.lineWidth = 10;
            ctx.strokeStyle = '#ffffff';
            for (let i = 1; i <= 4; i++) {
                const angle = Math.PI + (i * 0.2 * Math.PI);
                ctx.beginPath();
                ctx.moveTo(centerX + (radius - thickness / 2) * Math.cos(angle), centerY + (radius - thickness / 2) * Math.sin(angle));
                ctx.lineTo(centerX + (radius + thickness / 2 + 2) * Math.cos(angle), centerY + (radius + thickness / 2 + 2) * Math.sin(angle));
                ctx.stroke();
            }

            // Textos del medidor
            ctx.fillStyle = '#000000';
            ctx.font = '30px Arial';
            ctx.textAlign = 'center';

            ctx.save();
            ctx.translate(centerX - 240, centerY - 140);
            ctx.rotate(-Math.PI / 4);
            ctx.fillText('LOW', 0, 0);
            ctx.restore();

            ctx.fillText('NORMAL', centerX, centerY - 290);

            ctx.save();
            ctx.translate(centerX + 240, centerY - 140);
            ctx.rotate(Math.PI / 4);
            ctx.fillText('HIGH', 0, 0);
            ctx.restore();

            // --- LA AGUJA ---
            const needleAngle = Math.PI + (percent / 100) * Math.PI;

            ctx.save();
            ctx.translate(centerX, centerY);
            ctx.rotate(needleAngle);

            ctx.beginPath();
            ctx.moveTo(0, -12);
            ctx.lineTo(radius - 30, 0);
            ctx.lineTo(0, 12);
            ctx.fillStyle = '#333333';
            ctx.fill();
            ctx.restore();

            // --- EJE CENTRAL ---
            ctx.beginPath();
            ctx.arc(centerX, centerY, 40, 0, 2 * Math.PI);
            ctx.fillStyle = '#333333';
            ctx.fill();

            ctx.beginPath();
            ctx.arc(centerX, centerY, 22, 0, 2 * Math.PI);
            ctx.fillStyle = '#ffffff';
            ctx.fill();

            ctx.beginPath();
            ctx.arc(centerX, centerY, 12, 0, 2 * Math.PI);
            ctx.fillStyle = '#333333';
            ctx.fill();

            // --- DIBUJAR FOTO DE PERFIL (ESQUINA SUPERIOR IZQUIERDA) ---
            const avSize = 100;
            const avX = 30;
            const avY = 30;

            ctx.save();
            ctx.beginPath();
            ctx.arc(avX + avSize / 2, avY + avSize / 2, avSize / 2, 0, Math.PI * 2, true);
            ctx.closePath();
            ctx.clip();
            ctx.drawImage(avatarImg, avX, avY, avSize, avSize);
            ctx.restore();

            // Borde elegante para la foto
            ctx.beginPath();
            ctx.arc(avX + avSize / 2, avY + avSize / 2, avSize / 2, 0, Math.PI * 2, true);
            ctx.lineWidth = 6;
            ctx.strokeStyle = '#e0e0e0';
            ctx.stroke();

            const buffer = canvas.toBuffer('image/png');

            // --- TEXTOS MINIMIZADOS ---
            let caption = '';
            if (percent < 20) {
                caption = `Modo Zen...`;
            } else if (percent < 40) {
                caption = `Todo chill 😌`;
            } else if (percent < 60) {
                caption = `Normal 🤔`;
            } else if (percent < 80) {
                caption = `Alerta alta 😬`;
            } else {
                caption = `Al máximo 🔥`;
            }

            const finalCaption = `*${targetName}* : *${percent}%* | ${caption}`;

            // Enviar la imagen
            await sock.sendMessage(jid, {
                image: buffer,
                caption: finalCaption
            }, { quoted: msg });

        } catch (err) {
            console.error('❌ [cor] Error:', err.message);
            await sock.sendMessage(msg.key.remoteJid, { react: { text: '⚠️', key: msg.key } });
        }
    }
};
