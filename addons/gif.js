// addons/gif.js
// Interacciones con GIFs animados (Tenor): .g <acción> @user, .g help, .g top,
// .ongif / .offgif para activar por grupo y .topg como alias del ranking.
// Portado del bot de rupa y adaptado al router de addons.
const fs = require('fs');
const path = require('path');
const axios = require('axios');

// --- CONFIGURACIÓN ---
const TENOR_KEY = process.env.TENOR_API_KEY || ''; // https://developers.google.com/tenor
const CLIENT_KEY = 'whatsbot_self';

// 👑 Números con privilegios de dueño: OWNER_NUMBERS=584...,182... en .env (opcional)
const OWNER_NUMBERS = (process.env.OWNER_NUMBERS || '')
    .split(',')
    .map(n => n.replace(/\D/g, ''))
    .filter(Boolean);

const STATS_DIR = path.join(__dirname, '..', 'assets', 'statistics');
const CONFIG_FILE = path.join(STATS_DIR, 'gif_config.json');
const INTERACTION_STATS_FILE = path.join(STATS_DIR, 'interactions.json');

// Memoria volátil para el Anti-Spam: { groupId: { senderId: { actionKey: [timestamp1, timestamp2] } } }
const cooldowns = {};

// --- UTILS DE VERIFICACIÓN ---
function extractNumber(id) {
    return id ? id.split('@')[0] : 'Desconocido';
}

// Función Anti-Spam: Límite de 3 usos cada 2 minutos (120000 ms) por tipo de acción
function checkSpam(groupId, senderId, actionKey) {
    if (!cooldowns[groupId]) cooldowns[groupId] = {};
    if (!cooldowns[groupId][senderId]) cooldowns[groupId][senderId] = {};
    if (!cooldowns[groupId][senderId][actionKey]) cooldowns[groupId][senderId][actionKey] = [];

    const now = Date.now();

    // Filtrar y mantener solo los timestamps de los últimos 2 minutos
    cooldowns[groupId][senderId][actionKey] = cooldowns[groupId][senderId][actionKey].filter(ts => now - ts < 120000);

    if (cooldowns[groupId][senderId][actionKey].length >= 3) {
        return false; // Límite alcanzado
    }

    cooldowns[groupId][senderId][actionKey].push(now);
    return true; // Permitido
}

const ACTIONS_DICT = {
    'furry':      { search: 'furry transformation anime', verb: 'convirtió en furro a',                    participle: 'convertidos en furros' },   
    'yuri':       { search: 'anime yuri couple',          verb: 'tuvo un romance yuri con',                participle: 'enamoradas' },   
    
    // ═══════════════════════════════════════════════════════════════
    // 💕 INTERACCIONES ROMÁNTICAS / AFECTIVAS
    // ═══════════════════════════════════════════════════════════════
    'kiss':       { search: 'anime kiss romantic',        verb: 'besó',                    participle: 'besados' },
    'hug':        { search: 'anime hug',                   verb: 'abrazó',                  participle: 'abrazados' },
    'cuddle':     { search: 'anime cuddle',                verb: 'se acurrucó con',         participle: 'acurrucados' },
    'handhold':   { search: 'anime hold hands',            verb: 'tomó de la mano a',       participle: 'tomados de la mano' },
    'snuggle':    { search: 'anime snuggle',               verb: 'se acomodó junto a',      participle: 'acompañados' },
    'nuzzle':     { search: 'anime nuzzle',                verb: 'restregó su cara contra', participle: 'mimados' },
    'caress':     { search: 'anime caress',                verb: 'acarició suavemente a',   participle: 'acariciados' },
    'propose':    { search: 'anime propose',               verb: 'le propuso matrimonio a', participle: 'propuestos' },
    'love':       { search: 'anime love confession',       verb: 'le declaró amor a',       participle: 'amados' },
    'flirt':      { search: 'anime flirting',              verb: 'coqueteó con',            participle: 'coqueteados' },
    'wink':       { search: 'anime wink',                  verb: 'le guiñó el ojo a',       participle: 'guiñados' },
    'blowkiss':   { search: 'anime blowing kiss',          verb: 'lanzó un beso a',         participle: 'besados desde lejos' },
    'doki':       { search: 'anime heart eyes',            verb: 'se enamoró de',           participle: 'enamorados' },
    'seduce':     { search: 'anime seduce',                verb: 'sedujo a',                participle: 'seducidos' },
    'marry':      { search: 'anime wedding',               verb: 'se casó con',             participle: 'casados' },
    'date':       { search: 'anime date',                  verb: 'tuvo una cita con',       participle: 'invitados a salir' },
    'carry':      { search: 'anime carry princess',        verb: 'cargó en brazos a',       participle: 'cargados en brazos' },
    'piggyback':  { search: 'anime piggyback',             verb: 'cargó en la espalda a',   participle: 'cargados en la espalda' },
    'forehead':   { search: 'anime forehead kiss',         verb: 'besó la frente de',       participle: 'besados en la frente' },
    'cheek':      { search: 'anime cheek kiss',            verb: 'besó la mejilla de',      participle: 'besados en la mejilla' },
    'pocky':      { search: 'anime pocky game',            verb: 'jugó pocky con',          participle: 'retados al pocky' },
    
    // ═══════════════════════════════════════════════════════════════
    // 🤗 INTERACCIONES AMISTOSAS / CARIÑOSAS
    // ═══════════════════════════════════════════════════════════════
    'pat':        { search: 'anime headpat',               verb: 'le dio palmaditas en la cabeza a', participle: 'acariciados' },
    'pet':        { search: 'anime pet head',              verb: 'le hizo cariño a',        participle: 'mimados' },
    'ruffle':     { search: 'anime ruffle hair',           verb: 'despeinó a',              participle: 'despeinados' },
    'boop':       { search: 'anime boop nose',             verb: 'tocó la nariz de',        participle: 'boopeados' },
    'poke':       { search: 'anime poke',                  verb: 'picó a',                  participle: 'picados' },
    'cheeks':     { search: 'anime pinch cheeks',          verb: 'pellizcó las mejillas de',participle: 'pellizcados' },
    'squish':     { search: 'anime squish cheeks',         verb: 'apretó los cachetes de',  participle: 'estrujados' },
    'tickle':     { search: 'anime tickle',                verb: 'hizo cosquillas a',       participle: 'cosquilleados' },
    'highfive':   { search: 'anime high five',             verb: 'chocó los cinco con',     participle: 'chocados los cinco' },
    'fistbump':   { search: 'anime fist bump',             verb: 'chocó puños con',         participle: 'chocados puños' },
    'handshake':  { search: 'anime handshake',             verb: 'saludó de mano a',        participle: 'saludados de mano' },
    'wave':       { search: 'anime wave hello',            verb: 'saludó a',                participle: 'saludados' },
    'hi':         { search: 'anime wave',                  verb: 'saludó a',                participle: 'saludados' },
    'bye':        { search: 'anime wave goodbye',          verb: 'se despidió de',          participle: 'despedidos' },
    'greet':      { search: 'anime greet',                 verb: 'dio la bienvenida a',     participle: 'bienvenidos' },
    'glomp':      { search: 'anime glomp',                 verb: 'se lanzó encima de',      participle: 'glompeados' },
    'tackle':     { search: 'anime tackle hug',            verb: 'tacleó con abrazo a',     participle: 'tacleados' },
    'cling':      { search: 'anime cling',                 verb: 'se aferró a',             participle: 'aferrados' },
    
    // ═══════════════════════════════════════════════════════════════
    // 👊 INTERACCIONES AGRESIVAS / DE COMBATE
    // ═══════════════════════════════════════════════════════════════
    'slap':       { search: 'anime slap',                  verb: 'abofeteó a',              participle: 'abofeteados' },
    'punch':      { search: 'anime punch',                 verb: 'golpeó a',                participle: 'golpeados' },
    'kick':       { search: 'anime kick',                  verb: 'pateó a',                 participle: 'pateados' },
    'smack':      { search: 'anime smack head',            verb: 'dio un zape a',           participle: 'zapeados' },
    'bonk':       { search: 'anime bonk',                  verb: 'dio un bonk a',           participle: 'bonkeados' },
    'whack':      { search: 'anime whack',                 verb: 'aporreó a',               participle: 'aporreados' },
    'hit':        { search: 'anime hit',                   verb: 'golpeó a',                participle: 'golpeados' },
    'bite':       { search: 'anime bite',                  verb: 'mordió a',                participle: 'mordidos' },
    'scratch':    { search: 'anime scratch',               verb: 'arañó a',                 participle: 'arañados' },
    'choke':      { search: 'anime choke',                 verb: 'ahorcó a',                participle: 'ahorcados' },
    'strangle':   { search: 'anime strangle',              verb: 'estranguló a',            participle: 'estrangulados' },
    'throw':      { search: 'anime throw',                 verb: 'lanzó a',                 participle: 'lanzados' },
    'yeet':       { search: 'anime throw person',          verb: 'yeeteó a',                participle: 'yeeteados' },
    'suplex':     { search: 'anime suplex',                verb: 'suplexeó a',              participle: 'suplexeados' },
    'dropkick':   { search: 'anime dropkick',              verb: 'dio una patada voladora a', participle: 'dropkickeados' },
    'uppercut':   { search: 'anime uppercut',              verb: 'dio un uppercut a',       participle: 'uppercuteados' },
    'combo':      { search: 'anime combo attack',          verb: 'hizo combo en',           participle: 'combeados' },
    'knockout':   { search: 'anime knockout',              verb: 'noqueó a',                participle: 'noqueados' },
    'kill':       { search: 'anime kill',                  verb: 'eliminó a',               participle: 'eliminados' },
    'destroy':    { search: 'anime destroy',               verb: 'destruyó a',              participle: 'destruidos' },
    'stab':       { search: 'anime stab',                  verb: 'apuñaló a',               participle: 'apuñalados' },
    'shoot':      { search: 'anime shoot',                 verb: 'disparó a',               participle: 'disparados' },
    'slash':      { search: 'anime sword slash',           verb: 'cortó a',                 participle: 'cortados' },
    'nuke':       { search: 'anime explosion',             verb: 'nukeó a',                 participle: 'nukeados' },
    'boom':       { search: 'anime explosion',             verb: 'explotó junto a',         participle: 'explotados' },
    'attack':     { search: 'anime attack',                verb: 'atacó a',                 participle: 'atacados' },
    'fight':      { search: 'anime fight',                 verb: 'peleó con',               participle: 'peleados' },
    'chase':      { search: 'anime chase',                 verb: 'persiguió a',             participle: 'perseguidos' },
    'spank':      { search: 'anime spank',                 verb: 'nalgueó a',               participle: 'nalgueados' },
    'squeeze':    { search: 'anime squeeze',               verb: 'apretujó a',              participle: 'apretujados' },
    'headbutt':   { search: 'anime headbutt',              verb: 'dio un cabezazo a',       participle: 'cabezados' },
    'elbow':      { search: 'anime elbow',                 verb: 'dio un codazo a',         participle: 'codeados' },
    'backhand':   { search: 'anime backhand',              verb: 'dio un revés a',          participle: 'golpeados con revés' },
    
    // ═══════════════════════════════════════════════════════════════
    // 😢 EMOCIONES TRISTES / NEGATIVAS
    // ═══════════════════════════════════════════════════════════════
    'cry':        { search: 'anime cry',                   verb: 'lloró con',               participle: 'acompañados en llanto' },
    'sob':        { search: 'anime sob',                   verb: 'sollozó frente a',        participle: 'acompañados en sollozos' },
    'sad':        { search: 'anime sad',                   verb: 'se entristeció por',      participle: 'entristecidos' },
    'depressed':  { search: 'anime depressed',             verb: 'se deprimió por',         participle: 'deprimidos' },
    'lonely':     { search: 'anime lonely',                verb: 'se sintió solo junto a',  participle: 'acompañados en soledad' },
    'hurt':       { search: 'anime hurt',                  verb: 'se sintió herido por',    participle: 'heridos' },
    'suffer':     { search: 'anime suffer',                verb: 'sufrió por',              participle: 'sufridos' },
    'pain':       { search: 'anime pain',                  verb: 'sintió dolor por',        participle: 'doloridos' },
    'disappoint': { search: 'anime disappointed',          verb: 'se decepcionó de',        participle: 'decepcionados' },
    'regret':     { search: 'anime regret',                verb: 'se arrepintió con',       participle: 'arrepentidos' },
    'miss':       { search: 'anime miss you',              verb: 'extrañó a',               participle: 'extrañados' },
    'worry':      { search: 'anime worry',                 verb: 'se preocupó por',         participle: 'preocupados' },
    'nervous':    { search: 'anime nervous',               verb: 'se puso nervioso con',    participle: 'puestos nerviosos' },
    'panic':      { search: 'anime panic',                 verb: 'entró en pánico por',     participle: 'aterrorizados' },
    'scared':     { search: 'anime scared',                verb: 'se asustó de',            participle: 'asustados' },
    'fear':       { search: 'anime fear',                  verb: 'le tuvo miedo a',         participle: 'temidos' },
    'shiver':     { search: 'anime shiver',                verb: 'tembló por',              participle: 'estremecidos' },
    'hide':       { search: 'anime hide scared',           verb: 'se escondió de',          participle: 'escondidos' },
    
    // ═══════════════════════════════════════════════════════════════
    // 😠 EMOCIONES DE ENOJO / FRUSTRACIÓN
    // ═══════════════════════════════════════════════════════════════
    'angry':      { search: 'anime angry',                 verb: 'se enfadó con',           participle: 'enfadados' },
    'rage':       { search: 'anime rage',                  verb: 'enfureció por',           participle: 'enfurecidos' },
    'mad':        { search: 'anime mad',                   verb: 'se enojó con',            participle: 'enojados' },
    'glare':      { search: 'anime glare',                 verb: 'fulminó con la mirada a', participle: 'fulminados' },
    'death':      { search: 'anime death stare',           verb: 'miró con odio a',         participle: 'odiados' },
    'pout':       { search: 'anime pout',                  verb: 'hizo pucheros a',         participle: 'puchereados' },
    'hmph':       { search: 'anime hmph',                  verb: 'ignoró con desprecio a',  participle: 'despreciados' },
    'tantrum':    { search: 'anime tantrum',               verb: 'hizo berrinche a',        participle: 'berrinchados' },
    'scold':      { search: 'anime scold',                 verb: 'regañó a',                participle: 'regañados' },
    'yell':       { search: 'anime yell',                  verb: 'gritó a',                 participle: 'gritados' },
    'scream':     { search: 'anime scream',                verb: 'le gritó a',              participle: 'gritados' },
    'baka':       { search: 'anime baka',                  verb: 'llamó idiota a',          participle: 'llamados idiotas' },
    'tsun':       { search: 'anime tsundere',              verb: 'actuó tsundere con',      participle: 'tsundeados' },
    'jealous':    { search: 'anime jealous',               verb: 'tuvo celos de',           participle: 'celados' },
    
    // ═══════════════════════════════════════════════════════════════
    // 😊 EMOCIONES FELICES / POSITIVAS
    // ═══════════════════════════════════════════════════════════════
    'happy':      { search: 'anime happy',                 verb: 'se alegró por',           participle: 'alegrados' },
    'smile':      { search: 'anime smile',                 verb: 'sonrió a',                participle: 'sonreídos' },
    'grin':       { search: 'anime grin',                  verb: 'sonrió ampliamente a',    participle: 'sonreídos' },
    'laugh':      { search: 'anime laugh',                 verb: 'se rió con',              participle: 'hechos reír' },
    'lol':        { search: 'anime laughing',              verb: 'se carcajeó con',         participle: 'carcajeados' },
    'giggle':     { search: 'anime giggle',                verb: 'se rió tontamente de',    participle: 'divertidos' },
    'joy':        { search: 'anime joy',                   verb: 'saltó de alegría con',    participle: 'alegrados' },
    'excited':    { search: 'anime excited',               verb: 'se emocionó con',         participle: 'emocionados' },
    'hype':       { search: 'anime hype',                  verb: 'se hypeó con',            participle: 'hypeados' },
    'celebrate':  { search: 'anime celebrate',             verb: 'celebró con',             participle: 'celebrados' },
    'cheer':      { search: 'anime cheer',                 verb: 'animó a',                 participle: 'animados' },
    'proud':      { search: 'anime proud',                 verb: 'se enorgulleció de',      participle: 'enorgullecidos' },
    'victory':    { search: 'anime victory',               verb: 'celebró victoria con',    participle: 'victoriosos' },
    'win':        { search: 'anime winner',                verb: 'ganó junto a',            participle: 'ganadores' },
    'jump':       { search: 'anime jump joy',              verb: 'saltó de alegría con',    participle: 'hechos saltar' },
    'spin':       { search: 'anime spin happy',            verb: 'giró de felicidad con',   participle: 'hechos girar' },
    
    // ═══════════════════════════════════════════════════════════════
    // 😳 VERGÜENZA / SONROJO
    // ═══════════════════════════════════════════════════════════════
    'blush':      { search: 'anime blush',                 verb: 'se sonrojó por',          participle: 'sonrojados' },
    'shy':        { search: 'anime shy',                   verb: 'se avergonzó con',        participle: 'avergonzados' },
    'awkward':    { search: 'anime awkward',               verb: 'tuvo un momento incómodo con', participle: 'incomodados' },
    'embarrass':  { search: 'anime embarrassed',           verb: 'avergonzó a',             participle: 'avergonzados' },
    'flustered':  { search: 'anime flustered',             verb: 'se puso rojo por',        participle: 'enrojecidos' },
    'steam':      { search: 'anime steam head',            verb: 'le salió humo por',       participle: 'humeados' },
    'lewd':       { search: 'anime lewd',                  verb: 'tuvo pensamientos sucios con', participle: 'deseados' },
    'nosebleed':  { search: 'anime nosebleed',             verb: 'sangró por la nariz por', participle: 'excitados' },
    'perv':       { search: 'anime pervert',               verb: 'acosó a',                 participle: 'acosados' },
    
    // ═══════════════════════════════════════════════════════════════
    // 🤔 PENSAMIENTO / CONFUSIÓN
    // ═══════════════════════════════════════════════════════════════
    'think':      { search: 'anime think',                 verb: 'pensó en',                participle: 'pensados' },
    'wonder':     { search: 'anime wonder',                verb: 'se preguntó sobre',       participle: 'cuestionados' },
    'confused':   { search: 'anime confused',              verb: 'se confundió con',        participle: 'confundidos' },
    'question':   { search: 'anime question mark',         verb: 'cuestionó a',             participle: 'cuestionados' },
    'curious':    { search: 'anime curious',               verb: 'tuvo curiosidad por',     participle: 'investigados' },
    'suspect':    { search: 'anime suspicious',            verb: 'sospechó de',             participle: 'sospechados' },
    'doubt':      { search: 'anime doubt',                 verb: 'dudó de',                 participle: 'dudados' },
    'shrug':      { search: 'anime shrug',                 verb: 'se encogió de hombros ante', participle: 'ignorados' },
    'idk':        { search: 'anime shrug',                 verb: 'no supo qué decir a',     participle: 'dejados en duda' },
    'realize':    { search: 'anime realize',               verb: 'se dio cuenta por',       participle: 'iluminados' },
    'eureka':     { search: 'anime idea',                  verb: 'tuvo una idea por',       participle: 'inspirados' },
    
    // ═══════════════════════════════════════════════════════════════
    // 😏 ACTITUDES / EXPRESIONES
    // ═══════════════════════════════════════════════════════════════
    'smug':       { search: 'anime smug',                  verb: 'miró con superioridad a', participle: 'menospreciados' },
    'smugdance':  { search: 'anime smug dance',            verb: 'bailó presumido ante',    participle: 'humillados' },
    'cool':       { search: 'anime cool',                  verb: 'actuó cool frente a',     participle: 'impresionados' },
    'flex':       { search: 'anime flex',                  verb: 'flexeó ante',             participle: 'flexeados' },
    'show':       { search: 'anime show off',              verb: 'presumió ante',           participle: 'impresionados' },
    'mock':       { search: 'anime mock',                  verb: 'se burló de',             participle: 'burlados' },
    'tease':      { search: 'anime tease',                 verb: 'molestó a',               participle: 'molestados' },
    'taunt':      { search: 'anime taunt',                 verb: 'provocó a',               participle: 'provocados' },
    'sass':       { search: 'anime sassy',                 verb: 'le tiró shade a',         participle: 'shadeados' },
    'sip':        { search: 'anime sip tea',               verb: 'bebió té juzgando a',     participle: 'juzgados' },
    'judge':      { search: 'anime judging',               verb: 'juzgó a',                 participle: 'juzgados' },
    'stare':      { search: 'anime stare',                 verb: 'miró fijamente a',        participle: 'observados' },
    'peek':       { search: 'anime peek',                  verb: 'espió a',                 participle: 'espiados' },
    'watch':      { search: 'anime watching',              verb: 'observó a',               participle: 'observados' },
    'notice':     { search: 'anime notice',                verb: 'notó a',                  participle: 'notados' },
    'ignore':     { search: 'anime ignore',                verb: 'ignoró a',                participle: 'ignorados' },
    'reject':     { search: 'anime reject',                verb: 'rechazó a',               participle: 'rechazados' },
    'nope':       { search: 'anime nope',                  verb: 'dijo nope a',             participle: 'negados' },
    'disgust':    { search: 'anime disgust',               verb: 'miró con asco a',         participle: 'asqueados' },
    'cringe':     { search: 'anime cringe',                verb: 'le dio cringe por',       participle: 'cringeados' },
    'facepalm':   { search: 'anime facepalm',              verb: 'se facepalmeó por',       participle: 'facepalmeados' },
    'bruh':       { search: 'anime bruh',                  verb: 'dijo bruh a',             participle: 'bruheados' },
    'done':       { search: 'anime done',                  verb: 'ya no aguanta a',         participle: 'agotados' },
    'dead':       { search: 'anime dead inside',           verb: 'murió por dentro por',    participle: 'muertos por dentro' },
    'sigh':       { search: 'anime sigh',                  verb: 'suspiró por',             participle: 'suspirados' },
    'tired':      { search: 'anime tired',                 verb: 'se cansó de',             participle: 'cansados' },
    
    // ═══════════════════════════════════════════════════════════════
    // 🎭 ACCIONES DRAMÁTICAS
    // ═══════════════════════════════════════════════════════════════
    'dramatic':   { search: 'anime dramatic',              verb: 'actuó dramático con',     participle: 'dramatizados' },
    'gasp':       { search: 'anime gasp',                  verb: 'jadeó sorprendido por',   participle: 'impactados' },
    'shock':      { search: 'anime shocked',               verb: 'quedó en shock por',      participle: 'shockeados' },
    'faint':      { search: 'anime faint',                 verb: 'se desmayó por',          participle: 'desmayados' },
    'collapse':   { search: 'anime collapse',              verb: 'colapsó frente a',        participle: 'colapsados' },
    'revive':     { search: 'anime revive',                verb: 'revivió a',               participle: 'revividos' },
    'die':        { search: 'anime dramatic death',        verb: 'murió frente a',          participle: 'muertos' },
    'wasted':     { search: 'anime wasted',                verb: 'quedó wasted con',        participle: 'wasteados' },
    'rip':        { search: 'anime rip',                   verb: 'descansó en paz con',     participle: 'despedidos' },
    'ghost':      { search: 'anime ghost',                 verb: 'fantasmeó a',             participle: 'ghosteados' },
    
    // ═══════════════════════════════════════════════════════════════
    // 💪 APOYO / CONSUELO
    // ═══════════════════════════════════════════════════════════════
    'comfort':    { search: 'anime comfort',               verb: 'consoló a',               participle: 'consolados' },
    'support':    { search: 'anime support',               verb: 'apoyó a',                 participle: 'apoyados' },
    'encourage':  { search: 'anime encourage',             verb: 'motivó a',                participle: 'motivados' },
    'believe':    { search: 'anime believe',               verb: 'creyó en',                participle: 'apoyados' },
    'protect':    { search: 'anime protect',               verb: 'protegió a',              participle: 'protegidos' },
    'save':       { search: 'anime save',                  verb: 'salvó a',                 participle: 'salvados' },
    'rescue':     { search: 'anime rescue',                verb: 'rescató a',               participle: 'rescatados' },
    'heal':       { search: 'anime heal',                  verb: 'sanó a',                  participle: 'sanados' },
    'bandage':    { search: 'anime bandage',               verb: 'vendó a',                 participle: 'vendados' },
    'medicine':   { search: 'anime medicine',              verb: 'dio medicina a',          participle: 'medicados' },
    'care':       { search: 'anime take care',             verb: 'cuidó de',                participle: 'cuidados' },
    'praise':     { search: 'anime praise',                verb: 'elogió a',                participle: 'elogiados' },
    'thanks':     { search: 'anime thank you',             verb: 'agradeció a',             participle: 'agradecidos' },
    'sorry':      { search: 'anime sorry',                 verb: 'se disculpó con',         participle: 'disculpados' },
    'forgive':    { search: 'anime forgive',               verb: 'perdonó a',               participle: 'perdonados' },
    
    // ═══════════════════════════════════════════════════════════════
    // 🍜 COMIDA / BEBIDA
    // ═══════════════════════════════════════════════════════════════
    'eat':        { search: 'anime eating',                verb: 'comió frente a',          participle: 'vistos comiendo' },
    'feed':       { search: 'anime feed',                  verb: 'alimentó a',              participle: 'alimentados' },
    'cook':       { search: 'anime cooking',               verb: 'cocinó para',             participle: 'alimentados' },
    'bake':       { search: 'anime baking',                verb: 'horneó para',             participle: 'obsequiados con horneados' },
    'drink':      { search: 'anime drink',                 verb: 'bebió con',               participle: 'acompañados en la bebida' },
    'coffee':     { search: 'anime coffee',                verb: 'tomó café con',           participle: 'compañeros de café' },
    'tea':        { search: 'anime tea',                   verb: 'tomó té con',             participle: 'compañeros de té' },
    'beer':       { search: 'anime beer',                  verb: 'bebió cerveza con',       participle: 'compañeros de cerveza' },
    'sake':       { search: 'anime sake',                  verb: 'tomó sake con',           participle: 'compañeros de sake' },
    'drunk':      { search: 'anime drunk',                 verb: 'se emborrachó con',       participle: 'emborrachados' },
    'cheers':     { search: 'anime cheers toast',          verb: 'brindó con',              participle: 'celebrados con brindis' },
    'lick':       { search: 'anime lick',                  verb: 'lamió a',                 participle: 'lamidos' },
    'nom':        { search: 'anime nom',                   verb: 'comió ñam ñam con',       participle: 'ñameados' },
    'hungry':     { search: 'anime hungry',                verb: 'tuvo hambre con',         participle: 'hambreados' },
    'yummy':      { search: 'anime yummy',                 verb: 'disfrutó comida con',     participle: 'disfrutados' },
    'share':      { search: 'anime share food',            verb: 'compartió comida con',    participle: 'compartidas comidas' },
    'ramen':      { search: 'anime ramen',                 verb: 'comió ramen con',         participle: 'rameneados' },
    'icecream':   { search: 'anime ice cream',             verb: 'comió helado con',        participle: 'heladeados' },
    'cake':       { search: 'anime cake',                  verb: 'comió pastel con',        participle: 'pastelados' },
    'candy':      { search: 'anime candy',                 verb: 'comió dulces con',        participle: 'endulzados' },
    
    // ═══════════════════════════════════════════════════════════════
    // 🛏️ DESCANSO / SUEÑO
    // ═══════════════════════════════════════════════════════════════
    'sleep':      { search: 'anime sleep',                 verb: 'durmió junto a',          participle: 'acompañados' },
    'nap':        { search: 'anime nap',                   verb: 'tomó siesta con',         participle: 'siesteados' },
    'sleepy':     { search: 'anime sleepy',                verb: 'tuvo sueño con',          participle: 'somnolientos' },
    'yawn':       { search: 'anime yawn',                  verb: 'bostezó frente a',        participle: 'hechos bostezar' },
    'wake':       { search: 'anime wake up',               verb: 'despertó a',              participle: 'despertados' },
    'dream':      { search: 'anime dream',                 verb: 'soñó con',                participle: 'soñados' },
    'nightmare':  { search: 'anime nightmare',             verb: 'tuvo pesadilla con',      participle: 'atormentados' },
    'lazy':       { search: 'anime lazy',                  verb: 'flojeó con',              participle: 'flojeados' },
    'relax':      { search: 'anime relax',                 verb: 'se relajó con',           participle: 'relajados' },
    'rest':       { search: 'anime rest',                  verb: 'descansó con',            participle: 'descansados' },
    'blanket':    { search: 'anime blanket',               verb: 'arropó a',                participle: 'arropados' },
    'pillow':     { search: 'anime pillow',                verb: 'lanzó almohada a',        participle: 'almohadazos' },
    
    // ═══════════════════════════════════════════════════════════════
    // 🎵 MÚSICA / BAILE
    // ═══════════════════════════════════════════════════════════════
    'dance':      { search: 'anime dance',                 verb: 'bailó con',               participle: 'bailados' },
    'waltz':      { search: 'anime waltz',                 verb: 'valsó con',               participle: 'valseados' },
    'sing':       { search: 'anime sing',                  verb: 'cantó para',              participle: 'entonados' },
    'karaoke':    { search: 'anime karaoke',               verb: 'cantó karaoke con',       participle: 'karaokeados' },
    'music':      { search: 'anime music',                 verb: 'escuchó música con',      participle: 'musicados' },
    'guitar':     { search: 'anime guitar',                verb: 'tocó guitarra para',      participle: 'guitarreados' },
    'piano':      { search: 'anime piano',                 verb: 'tocó piano para',         participle: 'pianeados' },
    'drum':       { search: 'anime drums',                 verb: 'tocó batería para',       participle: 'drummeados' },
    'headbang':   { search: 'anime headbang',              verb: 'headbangeó con',          participle: 'headbangeados' },
    'rave':       { search: 'anime rave',                  verb: 'raveó con',               participle: 'raveados' },
    'vibe':       { search: 'anime vibing',                verb: 'vibeó con',               participle: 'vibeados' },
    'groove':     { search: 'anime groove',                verb: 'grooveó con',             participle: 'grooveados' },
    
    // ═══════════════════════════════════════════════════════════════
    // 🎮 JUEGOS / ENTRETENIMIENTO
    // ═══════════════════════════════════════════════════════════════
    'play':       { search: 'anime playing games',         verb: 'jugó con',                participle: 'jugadores' },
    'game':       { search: 'anime gaming',                verb: 'gameó con',               participle: 'gameados' },
    'win':        { search: 'anime win',                   verb: 'ganó contra',             participle: 'derrotados' },
    'lose':       { search: 'anime lose',                  verb: 'perdió contra',           participle: 'ganadores' },
    'rage':       { search: 'anime rage quit',             verb: 'rage quiteó por',         participle: 'afectados por rage quit' },
    'gg':         { search: 'anime good game',             verb: 'dijo GG a',               participle: 'felicitados' },
    'noob':       { search: 'anime noob',                  verb: 'llamó noob a',            participle: 'llamados noobs' },
    'pro':        { search: 'anime pro gamer',             verb: 'proclamó pro a',          participle: 'proclamados pros' },
    'camp':       { search: 'anime camp',                  verb: 'acampó con',              participle: 'acampeados' },
    'rps':        { search: 'anime rock paper scissors',   verb: 'jugó piedra papel tijera con', participle: 'retados' },
    
    // ═══════════════════════════════════════════════════════════════
    // 🏃 ACCIONES FÍSICAS / MOVIMIENTO
    // ═══════════════════════════════════════════════════════════════
    'run':        { search: 'anime run',                   verb: 'corrió hacia',            participle: 'perseguidos' },
    'walk':       { search: 'anime walk',                  verb: 'caminó con',              participle: 'acompañados' },
    'fly':        { search: 'anime fly',                   verb: 'voló con',                participle: 'compañeros de vuelo' },
    'teleport':   { search: 'anime teleport',              verb: 'se teletransportó a',     participle: 'teletransportados' },
    'dodge':      { search: 'anime dodge',                 verb: 'esquivó a',               participle: 'esquivados' },
    'fall':       { search: 'anime fall',                  verb: 'cayó frente a',           participle: 'caídos' },
    'trip':       { search: 'anime trip fall',             verb: 'tropezó frente a',        participle: 'tropezados' },
    'slide':      { search: 'anime slide',                 verb: 'se deslizó hacia',        participle: 'deslizados' },
    'flip':       { search: 'anime flip',                  verb: 'hizo flip ante',          participle: 'flippeados' },
    'roll':       { search: 'anime roll',                  verb: 'rodó hacia',              participle: 'rodados' },
    'crawl':      { search: 'anime crawl',                 verb: 'gateó hacia',             participle: 'gateados' },
    'sneak':      { search: 'anime sneak',                 verb: 'se escabulló hacia',      participle: 'espiados' },
    'splash':     { search: 'anime splash water',          verb: 'salpicó a',               participle: 'salpicados' },
    'spray':      { search: 'anime spray',                 verb: 'roció a',                 participle: 'rociados' },
    'swim':       { search: 'anime swim',                  verb: 'nadó con',                participle: 'compañeros de natación' },
    
    // ═══════════════════════════════════════════════════════════════
    // ✨ MAGIA / PODERES
    // ═══════════════════════════════════════════════════════════════
    'magic':      { search: 'anime magic',                 verb: 'usó magia en',            participle: 'hechizados' },
    'spell':      { search: 'anime cast spell',            verb: 'lanzó hechizo a',         participle: 'hechizados' },
    'curse':      { search: 'anime curse',                 verb: 'maldijo a',               participle: 'maldecidos' },
    'bless':      { search: 'anime bless',                 verb: 'bendijo a',               participle: 'bendecidos' },
    'power':      { search: 'anime power up',              verb: 'se empoderó con',         participle: 'empoderados' },
    'transform':  { search: 'anime transform',             verb: 'se transformó ante',      participle: 'testigos' },
    'fusion':     { search: 'anime fusion',                verb: 'se fusionó con',          participle: 'fusionados' },
    'summon':     { search: 'anime summon',                verb: 'invocó a',                participle: 'invocados' },
    'banish':     { search: 'anime banish',                verb: 'desterró a',              participle: 'desterrados' },
    'freeze':     { search: 'anime freeze',                verb: 'congeló a',               participle: 'congelados' },
    'burn':       { search: 'anime fire',                  verb: 'quemó a',                 participle: 'quemados' },
    'shock':      { search: 'anime electric shock',        verb: 'electrocutó a',           participle: 'electrocutados' },
    'kamehameha': { search: 'anime kamehameha',            verb: 'lanzó kamehameha a',      participle: 'kamehameheados' },
    'rasengan':   { search: 'anime rasengan',              verb: 'lanzó rasengan a',        participle: 'rasenganeados' },
    
    // ═══════════════════════════════════════════════════════════════
    // 🐱 ACCIONES KAWAII / CUTE
    // ═══════════════════════════════════════════════════════════════
    'uwu':        { search: 'anime uwu',                   verb: 'hizo uwu a',              participle: 'uwueados' },
    'owo':        { search: 'anime owo',                   verb: 'hizo owo a',              participle: 'oweados' },
    'nya':        { search: 'anime nya cat',               verb: 'hizo nya a',              participle: 'nyados' },
    'meow':       { search: 'anime meow',                  verb: 'maulló a',                participle: 'maullados' },
    'wag':        { search: 'anime wag tail',              verb: 'movió la colita a',       participle: 'waggeados' },
    'purr':       { search: 'anime purr',                   verb: 'ronroneó a',              participle: 'ronroneados' },
    'sniff':      { search: 'anime sniff',                  verb: 'olfateó a',               participle: 'olfateados' },
    'nom':        { search: 'anime nom cute',               verb: 'hizo ñam a',              participle: 'ñameados' },
    'rawr':       { search: 'anime rawr',                   verb: 'hizo rawr a',             participle: 'rawreados' },
    'sparkle':    { search: 'anime sparkle eyes',           verb: 'brilló ante',             participle: 'deslumbrados' },
    'kawaii':     { search: 'anime kawaii',                 verb: 'actuó kawaii con',        participle: 'kawaiizados' },
    'cute':       { search: 'anime cute',                   verb: 'fue cute con',            participle: 'cuteados' },
    'adorable':   { search: 'anime adorable',               verb: 'fue adorable con',        participle: 'adorados' },
    
    // ═══════════════════════════════════════════════════════════════
    // 🎪 ACCIONES RANDOM / MEMES
    // ═══════════════════════════════════════════════════════════════
    'dab':        { search: 'anime dab',                    verb: 'hizo dab a',              participle: 'dabbeados' },
    'tpose':      { search: 'anime t pose',                 verb: 'hizo T-pose ante',        participle: 'dominados' },
    'default':    { search: 'anime default dance',          verb: 'default danceó a',        participle: 'defaulteados' },
    'floss':      { search: 'anime floss dance',            verb: 'hizo floss a',            participle: 'flosseados' },
    'rickroll':   { search: 'anime rick roll',              verb: 'rickrolleó a',            participle: 'rickrolleados' },
    'prank':      { search: 'anime prank',                  verb: 'bromeó a',                participle: 'prankeados' },
    'trap':       { search: 'anime trap card',              verb: 'activó trap card a',      participle: 'trapeados' },
    'ded':        { search: 'anime ded',                    verb: 'murió ante',              participle: 'dedeados' },
    'ascend':     { search: 'anime ascend',                 verb: 'ascendió ante',           participle: 'ascendidos' },
    'descend':    { search: 'anime descend',                verb: 'descendió ante',          participle: 'descendidos' },
    'vanish':     { search: 'anime vanish',                 verb: 'desapareció de',          participle: 'desaparecidos' },
    'appear':     { search: 'anime appear',                 verb: 'apareció ante',           participle: 'aparecidos' },
    'poof':       { search: 'anime poof',                   verb: 'hizo poof a',             participle: 'poofeados' },
    'yeehaw':     { search: 'anime cowboy',                 verb: 'hizo yeehaw a',           participle: 'yeehaweados' },
    'ara':        { search: 'anime ara ara',                verb: 'hizo ara ara a',          participle: 'ara araeados' },
    'rero':       { search: 'anime rero',                   verb: 'hizo rero rero a',        participle: 'reroeados' },
    'ora':        { search: 'anime ora ora',                verb: 'ORA ORA a',               participle: 'orados' },
    'muda':       { search: 'anime muda muda',              verb: 'MUDA MUDA a',             participle: 'mudados' },
    'za':         { search: 'anime za warudo',              verb: 'detuvo el tiempo ante',   participle: 'timeados' },
    'teehee':     { search: 'anime teehee',                 verb: 'hizo teehee a',           participle: 'teeheeados' },
    'psycho':     { search: 'anime yandere',                verb: 'psicopateó a',            participle: 'acosados' },
    'creep':      { search: 'anime creepy',                 verb: 'creepeó a',               participle: 'creepeados' },
    'simp':       { search: 'anime simp',                   verb: 'simpeó a',                participle: 'simpeados' },
    'stan':       { search: 'anime stan',                   verb: 'staneó a',                participle: 'staneados' },
    'sus':        { search: 'anime sus suspicious',         verb: 'fue sus con',             participle: 'suseados' },
    'bonkai':     { search: 'anime bonkai',                 verb: 'liberó bonkai ante',      participle: 'bonkaieados' },
    'bankai':     { search: 'anime bankai',                 verb: 'liberó bankai ante',      participle: 'bankaieados' },
    'gear':       { search: 'anime gear second',            verb: 'activó gear ante',        participle: 'geareados' },
    'plus':       { search: 'anime plus ultra',             verb: 'fue PLUS ULTRA con',      participle: 'plus ultrados' },
    'detroit':    { search: 'anime detroit smash',          verb: 'DETROIT SMASH a',         participle: 'detroit smasheados' },
    'texas':      { search: 'anime texas smash',            verb: 'TEXAS SMASH a',           participle: 'texas smasheados' },
    
    // ═══════════════════════════════════════════════════════════════
    // 🎂 OCASIONES ESPECIALES
    // ═══════════════════════════════════════════════════════════════
    'birthday':   { search: 'anime birthday',               verb: 'celebró cumpleaños con',  participle: 'festejados' },
    'party':      { search: 'anime party',                  verb: 'festejó con',             participle: 'partyeados' },
    'gift':       { search: 'anime gift',                   verb: 'dio regalo a',            participle: 'regalados' },
    'present':    { search: 'anime present',                verb: 'sorprendió con regalo a', participle: 'sorprendidos' },
    'flower':     { search: 'anime give flower',            verb: 'dio flores a',            participle: 'floreados' },
    'rose':       { search: 'anime rose',                   verb: 'dio una rosa a',          participle: 'roseados' },
    'chocolate':  { search: 'anime chocolate',              verb: 'dio chocolate a',         participle: 'chocolatados' },
    'valentine':  { search: 'anime valentine',              verb: 'san valentineó a',        participle: 'enamorados' },
    'xmas':       { search: 'anime christmas',              verb: 'deseó feliz navidad a',   participle: 'navideños' },
    'newyear':    { search: 'anime new year',               verb: 'celebró año nuevo con',   participle: 'celebrantes' },
    'halloween':  { search: 'anime halloween',              verb: 'asustó en halloween a',   participle: 'halloweeneados' },
    'cosplay':    { search: 'anime cosplay',                verb: 'cosplayeó para',          participle: 'cosplayeados' },
    
    // ═══════════════════════════════════════════════════════════════
    // 📱 MODERNO / ACTUAL
    // ═══════════════════════════════════════════════════════════════
    'selfie':     { search: 'anime selfie',                 verb: 'tomó selfie con',         participle: 'selfieados' },
    'photo':      { search: 'anime photo',                  verb: 'fotografió a',            participle: 'fotografiados' },
    'call':       { search: 'anime phone call',             verb: 'llamó a',                 participle: 'llamados' },
    'text':       { search: 'anime texting',                verb: 'texteó a',                participle: 'texteados' },
    'block':      { search: 'anime block',                  verb: 'bloqueó a',               participle: 'bloqueados' },
    'unblock':    { search: 'anime unblock',                verb: 'desbloqueó a',            participle: 'desbloqueados' },
    'like':       { search: 'anime thumbs up',              verb: 'dio like a',              participle: 'likeados' },
    'dislike':    { search: 'anime thumbs down',            verb: 'dio dislike a',           participle: 'dislikeados' },
    'subscribe':  { search: 'anime subscribe',              verb: 'se suscribió a',          participle: 'suscritos' },
    'follow':     { search: 'anime follow',                 verb: 'siguió a',                participle: 'seguidos' },
    'unfollow':   { search: 'anime unfollow',               verb: 'dejó de seguir a',        participle: 'unfolloweados' },
    
    // ═══════════════════════════════════════════════════════════════
    // 🔧 ACCIONES VARIAS
    // ═══════════════════════════════════════════════════════════════
    'clap':       { search: 'anime clap',                   verb: 'aplaudió a',              participle: 'aplaudidos' },
    'slowclap':   { search: 'anime slow clap',              verb: 'aplaudió lento a',        participle: 'aplaudidos sarcásticamente' },
    'bow':        { search: 'anime bow',                    verb: 'se inclinó ante',         participle: 'reverenciados' },
    'salute':     { search: 'anime salute',                 verb: 'saludó militarmente a',   participle: 'saludados' },
    'pray':       { search: 'anime pray',                   verb: 'rezó por',                participle: 'rezados' },
    'wish':       { search: 'anime wish',                   verb: 'deseó algo para',         participle: 'deseados' },
    'point':      { search: 'anime point',                  verb: 'señaló a',                participle: 'señalados' },
    'shush':      { search: 'anime shush',                  verb: 'calló a',                 participle: 'callados' },
    'silence':    { search: 'anime silence',                verb: 'silenció a',              participle: 'silenciados' },
    'clean':      { search: 'anime clean',                  verb: 'limpió para',             participle: 'beneficiados con limpieza' },
    'work':       { search: 'anime work',                   verb: 'trabajó con',             participle: 'compañeros de trabajo' },
    'study':      { search: 'anime study',                  verb: 'estudió con',             participle: 'compañeros de estudio' },
    'read':       { search: 'anime read',                   verb: 'leyó con',                participle: 'leídos' },
    'write':      { search: 'anime write',                  verb: 'escribió a',              participle: 'escritos' },
    'draw':       { search: 'anime draw',                   verb: 'dibujó a',                participle: 'dibujados' },
    'paint':      { search: 'anime paint',                  verb: 'pintó a',                 participle: 'pintados' },
    'build':      { search: 'anime build',                  verb: 'construyó con',           participle: 'constructores' },
    'break':      { search: 'anime break',                  verb: 'rompió algo de',          participle: 'rotos' },
    'fix':        { search: 'anime fix',                    verb: 'arregló algo para',       participle: 'arreglados' },
    'steal':      { search: 'anime steal',                  verb: 'robó de',                 participle: 'robados' },
    'give':       { search: 'anime give',                   verb: 'le dio algo a',           participle: 'recibidos' },
    'take':       { search: 'anime take',                   verb: 'tomó de',                 participle: 'tomados' },
    'grab':       { search: 'anime grab',                   verb: 'agarró a',                participle: 'agarrados' },
    'push':       { search: 'anime push',                   verb: 'empujó a',                participle: 'empujados' },
    'pull':       { search: 'anime pull',                   verb: 'jaló a',                  participle: 'jalados' },
    'drag':       { search: 'anime drag',                   verb: 'arrastró a',              participle: 'arrastrados' },
    'lift':       { search: 'anime lift',                   verb: 'levantó a',               participle: 'levantados' },
    'drop':       { search: 'anime drop',                   verb: 'dejó caer a',             participle: 'dropeados' },
    'catch':      { search: 'anime catch',                  verb: 'atrapó a',                participle: 'atrapados' },
    'miss':       { search: 'anime miss catch',             verb: 'falló en atrapar a',      participle: 'no atrapados' }
};

// --- GESTIÓN DE DATOS ---
if (!fs.existsSync(STATS_DIR)) fs.mkdirSync(STATS_DIR, { recursive: true });
if (!fs.existsSync(CONFIG_FILE)) fs.writeFileSync(CONFIG_FILE, JSON.stringify({ active_groups: [] }));
if (!fs.existsSync(INTERACTION_STATS_FILE)) fs.writeFileSync(INTERACTION_STATS_FILE, JSON.stringify({}));

let config = JSON.parse(fs.readFileSync(CONFIG_FILE));
let interactionStats = JSON.parse(fs.readFileSync(INTERACTION_STATS_FILE));

const saveConfig = () => fs.writeFileSync(CONFIG_FILE, JSON.stringify(config, null, 2));
const saveStats = () => fs.writeFileSync(INTERACTION_STATS_FILE, JSON.stringify(interactionStats, null, 2));

const registerInteraction = (chatId, senderId, senderName, receiverId, receiverName, actionKey) => {
    if (!interactionStats[chatId]) interactionStats[chatId] = {};

    // 1. Registrar al emisor
    if (!interactionStats[chatId][senderId]) {
        interactionStats[chatId][senderId] = { name: senderName, targets: {} };
    } else if (senderName && senderName !== 'Alguien') {
        interactionStats[chatId][senderId].name = senderName; // Actualiza nombre
    }

    // 2. Registrar al receptor (dentro del emisor)
    if (!interactionStats[chatId][senderId].targets[receiverId]) {
        interactionStats[chatId][senderId].targets[receiverId] = { name: receiverName, actions: {}, total: 0 };
    }
    if (receiverName && !receiverName.includes('@')) {
        interactionStats[chatId][senderId].targets[receiverId].name = receiverName;
    }

    // 3. Sumar la acción
    if (!interactionStats[chatId][senderId].targets[receiverId].actions[actionKey]) {
        interactionStats[chatId][senderId].targets[receiverId].actions[actionKey] = 0;
    }

    interactionStats[chatId][senderId].targets[receiverId].actions[actionKey]++;
    interactionStats[chatId][senderId].targets[receiverId].total++;

    saveStats();

    return interactionStats[chatId][senderId].targets[receiverId].actions[actionKey];
};

// --- API TENOR ---
const getAnimeGif = async (searchTerm) => {
    try {
        const url = `https://tenor.googleapis.com/v2/search?q=${encodeURIComponent(searchTerm)}&key=${TENOR_KEY}&client_key=${CLIENT_KEY}&limit=40&contentfilter=off&ar_range=wide`;
        const response = await axios.get(url);
        const results = response.data.results;

        if (!results || results.length === 0) return null;

        const randomIndex = Math.floor(Math.random() * results.length);
        const selectedGif = results[randomIndex];
        const formats = selectedGif.media_formats;

        let mediaData = formats.tinymp4 || formats.mp4 || formats.gif;
        return mediaData?.url || null;
    } catch (error) {
        console.error('❌ [gif] Error Tenor API:', error.message);
        return null;
    }
};

// Ranking del grupo (.g top / .topg)
async function sendTop(sock, msg) {
    const jid = msg.key.remoteJid;

    if (!interactionStats[jid] || Object.keys(interactionStats[jid]).length === 0) {
        return sock.sendMessage(jid, { text: '📭 Aún no hay interacciones registradas en este grupo.' }, { quoted: msg });
    }

    let allInteractions = [];

    // Recorrer la estructura emisor -> receptor
    for (const sender in interactionStats[jid]) {
        const senderData = interactionStats[jid][sender];
        for (const target in senderData.targets) {
            const targetData = senderData.targets[target];
            allInteractions.push({
                senderName: senderData.name,
                targetName: targetData.name,
                total: targetData.total
            });
        }
    }

    const sorted = allInteractions.sort((a, b) => b.total - a.total).slice(0, 10);

    if (sorted.length === 0) {
        return sock.sendMessage(jid, { text: '📭 Aún no hay interacciones válidas.' }, { quoted: msg });
    }

    let text = `🏆 *TOP INTERACCIONES DEL GRUPO*\n\n`;
    sorted.forEach((interaction, i) => {
        text += `${i + 1}. *${interaction.senderName}* ➡️ *${interaction.targetName}* (${interaction.total} veces)\n`;
    });

    return sock.sendMessage(jid, { text: text }, { quoted: msg });
}

module.exports = {
    commands: ['g', 'ongif', 'offgif', 'topg'],

    handler: async (sock, msg, args, store) => {
        const jid = msg.key.remoteJid;
        if (!jid.endsWith('@g.us')) return;

        const rawText = msg.message?.conversation || msg.message?.extendedTextMessage?.text || '';
        const commandUsed = rawText.trim().split(/\s+/)[0].toLowerCase().replace(/^[.!/,]/, '');
        const senderId = msg.key.participant || jid;
        const senderName = msg.pushName || 'Alguien';

        try {
            // =======================================================
            // 🔒 CONFIGURACIÓN (ON/OFF)
            // =======================================================
            if (commandUsed === 'ongif' || commandUsed === 'offgif') {
                const groupMeta = await sock.groupMetadata(jid);
                const admins = groupMeta.participants
                    .filter(p => p.admin === 'admin' || p.admin === 'superadmin')
                    .flatMap(p => [p.id, p.id.split('@')[0]]);

                const senderNumber = senderId.split('@')[0];
                const isAuthorized = admins.includes(senderId) || admins.includes(senderNumber)
                    || OWNER_NUMBERS.includes(senderNumber);

                if (!isAuthorized) {
                    await sock.sendMessage(jid, { react: { text: '⛔', key: msg.key } });
                    return;
                }

                if (commandUsed === 'ongif') {
                    if (!config.active_groups.includes(jid)) {
                        config.active_groups.push(jid);
                        saveConfig();
                        return sock.sendMessage(jid, { text: '✅ *Interacciones activadas.*' }, { quoted: msg });
                    }
                    return sock.sendMessage(jid, { text: 'ℹ️ Ya estaban activas.' }, { quoted: msg });
                }

                if (commandUsed === 'offgif') {
                    const idx = config.active_groups.indexOf(jid);
                    if (idx > -1) {
                        config.active_groups.splice(idx, 1);
                        saveConfig();
                        return sock.sendMessage(jid, { text: '❌ *Interacciones desactivadas.*' }, { quoted: msg });
                    }
                    return sock.sendMessage(jid, { text: 'ℹ️ No estaban activas.' }, { quoted: msg });
                }
            }

            // Ranking directo (.topg)
            if (commandUsed === 'topg') {
                if (!config.active_groups.includes(jid)) return;
                return sendTop(sock, msg);
            }

            // =======================================================
            // 🔓 INTERACCIONES (.g)
            // =======================================================
            if (commandUsed !== 'g') return;
            if (!config.active_groups.includes(jid)) return;

            if (args.length === 0) {
                return sock.sendMessage(jid, { text: '⚠️ Escribe una acción o búsqueda. Ej: `.g kiss` o `.g spiderman`' }, { quoted: msg });
            }

            const firstArg = args[0].toLowerCase();

            // 📖 Comando de ayuda
            if (firstArg === 'help') {
                const keys = Object.keys(ACTIONS_DICT).sort();
                const list = keys.join(', ');
                return sock.sendMessage(jid, { text: `📖 *Guía de interacciones*\n\nComandos:\n- *.g <acción> @user*\n- *.g <texto libre> @user*\n- *.g top* (Ver ranking del grupo)\n\n🎭 *Acciones:*\n_${list}_` }, { quoted: msg });
            }

            // 🏆 Ranking del grupo
            if (firstArg === 'top') {
                return sendTop(sock, msg);
            }

            // 🎭 Ejecución de la interacción (GIF)
            const presetAction = ACTIONS_DICT[firstArg];
            let searchForTenor = '';
            let verbPhrase = '';
            let participlePhrase = '';
            let statsKey = '';

            if (presetAction) {
                searchForTenor = presetAction.search;
                verbPhrase = presetAction.verb;
                participlePhrase = presetAction.participle;
                statsKey = firstArg;
            } else {
                const rawSearch = args.filter(w => !w.startsWith('@')).join(' ');
                searchForTenor = `anime ${rawSearch}`;
                verbPhrase = `envió un GIF de "${rawSearch}" a`;
                participlePhrase = `recibido un GIF de "${rawSearch}"`;
                statsKey = 'custom_gif';
            }

            // Extracción de objetivos (menciones o cita)
            let targetId = senderId;
            const contextInfo = msg.message?.extendedTextMessage?.contextInfo;

            if (contextInfo?.mentionedJid && contextInfo.mentionedJid.length > 0) {
                targetId = contextInfo.mentionedJid[0];
            } else if (contextInfo?.participant) {
                targetId = contextInfo.participant;
            }

            // 🛡️ Filtro anti-spam (3 intentos cada 2 minutos)
            if (!checkSpam(jid, senderId, statsKey)) {
                return sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
            }

            const isSelf = senderId === targetId;

            // 🧠 Lógica de nombre silencioso
            let targetName = '';
            let needsMention = false;

            if (isSelf) {
                targetName = senderName;
            } else {
                // 1. Buscar si el bot ya guardó su nombre en la DB de interacciones
                let knownName = null;
                if (interactionStats[jid] && interactionStats[jid][targetId] && interactionStats[jid][targetId].name) {
                    knownName = interactionStats[jid][targetId].name;
                }

                // 2. Buscar en la caché de contactos del store
                let storeName = null;
                if (store && store.contacts && store.contacts[targetId]) {
                    storeName = store.contacts[targetId].name || store.contacts[targetId].notify;
                }

                const finalName = knownName || storeName;

                // Si encontramos un nombre válido, lo usamos y NO mencionamos
                if (finalName && finalName !== 'Alguien' && !finalName.includes('@') && !finalName.match(/^\d+$/)) {
                    targetName = finalName;
                } else {
                    // Si no sabemos su nombre, forzamos la mención para que WA lo resuelva
                    targetName = `@${extractNumber(targetId)}`;
                    needsMention = true;
                }
            }

            const gifUrl = await getAnimeGif(searchForTenor);
            if (!gifUrl) return sock.sendMessage(jid, { text: '❌ No encontré nada con ese término.' }, { quoted: msg });

            try {
                // Guardamos el nombre en la base de datos (incluso si es solo el número de fallback)
                const newCount = registerInteraction(jid, senderId, senderName, targetId, targetName, statsKey);
                let caption = '';

                if (presetAction) {
                    if (isSelf) {
                        caption = `🎭 *${senderName}* ${verbPhrase} sí mismo.\n✨ Lo ha hecho ${newCount} veces.`;
                    } else {
                        caption = `🎭 *${senderName}* ${verbPhrase} *${targetName}*.\n✨ *${targetName}* ha sido ${participlePhrase} por ti ${newCount} veces.`;
                    }
                } else {
                    caption = isSelf
                        ? `🔎 *${senderName}* se ${verbPhrase} sí mismo.`
                        : `🔎 *${senderName}* ${verbPhrase} *${targetName}*.`;
                }

                // Preparamos el mensaje
                const sendOptions = {
                    video: { url: gifUrl },
                    gifPlayback: true,
                    caption: caption
                };

                // Solo añadimos la mención si es estrictamente necesario
                if (needsMention) {
                    sendOptions.mentions = [targetId];
                }

                await sock.sendMessage(jid, sendOptions);

            } catch (e) { console.error('❌ [gif] Error enviando GIF:', e.message); }

        } catch (e) {
            console.error('❌ [gif] Error:', e.message);
            await sock.sendMessage(jid, { react: { text: '❌', key: msg.key } });
        }
    }
};
