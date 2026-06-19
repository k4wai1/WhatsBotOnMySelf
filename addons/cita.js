// addons/cita.js

const path = require('path');
const fs = require('fs').promises;
const { createCanvas, loadImage, registerFont } = require('canvas');
const sharp = require('sharp');

try {
  registerFont(path.join(__dirname, '../assets/fonts/firasanscondensed-book.otf'), { family: 'FiraSansBook' });
  registerFont(path.join(__dirname, '../assets/fonts/firasanscondensed-italic.otf'), { family: 'FiraSansItalic' });
} catch (error) {
  console.warn('⚠️ No se pudieron cargar las fuentes personalizadas. Revisa la ruta de los archivos .otf');
}

// ---------------------- Funciones auxiliares de dibujo ----------------------
function roundRect(ctx, x, y, w, h, r) {
  if (w < 2 * r) r = w / 2;
  if (h < 2 * r) r = h / 2;
  ctx.beginPath();
  ctx.moveTo(x + r, y);
  ctx.lineTo(x + w - r, y);
  ctx.quadraticCurveTo(x + w, y, x + w, y + r);
  ctx.lineTo(x + w, y + h - r);
  ctx.quadraticCurveTo(x + w, y + h, x + w - r, y + h);
  ctx.lineTo(x + r, y + h);
  ctx.quadraticCurveTo(x, y + h, x, y + h - r);
  ctx.lineTo(x, y + r);
  ctx.quadraticCurveTo(x, y, x + r, y);
  ctx.closePath();
  return ctx;
}

function wrapText(ctx, text, maxWidth) {
  const words = text.split(' ');
  const lines = [];
  let currentLine = '';
  for (const word of words) {
    const testLine = currentLine ? `${currentLine} ${word}` : word;
    const metrics = ctx.measureText(testLine);
    if (metrics.width > maxWidth && currentLine) {
      lines.push(currentLine);
      currentLine = word;
    } else {
      currentLine = testLine;
    }
  }
  if (currentLine) lines.push(currentLine);
  return lines;
}

function drawCenteredParagraph(ctx, lines, yStart, lineHeight, centerX) {
  let y = yStart;
  for (const line of lines) {
    ctx.fillText(line, centerX, y);
    y += lineHeight;
  }
  return y;
}

// ---------------------- Resolución de usuario (LID y nombres) ----------------------
async function resolveUser(targetIdRaw, sock, msg, store, chatJid) {
  let targetId = targetIdRaw;
  let pureJid = '';
  let userName = '~';
  let phoneNumber = 'desconocido';

  if (targetId.includes('@lid')) {
    // Caso especial: el propio bot
    const botLid = sock.authState?.creds?.me?.lid;
    if (botLid && targetId === botLid) {
      const botId = sock.user?.id || sock.authState?.creds?.me?.id;
      if (botId) {
        pureJid = botId.split(':')[0] + '@s.whatsapp.net';
        phoneNumber = pureJid.split('@')[0];
        userName = sock.user?.name || msg.pushName || 'Bot';
        return { pureJid, userName, phoneNumber };
      }
    }

    // Buscar en metadatos del grupo
    if (chatJid.endsWith('@g.us')) {
      try {
        const metadata = await sock.groupMetadata(chatJid);
        const participante = metadata.participants.find(p => p.lid === targetId || p.id === targetId);
        if (participante && participante.id && participante.id.endsWith('@s.whatsapp.net')) {
          pureJid = participante.id;
          phoneNumber = pureJid.split('@')[0];
          const contact = store?.contacts?.[pureJid];
          if (contact) {
            userName = contact.verifiedName || contact.name || contact.notify || phoneNumber;
          } else {
            userName = phoneNumber;
          }
          return { pureJid, userName, phoneNumber };
        }
      } catch (e) { /* silencioso */ }
    }

    // Búsqueda exhaustiva en store.contacts
    if (store?.contacts) {
      let foundContact = null;
      let foundKey = null;
      if (store.contacts[targetId]) {
        foundContact = store.contacts[targetId];
        foundKey = targetId;
      } else {
        for (const [key, contact] of Object.entries(store.contacts)) {
          if (contact.lid === targetId || contact.id === targetId) {
            foundContact = contact;
            foundKey = key;
            break;
          }
        }
      }
      if (foundContact) {
        let realNumber = foundContact.pn;
        if (!realNumber && foundKey && foundKey.endsWith('@s.whatsapp.net')) {
          realNumber = foundKey.split('@')[0];
        }
        if (realNumber) {
          pureJid = `${realNumber}@s.whatsapp.net`;
          phoneNumber = realNumber;
          userName = foundContact.verifiedName || foundContact.name || foundContact.notify || phoneNumber;
          return { pureJid, userName, phoneNumber };
        }
      }
    }

    // Si no se pudo resolver, mantener como está
    pureJid = targetId;
    phoneNumber = targetId.split('@')[0];
    userName = 'Usuario Oculto';
  } else {
    pureJid = targetId;
    phoneNumber = targetId.split('@')[0];
  }

  // Obtener nombre desde store si es posible
  if (pureJid && store?.contacts) {
    const contact = store.contacts[pureJid] || store.contacts[targetId];
    if (contact) {
      userName = contact.verifiedName || contact.name || contact.notify || userName;
    }
  }

  const isSelf = (phoneNumber === sock.user?.id?.split(':')[0]);
  if (userName === '~' && isSelf) {
    userName = msg.pushName || sock.user?.name || 'Luis';
  }

  if (userName === '~') {
    userName = phoneNumber === 'desconocido' ? '~Anónimo~' : phoneNumber;
  }

  return { pureJid, userName, phoneNumber };
}

// ---------------------- Obtener imagen aleatoria de la carpeta hopecore ----------------------
async function getRandomHopecoreImage() {
  const hopecoreDir = path.join(__dirname, '../assets/hopecore');
  try {
    const files = await fs.readdir(hopecoreDir);
    const images = files.filter(f => /\.(jpg|jpeg|png)$/i.test(f));
    if (images.length === 0) return null;
    const randomFile = images[Math.floor(Math.random() * images.length)];
    const filePath = path.join(hopecoreDir, randomFile);
    const buffer = await fs.readFile(filePath);
    return buffer;
  } catch (err) {
    console.warn('⚠️ No se pudo leer la carpeta hopecore:', err.message);
    return null;
  }
}

// ---------------------- Comando principal ----------------------
module.exports = {
  commands: ['cita'],
  handler: async (sock, msg, args, store) => {
    const jid = msg.key.remoteJid;
    try {
      await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

      // 1. Determinar el ID objetivo (quién es citado)
      let targetId;
      const contextInfo = msg.message?.extendedTextMessage?.contextInfo;
      const isGroup = jid.endsWith('@g.us');

      if (contextInfo?.mentionedJid?.length > 0) {
        targetId = contextInfo.mentionedJid[0];
      } else if (contextInfo?.quotedMessage) {
        targetId = contextInfo.participant || (isGroup ? msg.key.participant : jid);
      } else {
        targetId = msg.key.participant || (msg.key.fromMe ? sock.user.id : jid);
      }

      // 2. Resolver usuario (número real + nombre)
      const { pureJid, userName, phoneNumber } = await resolveUser(targetId, sock, msg, store, jid);

      // 3. Obtener imagen (foto de perfil o fallback a hopecore o iniciales)
      let imgBuffer;
      let ppUrl = null;

      // Intento de foto de perfil real
      try {
        ppUrl = await sock.profilePictureUrl(pureJid, 'image');
      } catch (e1) {
        try {
          ppUrl = await sock.profilePictureUrl(pureJid, 'preview');
        } catch (e2) {
          // No hay foto de perfil
        }
      }

      if (ppUrl) {
        // Si hay foto real, la usamos
        const response = await fetch(ppUrl);
        imgBuffer = Buffer.from(await response.arrayBuffer());
      } else {
        // No hay foto -> intentar usar una imagen aleatoria de hopecore
        const hopecoreBuffer = await getRandomHopecoreImage();
        if (hopecoreBuffer) {
          imgBuffer = hopecoreBuffer;
          console.log('✨ Usando imagen aleatoria de hopecore como fallback');
        } else {
          // Último recurso: avatar con iniciales
          const safeName = encodeURIComponent(userName);
          const fallbackUrl = `https://ui-avatars.com/api/?name=${safeName}&background=random&color=fff&size=512`;
          const response = await fetch(fallbackUrl);
          imgBuffer = Buffer.from(await response.arrayBuffer());
          console.log('⚠️ Sin hopecore, usando avatar con iniciales');
        }
      }

      // 4. Parsear argumentos y texto de cita
      const argsStr = args.join(' ');
      const isVertical = argsStr.includes('--vertical') || argsStr.includes('-v');
      const isColor = argsStr.includes('--color');

      let quoteText = '';
      const doubleQuoteMatch = argsStr.match(/"([^"]*)"/);
      const singleQuoteMatch = argsStr.match(/'([^']*)'/);
      if (doubleQuoteMatch) quoteText = doubleQuoteMatch[1];
      else if (singleQuoteMatch) quoteText = singleQuoteMatch[1];
      else {
        let rest = argsStr
          .replace(/--vertical/g, '')
          .replace(/-v/g, '')
          .replace(/--color/g, '')
          .trim();
        if (rest) quoteText = rest;
      }

      const quotedMsg = msg.message?.extendedTextMessage?.contextInfo?.quotedMessage;
      if (!quoteText && quotedMsg) {
        quoteText = quotedMsg.conversation || quotedMsg.extendedTextMessage?.text || '';
      }

      if (!quoteText) {
        await sock.sendMessage(jid, { text: '❌ Debes escribir un texto para citar o responder a un mensaje.' }, { quoted: msg });
        await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
        return;
      }

      const MAX_CHARS = 560;
      if (quoteText.length > MAX_CHARS) {
        quoteText = quoteText.substring(0, MAX_CHARS - 3) + '...';
      }

      // 5. Aplicar escala de grises si no hay --color
      if (!isColor) {
        imgBuffer = await sharp(imgBuffer).grayscale().toBuffer();
      }

      // 6. Configurar canvas según orientación
      let width, height, avatarSize, avatarX, avatarY;
      if (isVertical) {
        width = 720;
        height = 960;
        avatarSize = width;
        avatarX = 0;
        avatarY = 0;
      } else {
        width = 1280;
        height = 720;
        avatarSize = 400;
        avatarX = 40;
        avatarY = (height - avatarSize) / 2;
      }

      const canvas = createCanvas(width, height);
      const ctx = canvas.getContext('2d');

      // Fondo negro (se verá detrás de la imagen si no cubre todo)
      ctx.fillStyle = '#000000';
      ctx.fillRect(0, 0, width, height);

      // Cargar y dibujar la imagen (foto real, hopecore o iniciales)
      const avatarImage = await loadImage(imgBuffer);
      if (isVertical) {
        // La imagen ocupa todo el ancho y la parte superior
        ctx.drawImage(avatarImage, avatarX, avatarY, width, avatarSize);
      } else {
        ctx.save();
        roundRect(ctx, avatarX, avatarY, avatarSize, avatarSize, 20);
        ctx.clip();
        ctx.drawImage(avatarImage, avatarX, avatarY, avatarSize, avatarSize);
        ctx.restore();
      }

      // Gradiente para la zona de texto (oscurece la imagen gradualmente)
      if (isVertical) {
        const gradient = ctx.createLinearGradient(0, 0, 0, avatarSize);
        gradient.addColorStop(0, 'rgba(0,0,0,0)');
        gradient.addColorStop(0.5, 'rgba(0,0,0,0.2)');
        gradient.addColorStop(1, 'rgba(0,0,0,1)');
        ctx.fillStyle = gradient;
        ctx.fillRect(0, 0, width, avatarSize);
      } else {
        const gradientStart = width * 0.8;
        const gradient = ctx.createLinearGradient(gradientStart, 0, width, 0);
        gradient.addColorStop(0, 'rgba(0,0,0,0)');
        gradient.addColorStop(0.5, 'rgba(0,0,0,0.7)');
        gradient.addColorStop(1, 'rgba(0,0,0,0.95)');
        ctx.fillStyle = gradient;
        ctx.fillRect(gradientStart, 0, width - gradientStart, height);
      }

      // 7. Renderizar texto (cita, nombre y número)
      const quotedFullText = `“${quoteText}”`;
      let fontSizeTitle = 60;
      let lineHeight = 0;
      let quoteLines = [];
      let centerX, startY;

      const nameMargin = 20;
      const phoneMargin = 10;
      let fontSizeName = isVertical ? 28 : 24;
      let fontSizePhone = isVertical ? 20 : 18;
      let nameY, phoneY;

      ctx.textBaseline = 'top';
      ctx.textAlign = 'center';

      if (isVertical) {
        const bottomPadding = 40;
        phoneY = height - bottomPadding - fontSizePhone;
        nameY = phoneY - phoneMargin - fontSizeName;

        const boxTop = height * 0.60;
        const boxHeight = nameY - 20 - boxTop;
        const boxLeftMargin = width * 0.10;
        const maxQuoteWidth = width - (boxLeftMargin * 2);
        centerX = width / 2;

        let fits = false;
        while (!fits && fontSizeTitle > 10) {
          ctx.font = `${fontSizeTitle}px "FiraSansBook", "Segoe UI", sans-serif`;
          lineHeight = fontSizeTitle * 1.3;
          quoteLines = wrapText(ctx, quotedFullText, maxQuoteWidth);
          const quoteHeight = quoteLines.length * lineHeight;
          if (quoteHeight <= boxHeight) fits = true;
          else fontSizeTitle--;
        }
        const finalQuoteHeight = quoteLines.length * lineHeight;
        startY = boxTop + (boxHeight - finalQuoteHeight) / 2;
      } else {
        const textAreaXMin = avatarX + avatarSize + 40;
        const textAreaXMax = width - 40;
        const maxQuoteWidth = textAreaXMax - textAreaXMin;
        centerX = textAreaXMin + maxQuoteWidth / 2;
        fontSizeTitle = 34;
        ctx.font = `${fontSizeTitle}px "FiraSansBook", "Segoe UI", sans-serif`;
        lineHeight = fontSizeTitle * 1.3;
        quoteLines = wrapText(ctx, quotedFullText, maxQuoteWidth);
        const quoteHeight = quoteLines.length * lineHeight;
        const totalTextHeight = quoteHeight + nameMargin + fontSizeName + phoneMargin + fontSizePhone;
        startY = (height - totalTextHeight) / 2;
      }

      ctx.fillStyle = '#ffffff';
      ctx.font = `${fontSizeTitle}px "FiraSansBook", "Segoe UI", sans-serif`;
      let currentY = drawCenteredParagraph(ctx, quoteLines, startY, lineHeight, centerX);

      if (isVertical) currentY = nameY;
      else currentY += nameMargin;
      ctx.font = `${fontSizeName}px "FiraSansItalic", "Segoe UI", sans-serif`;
      ctx.fillStyle = '#eeeeee';
      ctx.fillText(userName, centerX, currentY);

      if (isVertical) currentY = phoneY;
      else currentY += fontSizeName + phoneMargin;
      ctx.font = `${fontSizePhone}px "FiraSansBook", "Segoe UI", sans-serif`;
      ctx.fillStyle = 'rgba(255,255,255,0.5)';
      ctx.fillText(phoneNumber, centerX, currentY);

      // 8. Enviar imagen
      const finalBuffer = canvas.toBuffer('image/png');
      await sock.sendMessage(jid, { image: finalBuffer, caption: `📸 *Cita generada*` }, { quoted: msg });
      await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

      console.log('\n╭─── [CITA COMPLETADA EXITOSAMENTE] ───');
      console.log(`│ 👤 Usuario: ${userName}`);
      console.log(`│ 📱 Teléfono: ${phoneNumber}`);
      console.log(`│ 💬 Texto: "${quoteText.length > 50 ? quoteText.substring(0, 50) + '...' : quoteText}"`);
      console.log(`│ 📏 Formato: ${isVertical ? 'Vertical' : 'Horizontal'} | Color: ${isColor ? 'Sí' : 'No'}`);
      console.log('╰───────────────────────────────────────\n');

    } catch (err) {
      console.error('Error crítico en addon .cita:', err);
      await sock.sendMessage(msg.key.remoteJid, { react: { text: '❌', key: msg.key } });
    }
  }
};
