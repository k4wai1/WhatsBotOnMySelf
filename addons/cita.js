// addons/cita.js

const path = require('path');
const fs = require('fs');
const sharp = require('sharp');
const satori = require('satori').default || require('satori');

// 1. CARGA SÍNCRONA DE FUENTES
let fontNormalBuffer, fontBoldBuffer;
try {
  fontNormalBuffer = fs.readFileSync(path.join(__dirname, '../assets/fonts/firasanscondensed-book.otf'));
  fontBoldBuffer = fs.readFileSync(path.join(__dirname, '../assets/fonts/firasanscondensed-bold.otf'));
} catch (error) {
  console.error('❌ Error crítico: No se encontraron las fuentes .otf.');
}

// ---------------------- Funciones Auxiliares ----------------------

async function fetchProfilePictureWithTimeout(sock, pureJid, timeoutMs = 6000) {
  const controller = new AbortController();
  const timeoutId = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const urlPromise = sock.profilePictureUrl(pureJid, 'image').catch(() => sock.profilePictureUrl(pureJid, 'preview'));
    const url = await Promise.race([
      urlPromise,
      new Promise((_, reject) => setTimeout(() => reject(new Error('Timeout URL')), timeoutMs))
    ]);
    if (!url) throw new Error('No URL');
    const response = await fetch(url, { signal: controller.signal });
    clearTimeout(timeoutId);
    return Buffer.from(await response.arrayBuffer());
  } catch (error) {
    clearTimeout(timeoutId);
    return null;
  }
}

async function getRandomHopecoreImage() {
  const fsAsync = require('fs').promises;
  const hopecoreDir = path.join(__dirname, '../assets/hopecore');
  try {
    const files = await fsAsync.readdir(hopecoreDir);
    const images = files.filter(f => /\.(jpg|jpeg|png)$/i.test(f));
    if (images.length === 0) return null;
    const randomFile = images[Math.floor(Math.random() * images.length)];
    return await fsAsync.readFile(path.join(hopecoreDir, randomFile));
  } catch (err) {
    return null;
  }
}

function getOptimalFontSize(text, width, height, maxFontSize) {
  const textLength = text.length || 1;
  const boxArea = width * height;
  const targetArea = boxArea * 0.70; 
  let calculatedSize = Math.floor(Math.sqrt(targetArea / (textLength * 0.55)));
  return Math.min(calculatedSize, maxFontSize);
}

// ---------------------- Resolución de usuario Avanzada ----------------------
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

// ---------------------- Comando Principal ----------------------
module.exports = {
  commands: ['cita'],
  handler: async (sock, msg, args, store) => {
    const jid = msg.key.remoteJid;
    try {
      await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

      if (!fontNormalBuffer || !fontBoldBuffer) {
        await sock.sendMessage(jid, { text: '❌ Motor de renderizado de fuentes no disponible.' }, { quoted: msg });
        return;
      }

      // --- PARSEO DE ARGUMENTOS ---
      let isVertical = false;
      let isHorizontal = false;
      let isColor = false;
      const cleanArgs = [];

      for (const arg of args) {
        const lower = arg.toLowerCase();
        if (lower === 'v' || lower === 'vertical') isVertical = true;
        else if (lower === 'h' || lower === 'horizontal') isHorizontal = true;
        else if (lower === 'c' || lower === 'color') isColor = true;
        else cleanArgs.push(arg);
      }

      // Aleatoriedad si no se especifica orientación
      if (!isVertical && !isHorizontal) {
        isVertical = Math.random() > 0.5;
        isHorizontal = !isVertical;
      }

      // Extracción y limpieza del texto (eliminando saltos de línea)
      let quoteText = cleanArgs.join(' ').replace(/\n/g, ' ').trim();
      const quoteMatch = quoteText.match(/(["'])(.*?)\1/);
      if (quoteMatch) quoteText = quoteMatch[2];

      const contextInfo = msg.message?.extendedTextMessage?.contextInfo;
      let targetId = msg.key.participant || (msg.key.fromMe ? sock.user.id : jid);
      
      if (contextInfo?.mentionedJid?.length > 0) {
        targetId = contextInfo.mentionedJid[0];
      } else if (contextInfo?.quotedMessage) {
        targetId = contextInfo.participant || (jid.endsWith('@g.us') ? msg.key.participant : jid);
        if (!quoteText) {
          const qm = contextInfo.quotedMessage;
          quoteText = (qm.conversation || qm.extendedTextMessage?.text || '').replace(/\n/g, ' ').trim();
        }
      }

      if (!quoteText) {
        await sock.sendMessage(jid, { text: '❌ Escribe un texto para citar o responde a un mensaje.' }, { quoted: msg });
        return;
      }
      if (quoteText.length > 560) quoteText = quoteText.substring(0, 557) + '...';

      const { pureJid, userName, phoneNumber } = await resolveUser(targetId, sock, msg, store, jid);

      // Obtener Avatar
      let imgBuffer = await fetchProfilePictureWithTimeout(sock, pureJid, 6000);
      if (!imgBuffer) {
        imgBuffer = await getRandomHopecoreImage();
        if (!imgBuffer) {
          const safeName = encodeURIComponent(userName);
          const response = await fetch(`https://ui-avatars.com/api/?name=${safeName}&background=random&color=fff&size=512`);
          imgBuffer = Buffer.from(await response.arrayBuffer());
        }
      }

      // --- INICIO DE COMPOSICIÓN GRÁFICA ---
      let baseImg = sharp(imgBuffer);
      if (!isColor) baseImg = baseImg.grayscale();

      const words = `“${quoteText}”`.split(/\s+/);
      const textElements = words.map(word => ({
        type: 'span',
        props: {
          style: { fontWeight: Math.random() < 0.25 ? 700 : 400, marginRight: '8px' },
          children: word
        }
      }));

      let finalImageBuffer;

      if (isVertical) {
        // --- RENDERIZADO VERTICAL (720x960) ---
        const width = 720;
        const height = 960;

        const baseCanvas = await sharp({
          create: { width, height, channels: 4, background: { r: 0, g: 0, b: 0, alpha: 1 } }
        }).png().toBuffer();

        const avatarLayer = await baseImg.resize(720, 720, { fit: 'cover', position: 'center' }).png().toBuffer();

        const overlaySvg = Buffer.from(`
          <svg width="${width}" height="${height}">
            <defs>
              <linearGradient id="fadeV" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="black" stop-opacity="0" />
                <stop offset="100%" stop-color="black" stop-opacity="1" />
              </linearGradient>
            </defs>
            <rect x="0" y="576" width="720" height="144" fill="url(#fadeV)" />
            <rect x="0" y="720" width="720" height="240" fill="black" />
          </svg>
        `);

        const fontSize = getOptimalFontSize(quoteText, 640, 330, 60);

        const satoriSvg = await satori({
          type: 'div',
          props: {
            style: { width: '720px', height: '960px', position: 'relative', fontFamily: 'FiraSans', display: 'flex' },
            children: [
              // Caja de Cita (Absolute y Centrado Absoluto)
              {
                type: 'div',
                props: {
                  style: {
                    position: 'absolute', top: '520px', left: '40px', width: '640px', height: '330px',
                    display: 'flex', flexWrap: 'wrap', justifyContent: 'center', alignContent: 'center', alignItems: 'center',
                    color: '#ffffff', fontSize: `${fontSize}px`, textShadow: '2px 2px 8px rgba(0,0,0,0.9)', textAlign: 'center'
                  },
                  children: textElements
                }
              },
              // Footer: Nombre y Teléfono (Absolute)
              {
                type: 'div',
                props: {
                  style: {
                    position: 'absolute', top: '880px', left: '0', width: '720px',
                    display: 'flex', flexDirection: 'column', alignItems: 'center'
                  },
                  children: [
                    { type: 'span', props: { style: { fontSize: '28px', color: '#eeeeee', fontStyle: 'italic', textShadow: '1px 1px 4px rgba(0,0,0,0.8)' }, children: userName } },
                    { type: 'span', props: { style: { fontSize: '20px', color: 'rgba(255,255,255,0.6)', marginTop: '4px' }, children: phoneNumber } }
                  ]
                }
              }
            ]
          }
        }, { width, height, fonts: [{ name: 'FiraSans', data: fontNormalBuffer, weight: 400, style: 'normal' }, { name: 'FiraSans', data: fontBoldBuffer, weight: 700, style: 'normal' }], loadAdditionalAsset: async (code, segment) => (code === 'emoji' ? `https://cdnjs.cloudflare.com/ajax/libs/twemoji/14.0.2/svg/${segment}.svg` : undefined) });

        const textOverlayBuffer = await sharp(Buffer.from(satoriSvg)).png().toBuffer();

        finalImageBuffer = await sharp(baseCanvas)
          .composite([{ input: avatarLayer, top: 0, left: 0 }, { input: overlaySvg, top: 0, left: 0 }, { input: textOverlayBuffer, top: 0, left: 0 }])
          .png().toBuffer();

      } else {
        // --- RENDERIZADO HORIZONTAL (1280x720) ---
        const width = 1280;
        const height = 720;

        const baseCanvas = await sharp({
          create: { width, height, channels: 4, background: { r: 0, g: 0, b: 0, alpha: 1 } }
        }).png().toBuffer();

        const rx = 31;
        const maskSvg = Buffer.from(`<svg><rect x="0" y="0" width="620" height="620" rx="${rx}" ry="${rx}" fill="white" /></svg>`);
        
        const avatarLayer = await baseImg
          .resize(620, 620, { fit: 'cover', position: 'center' })
          .composite([{ input: maskSvg, blend: 'dest-in' }])
          .png().toBuffer();

        const overlaySvg = Buffer.from(`
          <svg width="620" height="620">
            <defs>
              <linearGradient id="fadeH" x1="1" y1="0" x2="0" y2="0">
                <stop offset="0%" stop-color="black" stop-opacity="1" />
                <stop offset="20%" stop-color="black" stop-opacity="0" />
              </linearGradient>
            </defs>
            <rect x="0" y="0" width="620" height="620" fill="url(#fadeH)" />
          </svg>
        `);

        const fontSize = getOptimalFontSize(quoteText, 630, 440, 60);

        const satoriSvg = await satori({
          type: 'div',
          props: {
            style: { width: '1280px', height: '720px', position: 'relative', fontFamily: 'FiraSans', display: 'flex' },
            children: [
              // Caja de Cita
              {
                type: 'div',
                props: {
                  style: {
                    position: 'absolute', top: '100px', left: '600px', width: '630px', height: '440px',
                    display: 'flex', flexWrap: 'wrap', justifyContent: 'center', alignContent: 'center', alignItems: 'center',
                    color: '#ffffff', fontSize: `${fontSize}px`, textShadow: '2px 2px 8px rgba(0,0,0,0.9)', textAlign: 'center'
                  },
                  children: textElements
                }
              },
              // Footer
              {
                type: 'div',
                props: {
                  style: {
                    position: 'absolute', top: '560px', left: '600px', width: '630px', 
                    display: 'flex', flexDirection: 'column', alignItems: 'center'
                  },
                  children: [
                    { type: 'span', props: { style: { fontSize: '28px', color: '#eeeeee', fontStyle: 'italic', textShadow: '1px 1px 4px rgba(0,0,0,0.8)' }, children: userName } },
                    { type: 'span', props: { style: { fontSize: '20px', color: 'rgba(255,255,255,0.6)', marginTop: '4px' }, children: phoneNumber } }
                  ]
                }
              }
            ]
          }
        }, { width, height, fonts: [{ name: 'FiraSans', data: fontNormalBuffer, weight: 400, style: 'normal' }, { name: 'FiraSans', data: fontBoldBuffer, weight: 700, style: 'normal' }], loadAdditionalAsset: async (code, segment) => (code === 'emoji' ? `https://cdnjs.cloudflare.com/ajax/libs/twemoji/14.0.2/svg/${segment}.svg` : undefined) });

        const textOverlayBuffer = await sharp(Buffer.from(satoriSvg)).png().toBuffer();

        const compositedAvatar = await sharp(avatarLayer).composite([{ input: overlaySvg, blend: 'over' }]).png().toBuffer();

        finalImageBuffer = await sharp(baseCanvas)
          .composite([
            { input: compositedAvatar, top: 50, left: 50 },
            { input: textOverlayBuffer, top: 0, left: 0 }
          ])
          .png().toBuffer();
      }

      await sock.sendMessage(jid, { image: finalImageBuffer, caption: `📸 *Cita generada*` }, { quoted: msg });
      await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });

    } catch (err) {
      console.error(err);
      await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
    }
  }
};
