// addons/rem.js
// Recordatorios persistentes con múltiples formatos de hora.
// .rem <tiempo> <texto> | .rem list | .rem del N | .rem clear | .rem help

const fs = require('fs');
const path = require('path');

const STORE_PATH = path.join(__dirname, '..', 'assets', 'reminders.json');
const MIN_MS = 5 * 1000;                          // mínimo 5 segundos
const MAX_MS = 5 * 365 * 24 * 60 * 60 * 1000;     // máximo 5 años
const MAX_TEXT = 500;
const MAX_PER_CHAT = 50;
const LATE_GRACE = 48 * 60 * 60 * 1000;           // entregar atrasados hasta 48h
const CHUNK = 2 ** 30;                            // trocear timeouts larguísimos

let store = { nextId: 1, items: [] };
let timers = new Map(); // id -> timeout
let currentSock = null;

// ─── Persistencia atómica ───────────────────────────────────────────────────
function loadStore() {
    try {
        if (!fs.existsSync(STORE_PATH)) return;
        const raw = JSON.parse(fs.readFileSync(STORE_PATH, 'utf-8'));
        if (raw && Array.isArray(raw.items)) {
            store.nextId = Number(raw.nextId) || 1;
            store.items = raw.items.filter(it =>
                it && Number.isFinite(it.at) && Number.isFinite(it.id) &&
                typeof it.text === 'string' && typeof it.chat === 'string'
            );
        }
    } catch (e) {
        console.error('❌ [rem] store corrupto, iniciando vacío:', e.message);
        store = { nextId: 1, items: [] };
    }
}

function saveStore() {
    try {
        const tmp = STORE_PATH + '.tmp';
        fs.writeFileSync(tmp, JSON.stringify(store, null, 2));
        fs.renameSync(tmp, STORE_PATH);
    } catch (e) {
        console.error('❌ [rem] no se pudo guardar:', e.message);
    }
}

// ─── Parser de tiempo ───────────────────────────────────────────────────────
const UNIT_MS = { s: 1000, m: 60000, h: 3600000, d: 86400000 };

const REL_FULL = /^\s*(?:en\s+)?((?:\d{1,6}\s*(?:d(?:[ií]as?)?|h(?:rs?|oras?)?|m(?:ins?|inutos?)?|s(?:egs?|egundos?)?)\s*)+)(?=\s|$)/i;
const UNIT_SCAN = /(\d{1,6})\s*(d(?:[ií]as?)?|h(?:rs?|oras?)?|m(?:ins?|inutos?)?|s(?:egs?|egundos?)?)/gi;

function unitToMs(numStr, unitRaw) {
    const n = parseInt(numStr, 10);
    if (!Number.isFinite(n) || n <= 0) return null;
    const u = unitRaw.toLowerCase();
    let key;
    if (/^d/.test(u)) key = 'd';
    else if (/^h/.test(u)) key = 'h';
    else if (/^m/.test(u)) key = 'm';
    else if (/^s/.test(u)) key = 's';
    else return null;
    return n * UNIT_MS[key];
}

function sumUnits(expr) {
    let total = 0;
    let m;
    UNIT_SCAN.lastIndex = 0;
    while ((m = UNIT_SCAN.exec(expr)) !== null) {
        const ms = unitToMs(m[1], m[2]);
        if (ms === null) return null;
        total += ms;
    }
    return total;
}

/**
 * Texto crudo tras el comando →
 *   { at, text }       interpretado
 *   { needTime, text } dijo "mañana/hoy" sin hora
 *   null               nada entendible
 * Relativo: 30s | 10m | 2h | 3d | 1h30m | "1h 30m" | "2 dias 4 horas" | en 15 min
 * Absoluto: [mañana|hoy] HH[:]MM [am|pm]
 */
function parseWhen(rawArgs) {
    const raw = String(rawArgs || '').trim();
    if (!raw) return null;

    // 1) Relativo
    const relMatch = raw.match(REL_FULL);
    if (relMatch) {
        const totalMs = sumUnits(relMatch[1]);
        if (totalMs === null || totalMs <= 0) return null;
        return { at: Date.now() + totalMs, text: raw.slice(relMatch[0].length).trim() };
    }

    // 2) Absoluto
    let rest = raw;
    let dayOffset = 0;
    const dayMatch = rest.match(/^\s*(ma[ñn]ana|tmr|hoy|today)\b/i);
    if (dayMatch) {
        const w = dayMatch[1].toLowerCase();
        dayOffset = (w === 'hoy' || w === 'today') ? 0 : 1;
        rest = rest.slice(dayMatch[0].length);
    }
    const timeMatch = rest.match(/^\s*(\d{1,2}(?:[:.]\d{1,2})?\s*(?:am|pm)?)(?:\s+|$)/i);
    if (!timeMatch) {
        if (dayMatch) return { needTime: true, text: rest.trim() };
        return null;
    }

    const tParts = timeMatch[1].trim().match(/^(\d{1,2})(?:[:.](\d{1,2}))?(?:\s*(am|pm))?$/i);
    if (!tParts) return null;

    let hh = parseInt(tParts[1], 10);
    const mm = tParts[2] ? parseInt(tParts[2], 10) : 0;
    const mer = tParts[3]?.toLowerCase();

    if (mer) {
        if (hh < 1 || hh > 12) return null;
        if (hh === 12) hh = 0;
        if (mer === 'pm') hh += 12;
    } else if (hh > 23) {
        return null;
    }
    if (mm > 59) return null;

    const d = new Date();
    d.setHours(hh, mm, 0, 0);
    d.setDate(d.getDate() + dayOffset);
    if (dayOffset === 0 && d.getTime() <= Date.now()) d.setDate(d.getDate() + 1);

    return { at: d.getTime(), text: rest.slice(timeMatch[0].length).trim() };
}

// ─── Formato legible ────────────────────────────────────────────────────────
const DIAS = ['dom', 'lun', 'mar', 'mié', 'jue', 'vie', 'sáb'];
const MESES = ['ene', 'feb', 'mar', 'abr', 'may', 'jun', 'jul', 'ago', 'sep', 'oct', 'nov', 'dic'];

function fmtDate(ms) {
    const d = new Date(ms);
    const hh = String(d.getHours()).padStart(2, '0');
    const mm = String(d.getMinutes()).padStart(2, '0');
    const hoy = new Date();
    const manana = new Date(hoy); manana.setDate(hoy.getDate() + 1);
    const sameDay = (a, b) => a.toDateString() === b.toDateString();
    const prefijo = sameDay(d, hoy) ? 'hoy' : sameDay(d, manana) ? 'mañana'
        : `${DIAS[d.getDay()]} ${d.getDate()} ${MESES[d.getMonth()]}`;
    return `${prefijo} a las ${hh}:${mm}`;
}

function fmtDur(ms) {
    const s = Math.floor(ms / 1000);
    const d = Math.floor(s / 86400);
    const h = Math.floor((s % 86400) / 3600);
    const m = Math.floor((s % 3600) / 60);
    const seg = s % 60;
    const parts = [];
    if (d) parts.push(`${d}d`);
    if (h) parts.push(`${h}h`);
    if (m) parts.push(`${m}m`);
    if (seg && !d && !h) parts.push(`${seg}s`);
    return parts.join(' ') || '0s';
}

const HELP = `⏰ *Recordatorios*

*Crear:*
• \`.rem 30m llamar a mamá\`
• \`.rem 2h tomar agua\`
• \`.rem 1h45m sacar la comida\`
• \`.rem 17:30 reunión\` (si ya pasó → mañana)
• \`.rem 9pm serie nueva\`
• \`.rem mañana 8am gym\`

*Gestionar:*
• \`.rem list\` — ver pendientes
• \`.rem del N\` — borrar el #N
• \`.rem clear\` — borrar todos`;

// ─── Programación y disparo ────────────────────────────────────────────────
function scheduleItem(item) {
    const delay = item.at - Date.now();
    if (delay <= 0) return fireItem(item, false);
    const effective = Math.min(delay, CHUNK);
    timers.set(item.id, setTimeout(() => {
        timers.delete(item.id);
        if (delay > CHUNK) scheduleItem(item);
        else fireItem(item, false);
    }, effective));
}

async function fireItem(item, late) {
    store.items = store.items.filter(i => i.id !== item.id);
    saveStore();
    if (!currentSock) return;
    try {
        await currentSock.sendMessage(item.chat, {
            text: `⏰ *Recordatorio:* ${item.text}${late ? `\n_(atrasado ${fmtDur(Date.now() - item.at)})_` : ''}`
        });
    } catch (e) {
        console.error(`❌ [rem] no se pudo entregar #${item.id}:`, e.message);
    }
}

module.exports = {
    commands: ['rem', 'recordar'],

    init(sock) {
        currentSock = sock;
        loadStore();
        const now = Date.now();
        const vivos = [];
        for (const item of store.items) {
            if (item.at > now) {
                vivos.push(item);
                scheduleItem(item);
            } else if (now - item.at < LATE_GRACE) {
                fireItem(item, true); // venció con el bot apagado
            }
            // demasiado viejo → descartado silenciosamente
        }
        store.items = vivos;
        saveStore();
        console.log(`⚙️ [rem] ${store.items.length} recordatorio(s) activo(s)`);
    },

    handler: async (sock, msg, args) => {
        const jid = msg.key.remoteJid;
        try {
            if (args.length === 0 || args[0].toLowerCase() === 'help' || args[0] === '?') {
                await sock.sendMessage(jid, { text: HELP }, { quoted: msg });
                return;
            }

            const sub = args[0].toLowerCase();
            loadStore();

            // ── list ──
            if (sub === 'list' || sub === 'ls') {
                const mine = store.items.filter(i => i.chat === jid).sort((a, b) => a.at - b.at);
                if (!mine.length) {
                    await sock.sendMessage(jid, { text: '📭 No tienes recordatorios pendientes.' }, { quoted: msg });
                    return;
                }
                const now = Date.now();
                const lines = mine.map((it, i) =>
                    `*${i + 1}.* ${fmtDate(it.at)} · en ${fmtDur(it.at - now)}\n   ${it.text.slice(0, 120)}`
                );
                await sock.sendMessage(jid, { text: `⏰ *Pendientes (${mine.length}):*\n\n${lines.join('\n\n')}` }, { quoted: msg });
                return;
            }

            // ── del N ──
            if (sub === 'del' || sub === 'rm' || sub === 'delete') {
                const num = parseInt(args[1], 10);
                if (!Number.isFinite(num) || num < 1) {
                    await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
                    await sock.sendMessage(jid, { text: 'Uso: `.rem del <número>` (ver la lista con `.rem list`)' }, { quoted: msg });
                    return;
                }
                const mine = store.items.filter(i => i.chat === jid).sort((a, b) => a.at - b.at);
                const target = mine[num - 1];
                if (!target) {
                    await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
                    await sock.sendMessage(jid, { text: `No existe el recordatorio #${num}. Mira la lista con \`.rem list\`.` }, { quoted: msg });
                    return;
                }
                store.items = store.items.filter(i => i.id !== target.id);
                const t = timers.get(target.id);
                if (t) { clearTimeout(t); timers.delete(target.id); }
                saveStore();
                await sock.sendMessage(jid, { react: { text: '🗑️', key: msg.key } });
                await sock.sendMessage(jid, { text: `🗑️ Eliminado: "${target.text.slice(0, 80)}"` }, { quoted: msg });
                return;
            }

            // ── clear ──
            if (sub === 'clear') {
                const before = store.items.length;
                for (const it of store.items.filter(i => i.chat === jid)) {
                    const t = timers.get(it.id);
                    if (t) clearTimeout(t);
                    timers.delete(it.id);
                }
                store.items = store.items.filter(i => i.chat !== jid);
                saveStore();
                const removed = before - store.items.length;
                await sock.sendMessage(jid, { text: removed ? `🗑️ ${removed} recordatorio(s) eliminados.` : 'No tenías recordatorios.' }, { quoted: msg });
                return;
            }

            // ── crear ──
            const parsed = parseWhen(args.join(' '));
            if (!parsed || parsed.needTime) {
                await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
                const motivo = parsed?.needTime
                    ? 'Falta la hora (ej: `.rem mañana 9am texto`)'
                    : 'No entendí el tiempo';
                await sock.sendMessage(jid, { text: `❌ ${motivo}.\n\n${HELP}` }, { quoted: msg });
                return;
            }

            const text = parsed.text.slice(0, MAX_TEXT);
            if (!text) {
                await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
                await sock.sendMessage(jid, { text: '❌ Falta el texto del recordatorio.\nEjemplo: `.rem 30m llamar a mamá`\n\n' + HELP }, { quoted: msg });
                return;
            }

            const delta = parsed.at - Date.now();
            if (delta < MIN_MS || delta > MAX_MS) {
                await sock.sendMessage(jid, { react: { text: '⚠️', key: msg.key } });
                await sock.sendMessage(jid, { text: '❌ El tiempo debe estar entre 5 segundos y 5 años.' }, { quoted: msg });
                return;
            }

            const enEsteChat = store.items.filter(i => i.chat === jid).length;
            if (enEsteChat >= MAX_PER_CHAT) {
                await sock.sendMessage(jid, { react: { text: '🚫', key: msg.key } });
                await sock.sendMessage(jid, { text: `Límite alcanzado (${MAX_PER_CHAT} por chat). Borra algunos con \`.rem del N\`.` }, { quoted: msg });
                return;
            }

            const item = { id: store.nextId++, chat: jid, text, at: parsed.at, created: Date.now() };
            store.items.push(item);
            saveStore();
            scheduleItem(item);

            await sock.sendMessage(jid, { text: `⏰ Anotado para ${fmtDate(item.at)} *(en ${fmtDur(delta)})*:\n"${text}"` }, { quoted: msg });

        } catch (error) {
            console.error('❌ [rem]:', error);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } }).catch(() => {});
        }
    },

    // expuesto solo para pruebas del parser
    _internals: { parseWhen, fmtDur, fmtDate }
};
