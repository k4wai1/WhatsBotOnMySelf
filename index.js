const {
    default: makeWASocket,
    useMultiFileAuthState,
    DisconnectReason,
    fetchLatestBaileysVersion
} = require('@whiskeysockets/baileys');
const pino = require('pino');
const fs = require('fs');
const path = require('path');
const qrcode = require('qrcode-terminal');

require('dotenv').config();
require('events').EventEmitter.defaultMaxListeners = 0;

const CONFIG_PATH = path.join(__dirname, 'assets', 'config.json');

// ─── Configuración persistente (prefixes) ──────────────────────────────────
function loadPrefixes() {
    try {
        const raw = fs.readFileSync(CONFIG_PATH, 'utf-8');
        const cfg = JSON.parse(raw);
        if (Array.isArray(cfg.prefixes) && cfg.prefixes.length) return cfg.prefixes;
    } catch (_) {}
    return [',', '!', '/'];
}

function savePrefixes(list) {
    try {
        fs.writeFileSync(CONFIG_PATH, JSON.stringify({ prefixes: list }, null, 2));
    } catch (e) {
        console.error('❌ No se pudo guardar config.json:', e.message);
    }
}

// Garantizar que config.json exista desde el inicio
if (!fs.existsSync(CONFIG_PATH)) {
    savePrefixes([',', '!', '/']);
}

// ─── Store Personalizado (Solución definitiva para Baileys 2026) ─────────────
// Suple la eliminación de makeInMemoryStore capturando la información nativamente.
const store = {
    contacts: {}
};

const CACHE_PATH = path.join(__dirname, 'baileys_store_cache.json');
try {
    if (fs.existsSync(CACHE_PATH)) {
        store.contacts = JSON.parse(fs.readFileSync(CACHE_PATH, 'utf-8'));
    }
} catch (_) {}

// Guardar caché en disco de manera asíncrona cada 10 segundos
setInterval(() => {
    try {
        fs.writeFileSync(CACHE_PATH, JSON.stringify(store.contacts, null, 2));
    } catch (_) {}
}, 10_000);

// Configuración de enrutamiento
const commandRegistry = new Map();

async function startBot() {
    const { state, saveCreds } = await useMultiFileAuthState('auth_info_baileys');
    const { version } = await fetchLatestBaileysVersion();

    const sock = makeWASocket({
        version,
        logger: pino({ level: 'silent' }),
        auth: state,
        printQRInTerminal: false,
        generateHighQualityLinkPreview: true
    });

    sock.ev.on('creds.update', saveCreds);

    // Interceptar sincronizaciones de contactos iniciales y grupales
    sock.ev.on('contacts.upsert', (contacts) => {
        for (const contact of contacts) {
            if (contact.id) {
                if (!store.contacts[contact.id]) store.contacts[contact.id] = {};
                Object.assign(store.contacts[contact.id], contact);
            }
        }
    });

    sock.ev.on('contacts.update', (updates) => {
        for (const update of updates) {
            if (update.id) {
                if (!store.contacts[update.id]) store.contacts[update.id] = {};
                Object.assign(store.contacts[update.id], update);
            }
        }
    });

    sock.ev.on('connection.update', (update) => {
        const { connection, lastDisconnect, qr } = update;

        if (qr) {
            console.log('\n📱 ESCANEA ESTE CÓDIGO QR PARA INICIAR SESIÓN:');
            qrcode.generate(qr, { small: true });
        }

        if (connection === 'close') {
            const shouldReconnect = lastDisconnect?.error?.output?.statusCode !== DisconnectReason.loggedOut;
            console.log('❌ Conexión cerrada. ¿Debe reconectar?', shouldReconnect);
            if (shouldReconnect) startBot();
        } else if (connection === 'open') {
            console.log('✅ Enlace establecido. Motor en línea.');
        }
    });

    // Cargador Dinámico de Addons
    const addonsDir = path.join(__dirname, 'addons');
    if (fs.existsSync(addonsDir)) {
        const files = fs.readdirSync(addonsDir).filter(f => f.endsWith('.js'));
        for (const file of files) {
            try {
                const addon = require(path.join(addonsDir, file));

                // 1. Registra los comandos normales (Ej: .cita)
                if (addon.commands && typeof addon.handler === 'function') {
                    const cmds = Array.isArray(addon.commands) ? addon.commands : [addon.commands];
                    cmds.forEach(cmd => commandRegistry.set(cmd.toLowerCase(), addon.handler));
                    console.log(`🔌 Addon acoplado: ${file} (Rutas: ${cmds.join(', ')})`);
                }

                // 2. Inicia los procesos silenciosos en segundo plano pasando el store nuevo
                if (typeof addon.init === 'function') {
                    addon.init(sock, store);
                    console.log(`⚙️ Proceso en segundo plano iniciado: ${file}`);
                }
            } catch (err) {
                console.error(`❌ Fallo crítico al cargar addon ${file}:`, err.message);
            }
        }
    } else {
        fs.mkdirSync(addonsDir, { recursive: true });
    }

    // Interceptor Global y Enrutador
    sock.ev.on('messages.upsert', async (m) => {
        if (m.type !== 'notify') return;
        const msg = m.messages[0];
        if (!msg.message) return;

        // Auto-alimentar el almacén con el pushName del usuario en cada mensaje que entra
        const sender = msg.key.participant || msg.key.remoteJid;
        if (sender && msg.pushName) {
            const pureSenderJid = sender.split('@')[0].split(':')[0] + '@s.whatsapp.net';
            if (!store.contacts[pureSenderJid]) store.contacts[pureSenderJid] = {};
            store.contacts[pureSenderJid].id = pureSenderJid;
            store.contacts[pureSenderJid].notify = msg.pushName;
        }

        const text = msg.message.conversation ||
                     msg.message.extendedTextMessage?.text ||
                     msg.message.imageMessage?.caption ||
                     msg.message.videoMessage?.caption ||
                     msg.message.documentMessage?.caption || '';

        if (!text) return;

        // .restart / !restart / ,restart — funciona con cualquier prefix
        const prefixes = loadPrefixes();
        if (prefixes.some(p => text.trim() === p + 'restart')) {
            await sock.sendMessage(msg.key.remoteJid, { react: { text: '🔄', key: msg.key } });
            console.log('🔄 Señal de reinicio recibida.');
            process.exit(1);
        }

        const usedPrefix = prefixes.find(p => text.startsWith(p));
        if (!usedPrefix) return;

        const args = text.slice(usedPrefix.length).trim().split(/\s+/);
        const command = args.shift().toLowerCase();

        const handler = commandRegistry.get(command);
        if (handler) {
            try {
                // Inyección del store nativo como 4to argumento
                await handler(sock, msg, args, store);
            } catch (error) {
                console.error(`❌ Error en addon [${command}]:`, error);
                await sock.sendMessage(msg.key.remoteJid, { react: { text: '❌', key: msg.key } });
            }
        }
    });
}

startBot().catch(console.error);
