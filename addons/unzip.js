// addons/unzip.js
// .unzip — descarga un .zip (adjunto en el comando o citado), lo descomprime en el VPS
// y devuelve los archivos del nivel raíz como documentos. Si hay más de 7 archivos,
// todo el conjunto se envía al chat privado del usuario, con un tope de 40 archivos.
const fs = require('fs');
const path = require('path');
const os = require('os');
const crypto = require('crypto');
const { execFile } = require('child_process');
const { promisify } = require('util');
const { downloadContentFromMessage } = require('@whiskeysockets/baileys');

const execFileP = promisify(execFile);

const MAX_ZIP_BYTES = 300 * 1024 * 1024;        // límite del archivador comprimido
const MAX_UNCOMPRESSED = 2 * 1024 * 1024 * 1024; // guardia de disco: contenido descomprimido
const PRIVATE_THRESHOLD = 7;                     // >7 archivos → chat privado
const MAX_FILES = 40;                            // tope para no saturar la sección

function getDocument(msg) {
    return msg.message?.documentMessage ||
           msg.message?.extendedTextMessage?.contextInfo?.quotedMessage?.documentMessage || null;
}

function isZip(doc) {
    const name = (doc.fileName || '').toLowerCase();
    const mime = (doc.mimetype || '').toLowerCase();
    return name.endsWith('.zip') || mime.includes('zip');
}

function getSenderJid(msg) {
    const raw = msg.key.participant || msg.key.remoteJid;
    return raw.split('@')[0].split(':')[0] + '@s.whatsapp.net';
}

async function downloadToBuffer(doc) {
    const stream = await downloadContentFromMessage(doc, 'document');
    const chunks = [];
    for await (const chunk of stream) chunks.push(chunk);
    return Buffer.concat(chunks);
}

async function uncompressedTotal(zipPath) {
    try {
        const { stdout } = await execFileP('unzip', ['-l', zipPath]);
        const m = stdout.match(/(\d+)\s+bytes?\s+uncompressed/);
        return m ? parseInt(m[1], 10) : null;
    } catch (_) {
        return null;
    }
}

async function extract(zipPath, outDir) {
    await execFileP('unzip', ['-qq', '-o', zipPath, '-d', outDir]);
}

async function sendDocument(sock, to, filePath, quotedMsg) {
    const buf = fs.readFileSync(filePath);
    const payload = {
        document: buf,
        fileName: path.basename(filePath),
        mimetype: 'application/octet-stream'
    };
    const extra = quotedMsg ? { quoted: quotedMsg } : {};
    await sock.sendMessage(to, payload, extra);
}

module.exports = {
    commands: ['unzip'],

    handler: async (sock, msg) => {
        const jid = msg.key.remoteJid;

        try {
            const doc = getDocument(msg);
            if (!doc) {
                await sock.sendMessage(jid, { react: { text: '❓', key: msg.key } });
                await sock.sendMessage(jid, {
                    text: '❌ Envía el comando .unzip como *caption de un .zip* o responde a un mensaje con un .zip citado.'
                }, { quoted: msg });
                return;
            }

            if (!isZip(doc)) {
                await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                await sock.sendMessage(jid, { text: '❌ Solo acepto archivadores .zip.' }, { quoted: msg });
                return;
            }

            const declared = Number(doc.fileLength || 0);
            if (declared > MAX_ZIP_BYTES) {
                await sock.sendMessage(jid, { react: { text: '🚫', key: msg.key } });
                await sock.sendMessage(jid, {
                    text: `🚫 El archivo pesa ${(declared / (1024 * 1024)).toFixed(1)} MB. El límite es 300 MB.`
                }, { quoted: msg });
                return;
            }

            await sock.sendMessage(jid, { react: { text: '⏳', key: msg.key } });

            const tmpRoot = path.join(os.tmpdir(), `wboms-unzip-${process.pid}-${crypto.randomBytes(4).toString('hex')}`);
            fs.mkdirSync(tmpRoot, { recursive: true });

            let outDir;
            let zipPath;
            try {
                const buffer = await downloadToBuffer(doc);
                if (buffer.length > MAX_ZIP_BYTES) {
                    throw new Error(`El archivo pesa ${(buffer.length / (1024 * 1024)).toFixed(1)} MB. El límite es 300 MB.`);
                }

                zipPath = path.join(tmpRoot, 'entrada.zip');
                outDir = path.join(tmpRoot, 'salida');
                fs.writeFileSync(zipPath, buffer);
                fs.mkdirSync(outDir, { recursive: true });

                const total = await uncompressedTotal(zipPath);
                if (total !== null && total > MAX_UNCOMPRESSED) {
                    throw new Error('El contenido descomprimido excede el límite de seguridad del servidor.');
                }

                await extract(zipPath, outDir);

                const files = fs.readdirSync(outDir, { withFileTypes: true })
                    .filter(d => d.isFile())
                    .map(d => path.join(outDir, d.name))
                    .sort();

                if (!files.length) {
                    throw new Error('No hay archivos sueltos en la raíz del .zip (solo carpetas).');
                }

                const overflow = files.length - MAX_FILES;
                const toSend = overflow > 0 ? files.slice(0, MAX_FILES) : files;
                const privateMode = toSend.length > PRIVATE_THRESHOLD;
                const target = privateMode ? getSenderJid(msg) : jid;

                for (const f of toSend) {
                    await sendDocument(sock, target, f, privateMode ? null : msg);
                }

                if (privateMode) {
                    const slack = [];
                    slack.push(`📦 *${toSend.length} archivos* descomprimidos de ${doc.fileName || 'el .zip'}.`);
                    if (overflow > 0) {
                        slack.push(`⚠️ ${overflow} archivo(s) adicional(es) quedaron fuera del tope de ${MAX_FILES}.`);
                    }
                    slack.push('📨 Enviados a tu chat privado por ser más de 7 archivos.');
                    await sock.sendMessage(jid, { text: slack.join('\n') }, { quoted: msg });
                }

                await sock.sendMessage(jid, { react: { text: '✅', key: msg.key } });
            } finally {
                fs.rmSync(tmpRoot, { recursive: true, force: true });
            }
        } catch (error) {
            console.error(`❌ Error en addon [unzip]:`, error);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } }).catch(() => {});
            await sock.sendMessage(jid, { text: `❌ No pude completar la extracción: ${error.message}` }, { quoted: msg }).catch(() => {});
        }
    }
};
