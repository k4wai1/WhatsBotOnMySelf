// addons/ttt.js
// Tres en raya (Tic Tac Toe): vs. amigo, vs. bot con 3 dificultades, o matchmaking.
// Portado del bot de rupa y adaptado al router de addons.
const fs = require('fs');
const path = require('path');
const { jidNormalizedUser } = require('@whiskeysockets/baileys');

const activeGames = new Map();
let waitingUser = null;

class TicTacToe {
    constructor(chatId, playerX, playerO, playerXName, playerOName, isBotGame = false, difficulty = 'medio') {
        this.chatId = chatId;
        this.playerX = playerX;
        this.playerO = playerO;
        this.playerXName = playerXName;
        this.playerOName = playerOName;
        this.isBotGame = isBotGame;
        this.difficulty = difficulty;
        this.board = [[null, null, null], [null, null, null], [null, null, null]];
        this.turn = 'X';
        this.messageKey = null;
        this.timer = null;
        this.finished = false;
    }

    toString() {
        const symbols = { null: '⬛', 'X': '❌', 'O': '⭕' };
        let boardStr = `Hora de jugar ✨\n\n    🅰️   🅱️   ©️\n`;

        this.board.forEach((row, index) => {
            const r = row.map(cell => symbols[cell]).join('  ');
            boardStr += `${index + 1}   ${r}\n`;
        });

        const currentName = this.turn === 'X' ? this.playerXName : this.playerOName;
        boardStr += `\n❌ ${this.playerXName}\n⭕ ${this.isBotGame ? 'Yo (Bot)' : this.playerOName}`;

        if (!this.finished) {
            boardStr += `\n\n🎯 Le toca a: ${currentName}\n📝 Dime la coordenada (ej: a1, b3)`;
        } else {
            boardStr += `\n\n🏁 Ya se terminó 🏁`;
        }
        return boardStr;
    }

    play(row, col) {
        if (this.board[row][col] !== null) return false;
        this.board[row][col] = this.turn;
        return true;
    }

    switchTurn() { this.turn = this.turn === 'X' ? 'O' : 'X'; }

    getWinner() {
        const b = this.board;
        const lines = [
            [b[0][0], b[0][1], b[0][2]], [b[1][0], b[1][1], b[1][2]], [b[2][0], b[2][1], b[2][2]],
            [b[0][0], b[1][0], b[2][0]], [b[0][1], b[1][1], b[2][1]], [b[0][2], b[1][2], b[2][2]],
            [b[0][0], b[1][1], b[2][2]], [b[0][2], b[1][1], b[2][0]]
        ];
        for (let line of lines) {
            if (line[0] && line[0] === line[1] && line[1] === line[2]) return line[0];
        }
        if (b.flat().every(cell => cell !== null)) return 'draw';
        return null;
    }

    evaluateBoard() {
        const b = this.board; let score = 0;
        if (b[1][1] === 'O') score += 3; else if (b[1][1] === 'X') score -= 3;
        const corners = [[0, 0], [0, 2], [2, 0], [2, 2]];
        corners.forEach(([r, c]) => { if (b[r][c] === 'O') score += 2; else if (b[r][c] === 'X') score -= 2; });
        return score;
    }
    minimaxLimited(depth, isMaximizing, limitDepth) {
        const winner = this.getWinner();
        if (winner === 'O') return 10 - depth;
        if (winner === 'X') return depth - 10;
        if (winner === 'draw' || depth === limitDepth) return this.evaluateBoard();

        let bestScore = isMaximizing ? -Infinity : Infinity;
        const player = isMaximizing ? 'O' : 'X';
        for (let r = 0; r < 3; r++) {
            for (let c = 0; c < 3; c++) {
                if (this.board[r][c] === null) {
                    this.board[r][c] = player;
                    const score = this.minimaxLimited(depth + 1, !isMaximizing, limitDepth);
                    this.board[r][c] = null;
                    bestScore = isMaximizing ? Math.max(score, bestScore) : Math.min(score, bestScore);
                }
            }
        }
        return bestScore;
    }
    getAvailableMoves() {
        const moves = [];
        for (let r = 0; r < 3; r++) for (let c = 0; c < 3; c++) if (this.board[r][c] === null) moves.push({ r, c });
        return moves;
    }
    easyMove() { const m = this.getAvailableMoves(); return m[Math.floor(Math.random() * m.length)]; }
    mediumMove() {
        const moves = this.getAvailableMoves();
        if (Math.random() < 0.7) {
            for (let player of ['O', 'X']) {
                for (let move of moves) {
                    this.board[move.r][move.c] = player;
                    if (this.getWinner() === player) { this.board[move.r][move.c] = null; return move; }
                    this.board[move.r][move.c] = null;
                }
            }
            let bestScore = -Infinity; let bestMove = moves[0];
            const limit = Math.random() < 0.5 ? 2 : 3;
            for (let move of moves) {
                this.board[move.r][move.c] = 'O';
                const score = this.minimaxLimited(0, false, limit);
                this.board[move.r][move.c] = null;
                if (score > bestScore) { bestScore = score; bestMove = move; }
            }
            return bestMove;
        } else {
            const pMoves = moves.filter(m => (m.r === 1 && m.c === 1) || (m.r === 0 && m.c === 0) || (m.r === 0 && m.c === 2) || (m.r === 2 && m.c === 0) || (m.r === 2 && m.c === 2));
            return (pMoves.length > 0 && Math.random() < 0.7) ? pMoves[Math.floor(Math.random() * pMoves.length)] : moves[Math.floor(Math.random() * moves.length)];
        }
    }
    hardMove() {
        let bestScore = -Infinity; let bestMove = { r: 0, c: 0 };
        for (let r = 0; r < 3; r++) {
            for (let c = 0; c < 3; c++) {
                if (this.board[r][c] === null) {
                    this.board[r][c] = 'O';
                    const score = this.minimaxLimited(0, false, 9);
                    this.board[r][c] = null;
                    if (score > bestScore) { bestScore = score; bestMove = { r, c }; }
                }
            }
        }
        return bestMove;
    }
    makeBotMove() {
        let move = this.difficulty === 'facil' ? this.easyMove() : this.difficulty === 'dificil' ? this.hardMove() : this.mediumMove();
        this.play(move.r, move.c);
    }
}

// Identidades del bot (dinámicas). BOT_NUMBERS del .env es opcional.
function getBotNumbers(sock) {
    const nums = new Set();
    const ids = [sock.user?.id, sock.authState?.creds?.me?.id, sock.authState?.creds?.me?.lid];
    for (const id of ids.filter(Boolean)) {
        const clean = jidNormalizedUser(id).split('@')[0];
        if (clean) nums.add(clean);
    }
    for (const n of (process.env.BOT_NUMBERS || '').split(',')) {
        const clean = n.replace(/\D/g, '');
        if (clean) nums.add(clean);
    }
    return [...nums];
}

const coordRegex = /^(?:([abc])\s*([123])|([123])\s*([abc]))$/i;

async function finishGame(sock, chatId, game, result) {
    game.finished = true;
    clearTimeout(game.timer);
    let endText = game.toString();

    if (result === 'draw') endText += '\n\n🤝 Quedamos iguales... empate ✨';
    else if (result === 'O' && game.isBotGame) endText += `\n\n🤖 Gané...! fue muy divertido`;
    else if (result === 'X' && game.isBotGame) endText += `\n\n🎉 ¡Me ganaste!... Eres super listo 💕`;
    else {
        const winnerName = result === 'X' ? game.playerXName : game.playerOName;
        endText += `\n\n🎉 Ganó ${winnerName}, felicidades`;
    }

    await sock.sendMessage(chatId, { text: endText, edit: game.messageKey });
    activeGames.delete(chatId);
}

// Lógica de un movimiento (jugador X/O). Devuelve true si se procesó.
async function handleMove(sock, msg, game, match) {
    const chatId = msg.key.remoteJid;
    const sender = msg.key.participant || chatId;

    const currentPlayerId = game.turn === 'X' ? game.playerX : game.playerO;
    const isCurrentPlayer = (currentPlayerId === sender);

    if (!isCurrentPlayer) {
        if (game.isBotGame && game.turn === 'O') {
            await sock.sendMessage(chatId, { react: { text: '😖', key: msg.key } });
            return;
        }
        await sock.sendMessage(chatId, { react: { text: '❌', key: msg.key } });
        await sock.sendMessage(chatId, { text: 'Nu, todavía no te toca... estoy pensando' }, { quoted: msg });
        return;
    }

    const letter = (match[1] || match[4]).toLowerCase();
    const number = (match[2] || match[3]);
    const colMap = { 'a': 0, 'b': 1, 'c': 2 };
    const rowMap = { '1': 0, '2': 1, '3': 2 };

    if (!game.play(rowMap[number], colMap[letter])) {
        await sock.sendMessage(chatId, { react: { text: '🚫', key: msg.key } });
        await sock.sendMessage(chatId, { text: 'Ahí no se puede, ya está ocupado...' }, { quoted: msg });
        return;
    }

    let winner = game.getWinner();
    if (winner) {
        await sock.sendMessage(chatId, { react: { text: '✅', key: msg.key } });
        return finishGame(sock, chatId, game, winner);
    }

    game.switchTurn();
    await sock.sendMessage(chatId, { text: game.toString(), edit: game.messageKey });
    await sock.sendMessage(chatId, { react: { text: '✅', key: msg.key } });

    // Movimiento automático del bot
    if (game.isBotGame && !game.finished && game.turn === 'O') {
        setTimeout(async () => {
            try {
                const currentGame = activeGames.get(chatId);
                if (!currentGame || currentGame.turn !== 'O' || currentGame.finished) return;

                currentGame.makeBotMove();
                winner = currentGame.getWinner();
                if (winner) return finishGame(sock, chatId, currentGame, winner);

                currentGame.switchTurn();
                await sock.sendMessage(chatId, { text: currentGame.toString(), edit: currentGame.messageKey });
            } catch (error) { console.error('❌ [ttt] Error bot:', error); }
        }, 1000);
    }
}

module.exports = {
    commands: ['ttt'],

    // Escucha coordenadas sueltas (sin comando) mientras haya una partida activa
    init: (sock) => {
        let prefixes = [',', '!', '/'];
        try {
            const cfg = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'assets', 'config.json'), 'utf-8'));
            if (Array.isArray(cfg.prefixes) && cfg.prefixes.length) prefixes = cfg.prefixes;
        } catch (_) {}

        sock.ev.on('messages.upsert', async (m) => {
            if (m.type !== 'notify') return;
            const msg = m.messages[0];
            if (!msg.message || msg.key.fromMe) return;

            const chatId = msg.key.remoteJid;
            const game = activeGames.get(chatId);
            if (!game) return;

            const text = msg.message?.conversation || msg.message?.extendedTextMessage?.text || '';
            const body = text.trim().toLowerCase();
            if (!body) return;
            // Si empieza con un prefix, lo maneja el router (comandos)
            if (prefixes.some(p => body.startsWith(p.toLowerCase()))) return;

            const match = body.match(coordRegex);
            if (!match) return;
            await handleMove(sock, msg, game, match);
        });
    },

    handler: async (sock, msg, args) => {
        const chatId = msg.key.remoteJid;
        const sender = msg.key.participant || chatId;
        const senderName = msg.pushName || sender.split('@')[0];

        try {
            // ---------------------------------------------------------
            // 1. JUGABILIDAD (si ya hay partida, args puede ser una coordenada)
            // ---------------------------------------------------------
            if (activeGames.has(chatId)) {
                const game = activeGames.get(chatId);
                const asCoord = args.join(' ').trim().toLowerCase();
                const match = asCoord.match(coordRegex);
                if (match) return handleMove(sock, msg, game, match);

                await sock.sendMessage(chatId, { text: '⚠️ Ya estamos jugando... dime una coordenada (ej: a1) 💕' }, { quoted: msg });
                return;
            }

            // ---------------------------------------------------------
            // 2. CREAR JUEGO
            // ---------------------------------------------------------
            let p1 = sender;
            let p2 = null;
            let isBot = false;
            let difficulty = 'medio';
            const playerXName = senderName;
            let playerOName = '';

            const parts = args.map(a => a.toLowerCase());
            const diffKw = {
                'facil': 'facil', '1': 'facil', 'easy': 'facil',
                'medio': 'medio', '2': 'medio',
                'dificil': 'dificil', '3': 'dificil', 'hard': 'dificil'
            };

            const mentionedJids = msg.message?.extendedTextMessage?.contextInfo?.mentionedJid || [];
            const knownBotNumbers = getBotNumbers(sock);

            // ¿Me están mencionando? (JID real o LID)
            let botMentioned = false;
            for (const jid of mentionedJids) {
                const numericId = jid.split('@')[0];
                if (knownBotNumbers.includes(numericId)) {
                    botMentioned = true;
                    break;
                }
            }

            // Fallback por si escriben el número directo sin mención real de WA
            const body = (msg.message?.conversation || msg.message?.extendedTextMessage?.text || '').toLowerCase();
            if (!botMentioned && knownBotNumbers.some(num => body.includes(`@${num}`))) {
                botMentioned = true;
            }

            if (botMentioned) {
                p2 = 'BOT';
                isBot = true;
                playerOName = 'Bot';
                for (let part of parts) { if (diffKw[part]) { difficulty = diffKw[part]; break; } }
            } else if (mentionedJids.length > 0) {
                p2 = mentionedJids[0];
                if (p2 === p1) {
                    await sock.sendMessage(chatId, { text: 'Eh? no puedes jugar solito 😿' }, { quoted: msg });
                    return;
                }
                playerOName = p2.split('@')[0];
            } else {
                if (waitingUser && waitingUser.chat === chatId && waitingUser.id !== p1) {
                    p2 = p1; p1 = waitingUser.id;
                    playerOName = p2.split('@')[0];
                    waitingUser = null;
                } else {
                    waitingUser = { id: p1, chat: chatId, name: playerXName };
                    await sock.sendMessage(chatId, { text: 'Buscando a alguien que quiera jugar... 🐢\n\nO si quieres juego yo: .ttt @bot [facil(1)|medio(2)|dificil(3)]' }, { quoted: msg });
                    return;
                }
            }

            const game = new TicTacToe(chatId, p1, p2, playerXName, playerOName, isBot, difficulty);
            const sentMsg = await sock.sendMessage(chatId, { text: game.toString() });
            game.messageKey = sentMsg.key;

            game.timer = setTimeout(() => {
                if (activeGames.has(chatId)) {
                    activeGames.delete(chatId);
                    sock.sendMessage(chatId, { text: "Se acabó el tiempo, perdón 😿 se cancela.", edit: game.messageKey });
                }
            }, 4 * 60 * 1000);

            activeGames.set(chatId, game);

            if (isBot) {
                await sock.sendMessage(chatId, { text: `Sii, jugaré contigo 🫡\nDificultad: ${difficulty}\n\n¡Haré mi mejor esfuerzo!` }, { quoted: msg });
            }

        } catch (err) {
            console.error('❌ [ttt] Error:', err.message);
            await sock.sendMessage(chatId, { react: { text: '❌', key: msg.key } });
        }
    }
};
