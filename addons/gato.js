// addons/gato.js — Tamagotchi por ediciones de mensaje y reacciones
const fs = require('fs');
const path = require('path');

const DATA_FILE = path.join(__dirname, '..', 'assets', 'gato_data.json');
const INACTIVIDAD_MS = 40000;
const PARPADEO_MS = 7000;

const activeGames = new Map();

// ─── Sprites ────────────────────────────────────────────────────────────────
const SPRITES = {
    // COCINA
    idle_1: " ╱|_\n(•  •7\n 、 ˜〵\n じしˍ,)ノ",
    idle_blink: " ╱|_\n(-  -7\n 、 ˜〵\n じしˍ,)ノ",
    eat_1: (f) => ` ╱|_\n(•  •7\n 、 ˜〵\n ${f}しˍ,)ノ`,
    eat_2: (f) => ` ╱|_\n(•  •7\n ${f}૮〵\n  | ˍ,)ノ`,
    eat_loop: (m) => ` ╱|__\n(• ${m} •7\n ꪒ  ꪒ〵\n じしˍ,)ノ`,
    lemon: (m, t) => ` ╱| _\n( >${m}< 7\n 、 ˜〵\n じしˍ,)${t}`,

    // SALA PRINCIPAL
    sala_idle: "     へ   ♡\n ૮ - ‸՛)   \n  / ⁻ ៸|   \n乀 (ˍ,لل   ",
    sala_blink: "     へ   ♡\n ૮  > <)   \n  / ⁻ ៸|   \n乀 (ˍ,لل   "
};

const COMIDAS = ['🐟', '🍗', '🍣', '🥩', '🍉', '🍋'];

const delay = (ms) => new Promise(res => setTimeout(res, ms));

// ─── Persistencia ───────────────────────────────────────────────────────────
function loadData() {
    if (!fs.existsSync(DATA_FILE)) return {};
    try {
        return JSON.parse(fs.readFileSync(DATA_FILE, 'utf8'));
    } catch (e) {
        console.error('Error leyendo datos del gato:', e.message);
        return {};
    }
}

function saveData(data) {
    try {
        fs.mkdirSync(path.dirname(DATA_FILE), { recursive: true });
        fs.writeFileSync(DATA_FILE, JSON.stringify(data, null, 2));
    } catch (e) {
        console.error('Error guardando datos del gato:', e.message);
    }
}

// ─── Escenas (para agregar una nueva: sprite aquí + acción abajo) ───────────
const ESCENAS = {
    'Cocina': { reposo: SPRITES.idle_1, parpadeo: SPRITES.idle_blink },
    'Sala Principal': { reposo: SPRITES.sala_idle, parpadeo: SPRITES.sala_blink }
};

function renderScreen(catData, asciiArt = "") {
    const accionesUi = Object.entries(ACCIONES)
        .map(([emoji, acc]) => `${emoji}(${acc.etiqueta})`)
        .join(' | ');

    let text = `🐾 *${catData.name}* | 📍 *Escena: ${catData.scene}*\n`;
    text += `🍖 Hambre: ${Math.round(catData.stats.hambre)}% | ⚡ Energía: ${Math.round(catData.stats.energia)}%\n`;
    text += `💖 Cariño: ${Math.round(catData.stats.felicidad)}%\n`;
    text += `Acciones: ${accionesUi}\n\n\`\`\`\n`;

    text += "◈" + "―".repeat(20) + "◈\n";
    const artLines = asciiArt.split('\n');
    const startRow = 10 - artLines.length;

    for (let i = 0; i < 10; i++) {
        if (i >= startRow && asciiArt !== "") {
            const line = artLines[i - startRow];
            text += "│" + line.padEnd(20, ' ') + "│\n";
        } else {
            text += "│" + " ".repeat(20) + "│\n";
        }
    }
    text += "◇" + "―".repeat(20) + "◇\n\`\`\`";
    return text;
}

// ─── Acciones (cada una recibe (game, db, cat) y usa game.sock) ─────────────
async function comer(game, db, cat) {
    game.isAnimating = true;
    try {
        cat.scene = 'Comiendo...';
        const food = COMIDAS[Math.floor(Math.random() * COMIDAS.length)];

        await pantalla(game, cat, SPRITES.eat_1(food));
        await delay(1000);
        await pantalla(game, cat, SPRITES.eat_2(food));
        await delay(1000);

        if (food === '🍋') {
            cat.stats.hambre = Math.min(100, cat.stats.hambre + 1);
            const bocas = ['༝', '△', 'ᯅ', 'Д', '‸'];
            for (let i = 0; i < 6; i++) {
                const bocaRandom = bocas[Math.floor(Math.random() * bocas.length)];
                const colaTiembla = i % 2 === 0 ? '/' : 'ノ';
                await pantalla(game, cat, SPRITES.lemon(bocaRandom, colaTiembla));
                await delay(500);
            }
        } else {
            cat.stats.hambre = Math.min(100, cat.stats.hambre + 20);
            const bocas = ['⤙', '~', '-'];
            for (let i = 0; i < 10; i++) {
                const m = bocas[i % 3];
                await pantalla(game, cat, SPRITES.eat_loop(m));
                await delay(500);
            }
        }

        cat.scene = 'Cocina';
        cat.lastUpdate = Date.now();
        saveData(db);
        await pantalla(game, cat, SPRITES.idle_1);
    } finally {
        game.isAnimating = false;
    }
}

async function irALaSala(game, db, cat) {
    if (cat.scene === 'Sala Principal') return;
    cat.scene = 'Sala Principal';
    saveData(db);
    await pantalla(game, cat, SPRITES.sala_idle);
}

// Registro global de acciones: agregar entradas aquí las aparece en la UI y se activan solas
const ACCIONES = {
    '👍': { etiqueta: 'Sala', ejecutar: irALaSala },
    '❤️': { etiqueta: 'Comer', ejecutar: comer }
};

// ─── Motor de render (edita solo si el texto cambió) ────────────────────────
async function pantalla(game, cat, arte) {
    const texto = renderScreen(cat, arte);
    if (texto === game.lastText) return;
    try {
        await game.sock.sendMessage(game.chatJid, { text: texto }, { edit: game.displayKey });
        game.lastText = texto;
    } catch (e) {}
}

function resetTimer(gameId) {
    const game = activeGames.get(gameId);
    if (game) {
        clearTimeout(game.timeout);
        game.timeout = setTimeout(() => closeGame(gameId), INACTIVIDAD_MS);
    }
}

async function closeGame(gameId, razon = '/ᐠ - ˕ -マ Ⳋ n/ > Mucha inactividad, me aburrí.') {
    const game = activeGames.get(gameId);
    if (!game) return;

    clearInterval(game.loop);
    clearTimeout(game.timeout);
    activeGames.delete(gameId);

    try {
        await game.sock.sendMessage(game.chatJid, { text: razon }, { edit: game.displayKey });
    } catch (e) {}
}

// ─── Interacción ────────────────────────────────────────────────────────────
async function manejarReaccion(msg) {
    const reaction = msg.message.reactionMessage;
    const targetId = reaction.key?.id;
    if (!targetId || !activeGames.has(targetId)) return;

    const game = activeGames.get(targetId);
    const reactorId = (msg.key.participant || msg.key.remoteJid).split('@')[0];
    if (reactorId !== game.userId || game.isAnimating) return;

    const emoji = reaction.text;
    if (!emoji) return;

    const accion = ACCIONES[emoji];
    if (!accion) return;

    resetTimer(targetId);
    const db = loadData();
    const cat = db[game.userId];
    if (!cat) return closeGame(targetId);

    try {
        await accion.ejecutar(game, db, cat);
    } catch (e) {
        console.error('Error en acción del gato:', e);
        game.isAnimating = false;
    }
}

async function iniciarJuego(sock, msg) {
    const chatJid = msg.key.remoteJid;
    const userId = (msg.key.participant || msg.key.remoteJid).split('@')[0];

    for (const [id, game] of activeGames.entries()) {
        if (game.userId === userId) {
            await closeGame(id, '/ᐠ - ˕ -マ Ⳋ n/ > Me llamaste desde otro chat. Cerrando esta pantalla.');
        }
    }

    const db = loadData();
    if (!db[userId]) {
        db[userId] = {
            name: 'Michi', scene: 'Sala Principal', lastUpdate: Date.now(),
            stats: { hambre: 50, energia: 100, felicidad: 50 }
        };
    }
    db[userId].lastUpdate = Date.now();
    saveData(db);

    const catData = db[userId];
    const escena = ESCENAS[catData.scene] || ESCENAS['Sala Principal'];

    const displayMsg = await sock.sendMessage(chatJid, { text: renderScreen(catData, escena.reposo) });
    const controlMsg = await sock.sendMessage(chatJid, { text: '> ✨ Reacciona a este mensaje para interactuar 👇' });
    const gameId = controlMsg.key.id;

    const game = {
        sock, chatJid, userId,
        displayKey: displayMsg.key,
        isAnimating: false,
        lastText: null,
        loop: null,
        timeout: null
    };

    game.loop = setInterval(async () => {
        try {
            const g = activeGames.get(gameId);
            if (!g || g.isAnimating) return;

            const dbTick = loadData();
            const cat = dbTick[g.userId];
            if (!cat) return;

            const esc = ESCENAS[cat.scene];
            if (!esc?.parpadeo || !esc?.reposo) return;

            await pantalla(g, cat, esc.parpadeo);
            await delay(500);

            const g2 = activeGames.get(gameId);
            if (!g2 || g2.isAnimating) return;
            const cat2 = loadData()[g2.userId];
            if (cat2) await pantalla(g2, cat2, (ESCENAS[cat2.scene] || esc).reposo);
        } catch (e) {
            console.error('Error en idle del gato:', e);
        }
    }, PARPADEO_MS);

    game.timeout = setTimeout(() => closeGame(gameId), INACTIVIDAD_MS);
    activeGames.set(gameId, game);
}

// ─── Contrato del addon ─────────────────────────────────────────────────────
module.exports = {
    commands: ['gato'],

    handler: async (sock, msg) => {
        try {
            await iniciarJuego(sock, msg);
        } catch (e) {
            console.error('Error iniciando el gato:', e);
            await sock.sendMessage(msg.key.remoteJid, { react: { text: '❌', key: msg.key } });
        }
    },

    init: (sock) => {
        for (const [, game] of activeGames) {
            clearInterval(game.loop);
            clearTimeout(game.timeout);
        }
        activeGames.clear();

        sock.ev.on('messages.upsert', async (m) => {
            if (m.type !== 'notify') return;
            const msg = m.messages[0];
            if (!msg.message?.reactionMessage) return;
            try {
                await manejarReaccion(msg);
            } catch (e) {
                console.error('Error procesando reacción del gato:', e);
            }
        });
    }
};
