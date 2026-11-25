package addons

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"whatsbot-go/internal/core"
)

type gifAccion struct {
	Search     string
	Verb       string
	Participle string
}

var gifAcciones = map[string]gifAccion{
	"furry":      {Search: "furry transformation anime", Verb: "convirtió en furro a", Participle: "convertidos en furros"},
	"yuri":       {Search: "anime yuri couple", Verb: "tuvo un romance yuri con", Participle: "enamoradas"},
	"kiss":       {Search: "anime kiss romantic", Verb: "besó", Participle: "besados"},
	"hug":        {Search: "anime hug", Verb: "abrazó", Participle: "abrazados"},
	"cuddle":     {Search: "anime cuddle", Verb: "se acurrucó con", Participle: "acurrucados"},
	"handhold":   {Search: "anime hold hands", Verb: "tomó de la mano a", Participle: "tomados de la mano"},
	"snuggle":    {Search: "anime snuggle", Verb: "se acomodó junto a", Participle: "acompañados"},
	"nuzzle":     {Search: "anime nuzzle", Verb: "restregó su cara contra", Participle: "mimados"},
	"caress":     {Search: "anime caress", Verb: "acarició suavemente a", Participle: "acariciados"},
	"propose":    {Search: "anime propose", Verb: "le propuso matrimonio a", Participle: "propuestos"},
	"love":       {Search: "anime love confession", Verb: "le declaró amor a", Participle: "amados"},
	"flirt":      {Search: "anime flirting", Verb: "coqueteó con", Participle: "coqueteados"},
	"wink":       {Search: "anime wink", Verb: "le guiñó el ojo a", Participle: "guiñados"},
	"blowkiss":   {Search: "anime blowing kiss", Verb: "lanzó un beso a", Participle: "besados desde lejos"},
	"doki":       {Search: "anime heart eyes", Verb: "se enamoró de", Participle: "enamorados"},
	"seduce":     {Search: "anime seduce", Verb: "sedujo a", Participle: "seducidos"},
	"marry":      {Search: "anime wedding", Verb: "se casó con", Participle: "casados"},
	"date":       {Search: "anime date", Verb: "tuvo una cita con", Participle: "invitados a salir"},
	"carry":      {Search: "anime carry princess", Verb: "cargó en brazos a", Participle: "cargados en brazos"},
	"piggyback":  {Search: "anime piggyback", Verb: "cargó en la espalda a", Participle: "cargados en la espalda"},
	"forehead":   {Search: "anime forehead kiss", Verb: "besó la frente de", Participle: "besados en la frente"},
	"cheek":      {Search: "anime cheek kiss", Verb: "besó la mejilla de", Participle: "besados en la mejilla"},
	"pocky":      {Search: "anime pocky game", Verb: "jugó pocky con", Participle: "retados al pocky"},
	"pat":        {Search: "anime headpat", Verb: "le dio palmaditas en la cabeza a", Participle: "acariciados"},
	"pet":        {Search: "anime pet head", Verb: "le hizo cariño a", Participle: "mimados"},
	"ruffle":     {Search: "anime ruffle hair", Verb: "despeinó a", Participle: "despeinados"},
	"boop":       {Search: "anime boop nose", Verb: "tocó la nariz de", Participle: "boopeados"},
	"poke":       {Search: "anime poke", Verb: "picó a", Participle: "picados"},
	"cheeks":     {Search: "anime pinch cheeks", Verb: "pellizcó las mejillas de", Participle: "pellizcados"},
	"squish":     {Search: "anime squish cheeks", Verb: "apretó los cachetes de", Participle: "estrujados"},
	"tickle":     {Search: "anime tickle", Verb: "hizo cosquillas a", Participle: "cosquilleados"},
	"highfive":   {Search: "anime high five", Verb: "chocó los cinco con", Participle: "chocados los cinco"},
	"fistbump":   {Search: "anime fist bump", Verb: "chocó puños con", Participle: "chocados puños"},
	"handshake":  {Search: "anime handshake", Verb: "saludó de mano a", Participle: "saludados de mano"},
	"wave":       {Search: "anime wave hello", Verb: "saludó a", Participle: "saludados"},
	"hi":         {Search: "anime wave", Verb: "saludó a", Participle: "saludados"},
	"bye":        {Search: "anime wave goodbye", Verb: "se despidió de", Participle: "despedidos"},
	"greet":      {Search: "anime greet", Verb: "dio la bienvenida a", Participle: "bienvenidos"},
	"glomp":      {Search: "anime glomp", Verb: "se lanzó encima de", Participle: "glompeados"},
	"tackle":     {Search: "anime tackle hug", Verb: "tacleó con abrazo a", Participle: "tacleados"},
	"cling":      {Search: "anime cling", Verb: "se aferró a", Participle: "aferrados"},
	"slap":       {Search: "anime slap", Verb: "abofeteó a", Participle: "abofeteados"},
	"punch":      {Search: "anime punch", Verb: "golpeó a", Participle: "golpeados"},
	"kick":       {Search: "anime kick", Verb: "pateó a", Participle: "pateados"},
	"smack":      {Search: "anime smack head", Verb: "dio un zape a", Participle: "zapeados"},
	"bonk":       {Search: "anime bonk", Verb: "dio un bonk a", Participle: "bonkeados"},
	"whack":      {Search: "anime whack", Verb: "aporreó a", Participle: "aporreados"},
	"hit":        {Search: "anime hit", Verb: "golpeó a", Participle: "golpeados"},
	"bite":       {Search: "anime bite", Verb: "mordió a", Participle: "mordidos"},
	"scratch":    {Search: "anime scratch", Verb: "arañó a", Participle: "arañados"},
	"choke":      {Search: "anime choke", Verb: "ahorcó a", Participle: "ahorcados"},
	"strangle":   {Search: "anime strangle", Verb: "estranguló a", Participle: "estrangulados"},
	"throw":      {Search: "anime throw", Verb: "lanzó a", Participle: "lanzados"},
	"yeet":       {Search: "anime throw person", Verb: "yeeteó a", Participle: "yeeteados"},
	"suplex":     {Search: "anime suplex", Verb: "suplexeó a", Participle: "suplexeados"},
	"dropkick":   {Search: "anime dropkick", Verb: "dio una patada voladora a", Participle: "dropkickeados"},
	"uppercut":   {Search: "anime uppercut", Verb: "dio un uppercut a", Participle: "uppercuteados"},
	"combo":      {Search: "anime combo attack", Verb: "hizo combo en", Participle: "combeados"},
	"knockout":   {Search: "anime knockout", Verb: "noqueó a", Participle: "noqueados"},
	"kill":       {Search: "anime kill", Verb: "eliminó a", Participle: "eliminados"},
	"destroy":    {Search: "anime destroy", Verb: "destruyó a", Participle: "destruidos"},
	"stab":       {Search: "anime stab", Verb: "apuñaló a", Participle: "apuñalados"},
	"shoot":      {Search: "anime shoot", Verb: "disparó a", Participle: "disparados"},
	"slash":      {Search: "anime sword slash", Verb: "cortó a", Participle: "cortados"},
	"nuke":       {Search: "anime explosion", Verb: "nukeó a", Participle: "nukeados"},
	"boom":       {Search: "anime explosion", Verb: "explotó junto a", Participle: "explotados"},
	"attack":     {Search: "anime attack", Verb: "atacó a", Participle: "atacados"},
	"fight":      {Search: "anime fight", Verb: "peleó con", Participle: "peleados"},
	"chase":      {Search: "anime chase", Verb: "persiguió a", Participle: "perseguidos"},
	"spank":      {Search: "anime spank", Verb: "nalgueó a", Participle: "nalgueados"},
	"squeeze":    {Search: "anime squeeze", Verb: "apretujó a", Participle: "apretujados"},
	"headbutt":   {Search: "anime headbutt", Verb: "dio un cabezazo a", Participle: "cabezados"},
	"elbow":      {Search: "anime elbow", Verb: "dio un codazo a", Participle: "codeados"},
	"backhand":   {Search: "anime backhand", Verb: "dio un revés a", Participle: "golpeados con revés"},
	"cry":        {Search: "anime cry", Verb: "lloró con", Participle: "acompañados en llanto"},
	"sob":        {Search: "anime sob", Verb: "sollozó frente a", Participle: "acompañados en sollozos"},
	"sad":        {Search: "anime sad", Verb: "se entristeció por", Participle: "entristecidos"},
	"depressed":  {Search: "anime depressed", Verb: "se deprimió por", Participle: "deprimidos"},
	"lonely":     {Search: "anime lonely", Verb: "se sintió solo junto a", Participle: "acompañados en soledad"},
	"hurt":       {Search: "anime hurt", Verb: "se sintió herido por", Participle: "heridos"},
	"suffer":     {Search: "anime suffer", Verb: "sufrió por", Participle: "sufridos"},
	"pain":       {Search: "anime pain", Verb: "sintió dolor por", Participle: "doloridos"},
	"disappoint": {Search: "anime disappointed", Verb: "se decepcionó de", Participle: "decepcionados"},
	"regret":     {Search: "anime regret", Verb: "se arrepintió con", Participle: "arrepentidos"},
	"miss":       {Search: "anime miss catch", Verb: "falló en atrapar a", Participle: "no atrapados"},
	"worry":      {Search: "anime worry", Verb: "se preocupó por", Participle: "preocupados"},
	"nervous":    {Search: "anime nervous", Verb: "se puso nervioso con", Participle: "puestos nerviosos"},
	"panic":      {Search: "anime panic", Verb: "entró en pánico por", Participle: "aterrorizados"},
	"scared":     {Search: "anime scared", Verb: "se asustó de", Participle: "asustados"},
	"fear":       {Search: "anime fear", Verb: "le tuvo miedo a", Participle: "temidos"},
	"shiver":     {Search: "anime shiver", Verb: "tembló por", Participle: "estremecidos"},
	"hide":       {Search: "anime hide scared", Verb: "se escondió de", Participle: "escondidos"},
	"angry":      {Search: "anime angry", Verb: "se enfadó con", Participle: "enfadados"},
	"rage":       {Search: "anime rage quit", Verb: "rage quiteó por", Participle: "afectados por rage quit"},
	"mad":        {Search: "anime mad", Verb: "se enojó con", Participle: "enojados"},
	"glare":      {Search: "anime glare", Verb: "fulminó con la mirada a", Participle: "fulminados"},
	"death":      {Search: "anime death stare", Verb: "miró con odio a", Participle: "odiados"},
	"pout":       {Search: "anime pout", Verb: "hizo pucheros a", Participle: "puchereados"},
	"hmph":       {Search: "anime hmph", Verb: "ignoró con desprecio a", Participle: "despreciados"},
	"tantrum":    {Search: "anime tantrum", Verb: "hizo berrinche a", Participle: "berrinchados"},
	"scold":      {Search: "anime scold", Verb: "regañó a", Participle: "regañados"},
	"yell":       {Search: "anime yell", Verb: "gritó a", Participle: "gritados"},
	"scream":     {Search: "anime scream", Verb: "le gritó a", Participle: "gritados"},
	"baka":       {Search: "anime baka", Verb: "llamó idiota a", Participle: "llamados idiotas"},
	"tsun":       {Search: "anime tsundere", Verb: "actuó tsundere con", Participle: "tsundeados"},
	"jealous":    {Search: "anime jealous", Verb: "tuvo celos de", Participle: "celados"},
	"happy":      {Search: "anime happy", Verb: "se alegró por", Participle: "alegrados"},
	"smile":      {Search: "anime smile", Verb: "sonrió a", Participle: "sonreídos"},
	"grin":       {Search: "anime grin", Verb: "sonrió ampliamente a", Participle: "sonreídos"},
	"laugh":      {Search: "anime laugh", Verb: "se rió con", Participle: "hechos reír"},
	"lol":        {Search: "anime laughing", Verb: "se carcajeó con", Participle: "carcajeados"},
	"giggle":     {Search: "anime giggle", Verb: "se rió tontamente de", Participle: "divertidos"},
	"joy":        {Search: "anime joy", Verb: "saltó de alegría con", Participle: "alegrados"},
	"excited":    {Search: "anime excited", Verb: "se emocionó con", Participle: "emocionados"},
	"hype":       {Search: "anime hype", Verb: "se hypeó con", Participle: "hypeados"},
	"celebrate":  {Search: "anime celebrate", Verb: "celebró con", Participle: "celebrados"},
	"cheer":      {Search: "anime cheer", Verb: "animó a", Participle: "animados"},
	"proud":      {Search: "anime proud", Verb: "se enorgulleció de", Participle: "enorgullecidos"},
	"victory":    {Search: "anime victory", Verb: "celebró victoria con", Participle: "victoriosos"},
	"win":        {Search: "anime win", Verb: "ganó contra", Participle: "derrotados"},
	"jump":       {Search: "anime jump joy", Verb: "saltó de alegría con", Participle: "hechos saltar"},
	"spin":       {Search: "anime spin happy", Verb: "giró de felicidad con", Participle: "hechos girar"},
	"blush":      {Search: "anime blush", Verb: "se sonrojó por", Participle: "sonrojados"},
	"shy":        {Search: "anime shy", Verb: "se avergonzó con", Participle: "avergonzados"},
	"awkward":    {Search: "anime awkward", Verb: "tuvo un momento incómodo con", Participle: "incomodados"},
	"embarrass":  {Search: "anime embarrassed", Verb: "avergonzó a", Participle: "avergonzados"},
	"flustered":  {Search: "anime flustered", Verb: "se puso rojo por", Participle: "enrojecidos"},
	"steam":      {Search: "anime steam head", Verb: "le salió humo por", Participle: "humeados"},
	"lewd":       {Search: "anime lewd", Verb: "tuvo pensamientos sucios con", Participle: "deseados"},
	"nosebleed":  {Search: "anime nosebleed", Verb: "sangró por la nariz por", Participle: "excitados"},
	"perv":       {Search: "anime pervert", Verb: "acosó a", Participle: "acosados"},
	"think":      {Search: "anime think", Verb: "pensó en", Participle: "pensados"},
	"wonder":     {Search: "anime wonder", Verb: "se preguntó sobre", Participle: "cuestionados"},
	"confused":   {Search: "anime confused", Verb: "se confundió con", Participle: "confundidos"},
	"question":   {Search: "anime question mark", Verb: "cuestionó a", Participle: "cuestionados"},
	"curious":    {Search: "anime curious", Verb: "tuvo curiosidad por", Participle: "investigados"},
	"suspect":    {Search: "anime suspicious", Verb: "sospechó de", Participle: "sospechados"},
	"doubt":      {Search: "anime doubt", Verb: "dudó de", Participle: "dudados"},
	"shrug":      {Search: "anime shrug", Verb: "se encogió de hombros ante", Participle: "ignorados"},
	"idk":        {Search: "anime shrug", Verb: "no supo qué decir a", Participle: "dejados en duda"},
	"realize":    {Search: "anime realize", Verb: "se dio cuenta por", Participle: "iluminados"},
	"eureka":     {Search: "anime idea", Verb: "tuvo una idea por", Participle: "inspirados"},
	"smug":       {Search: "anime smug", Verb: "miró con superioridad a", Participle: "menospreciados"},
	"smugdance":  {Search: "anime smug dance", Verb: "bailó presumido ante", Participle: "humillados"},
	"cool":       {Search: "anime cool", Verb: "actuó cool frente a", Participle: "impresionados"},
	"flex":       {Search: "anime flex", Verb: "flexeó ante", Participle: "flexeados"},
	"show":       {Search: "anime show off", Verb: "presumió ante", Participle: "impresionados"},
	"mock":       {Search: "anime mock", Verb: "se burló de", Participle: "burlados"},
	"tease":      {Search: "anime tease", Verb: "molestó a", Participle: "molestados"},
	"taunt":      {Search: "anime taunt", Verb: "provocó a", Participle: "provocados"},
	"sass":       {Search: "anime sassy", Verb: "le tiró shade a", Participle: "shadeados"},
	"sip":        {Search: "anime sip tea", Verb: "bebió té juzgando a", Participle: "juzgados"},
	"judge":      {Search: "anime judging", Verb: "juzgó a", Participle: "juzgados"},
	"stare":      {Search: "anime stare", Verb: "miró fijamente a", Participle: "observados"},
	"peek":       {Search: "anime peek", Verb: "espió a", Participle: "espiados"},
	"watch":      {Search: "anime watching", Verb: "observó a", Participle: "observados"},
	"notice":     {Search: "anime notice", Verb: "notó a", Participle: "notados"},
	"ignore":     {Search: "anime ignore", Verb: "ignoró a", Participle: "ignorados"},
	"reject":     {Search: "anime reject", Verb: "rechazó a", Participle: "rechazados"},
	"nope":       {Search: "anime nope", Verb: "dijo nope a", Participle: "negados"},
	"disgust":    {Search: "anime disgust", Verb: "miró con asco a", Participle: "asqueados"},
	"cringe":     {Search: "anime cringe", Verb: "le dio cringe por", Participle: "cringeados"},
	"facepalm":   {Search: "anime facepalm", Verb: "se facepalmeó por", Participle: "facepalmeados"},
	"bruh":       {Search: "anime bruh", Verb: "dijo bruh a", Participle: "bruheados"},
	"done":       {Search: "anime done", Verb: "ya no aguanta a", Participle: "agotados"},
	"dead":       {Search: "anime dead inside", Verb: "murió por dentro por", Participle: "muertos por dentro"},
	"sigh":       {Search: "anime sigh", Verb: "suspiró por", Participle: "suspirados"},
	"tired":      {Search: "anime tired", Verb: "se cansó de", Participle: "cansados"},
	"dramatic":   {Search: "anime dramatic", Verb: "actuó dramático con", Participle: "dramatizados"},
	"gasp":       {Search: "anime gasp", Verb: "jadeó sorprendido por", Participle: "impactados"},
	"shock":      {Search: "anime electric shock", Verb: "electrocutó a", Participle: "electrocutados"},
	"faint":      {Search: "anime faint", Verb: "se desmayó por", Participle: "desmayados"},
	"collapse":   {Search: "anime collapse", Verb: "colapsó frente a", Participle: "colapsados"},
	"revive":     {Search: "anime revive", Verb: "revivió a", Participle: "revividos"},
	"die":        {Search: "anime dramatic death", Verb: "murió frente a", Participle: "muertos"},
	"wasted":     {Search: "anime wasted", Verb: "quedó wasted con", Participle: "wasteados"},
	"rip":        {Search: "anime rip", Verb: "descansó en paz con", Participle: "despedidos"},
	"ghost":      {Search: "anime ghost", Verb: "fantasmeó a", Participle: "ghosteados"},
	"comfort":    {Search: "anime comfort", Verb: "consoló a", Participle: "consolados"},
	"support":    {Search: "anime support", Verb: "apoyó a", Participle: "apoyados"},
	"encourage":  {Search: "anime encourage", Verb: "motivó a", Participle: "motivados"},
	"believe":    {Search: "anime believe", Verb: "creyó en", Participle: "apoyados"},
	"protect":    {Search: "anime protect", Verb: "protegió a", Participle: "protegidos"},
	"save":       {Search: "anime save", Verb: "salvó a", Participle: "salvados"},
	"rescue":     {Search: "anime rescue", Verb: "rescató a", Participle: "rescatados"},
	"heal":       {Search: "anime heal", Verb: "sanó a", Participle: "sanados"},
	"bandage":    {Search: "anime bandage", Verb: "vendó a", Participle: "vendados"},
	"medicine":   {Search: "anime medicine", Verb: "dio medicina a", Participle: "medicados"},
	"care":       {Search: "anime take care", Verb: "cuidó de", Participle: "cuidados"},
	"praise":     {Search: "anime praise", Verb: "elogió a", Participle: "elogiados"},
	"thanks":     {Search: "anime thank you", Verb: "agradeció a", Participle: "agradecidos"},
	"sorry":      {Search: "anime sorry", Verb: "se disculpó con", Participle: "disculpados"},
	"forgive":    {Search: "anime forgive", Verb: "perdonó a", Participle: "perdonados"},
	"eat":        {Search: "anime eating", Verb: "comió frente a", Participle: "vistos comiendo"},
	"feed":       {Search: "anime feed", Verb: "alimentó a", Participle: "alimentados"},
	"cook":       {Search: "anime cooking", Verb: "cocinó para", Participle: "alimentados"},
	"bake":       {Search: "anime baking", Verb: "horneó para", Participle: "obsequiados con horneados"},
	"drink":      {Search: "anime drink", Verb: "bebió con", Participle: "acompañados en la bebida"},
	"coffee":     {Search: "anime coffee", Verb: "tomó café con", Participle: "compañeros de café"},
	"tea":        {Search: "anime tea", Verb: "tomó té con", Participle: "compañeros de té"},
	"beer":       {Search: "anime beer", Verb: "bebió cerveza con", Participle: "compañeros de cerveza"},
	"sake":       {Search: "anime sake", Verb: "tomó sake con", Participle: "compañeros de sake"},
	"drunk":      {Search: "anime drunk", Verb: "se emborrachó con", Participle: "emborrachados"},
	"cheers":     {Search: "anime cheers toast", Verb: "brindó con", Participle: "celebrados con brindis"},
	"lick":       {Search: "anime lick", Verb: "lamió a", Participle: "lamidos"},
	"nom":        {Search: "anime nom cute", Verb: "hizo ñam a", Participle: "ñameados"},
	"hungry":     {Search: "anime hungry", Verb: "tuvo hambre con", Participle: "hambreados"},
	"yummy":      {Search: "anime yummy", Verb: "disfrutó comida con", Participle: "disfrutados"},
	"share":      {Search: "anime share food", Verb: "compartió comida con", Participle: "compartidas comidas"},
	"ramen":      {Search: "anime ramen", Verb: "comió ramen con", Participle: "rameneados"},
	"icecream":   {Search: "anime ice cream", Verb: "comió helado con", Participle: "heladeados"},
	"cake":       {Search: "anime cake", Verb: "comió pastel con", Participle: "pastelados"},
	"candy":      {Search: "anime candy", Verb: "comió dulces con", Participle: "endulzados"},
	"sleep":      {Search: "anime sleep", Verb: "durmió junto a", Participle: "acompañados"},
	"nap":        {Search: "anime nap", Verb: "tomó siesta con", Participle: "siesteados"},
	"sleepy":     {Search: "anime sleepy", Verb: "tuvo sueño con", Participle: "somnolientos"},
	"yawn":       {Search: "anime yawn", Verb: "bostezó frente a", Participle: "hechos bostezar"},
	"wake":       {Search: "anime wake up", Verb: "despertó a", Participle: "despertados"},
	"dream":      {Search: "anime dream", Verb: "soñó con", Participle: "soñados"},
	"nightmare":  {Search: "anime nightmare", Verb: "tuvo pesadilla con", Participle: "atormentados"},
	"lazy":       {Search: "anime lazy", Verb: "flojeó con", Participle: "flojeados"},
	"relax":      {Search: "anime relax", Verb: "se relajó con", Participle: "relajados"},
	"rest":       {Search: "anime rest", Verb: "descansó con", Participle: "descansados"},
	"blanket":    {Search: "anime blanket", Verb: "arropó a", Participle: "arropados"},
	"pillow":     {Search: "anime pillow", Verb: "lanzó almohada a", Participle: "almohadazos"},
	"dance":      {Search: "anime dance", Verb: "bailó con", Participle: "bailados"},
	"waltz":      {Search: "anime waltz", Verb: "valsó con", Participle: "valseados"},
	"sing":       {Search: "anime sing", Verb: "cantó para", Participle: "entonados"},
	"karaoke":    {Search: "anime karaoke", Verb: "cantó karaoke con", Participle: "karaokeados"},
	"music":      {Search: "anime music", Verb: "escuchó música con", Participle: "musicados"},
	"guitar":     {Search: "anime guitar", Verb: "tocó guitarra para", Participle: "guitarreados"},
	"piano":      {Search: "anime piano", Verb: "tocó piano para", Participle: "pianeados"},
	"drum":       {Search: "anime drums", Verb: "tocó batería para", Participle: "drummeados"},
	"headbang":   {Search: "anime headbang", Verb: "headbangeó con", Participle: "headbangeados"},
	"rave":       {Search: "anime rave", Verb: "raveó con", Participle: "raveados"},
	"vibe":       {Search: "anime vibing", Verb: "vibeó con", Participle: "vibeados"},
	"groove":     {Search: "anime groove", Verb: "grooveó con", Participle: "grooveados"},
	"play":       {Search: "anime playing games", Verb: "jugó con", Participle: "jugadores"},
	"game":       {Search: "anime gaming", Verb: "gameó con", Participle: "gameados"},
	"lose":       {Search: "anime lose", Verb: "perdió contra", Participle: "ganadores"},
	"gg":         {Search: "anime good game", Verb: "dijo GG a", Participle: "felicitados"},
	"noob":       {Search: "anime noob", Verb: "llamó noob a", Participle: "llamados noobs"},
	"pro":        {Search: "anime pro gamer", Verb: "proclamó pro a", Participle: "proclamados pros"},
	"camp":       {Search: "anime camp", Verb: "acampó con", Participle: "acampeados"},
	"rps":        {Search: "anime rock paper scissors", Verb: "jugó piedra papel tijera con", Participle: "retados"},
	"run":        {Search: "anime run", Verb: "corrió hacia", Participle: "perseguidos"},
	"walk":       {Search: "anime walk", Verb: "caminó con", Participle: "acompañados"},
	"fly":        {Search: "anime fly", Verb: "voló con", Participle: "compañeros de vuelo"},
	"teleport":   {Search: "anime teleport", Verb: "se teletransportó a", Participle: "teletransportados"},
	"dodge":      {Search: "anime dodge", Verb: "esquivó a", Participle: "esquivados"},
	"fall":       {Search: "anime fall", Verb: "cayó frente a", Participle: "caídos"},
	"trip":       {Search: "anime trip fall", Verb: "tropezó frente a", Participle: "tropezados"},
	"slide":      {Search: "anime slide", Verb: "se deslizó hacia", Participle: "deslizados"},
	"flip":       {Search: "anime flip", Verb: "hizo flip ante", Participle: "flippeados"},
	"roll":       {Search: "anime roll", Verb: "rodó hacia", Participle: "rodados"},
	"crawl":      {Search: "anime crawl", Verb: "gateó hacia", Participle: "gateados"},
	"sneak":      {Search: "anime sneak", Verb: "se escabulló hacia", Participle: "espiados"},
	"splash":     {Search: "anime splash water", Verb: "salpicó a", Participle: "salpicados"},
	"spray":      {Search: "anime spray", Verb: "roció a", Participle: "rociados"},
	"swim":       {Search: "anime swim", Verb: "nadó con", Participle: "compañeros de natación"},
	"magic":      {Search: "anime magic", Verb: "usó magia en", Participle: "hechizados"},
	"spell":      {Search: "anime cast spell", Verb: "lanzó hechizo a", Participle: "hechizados"},
	"curse":      {Search: "anime curse", Verb: "maldijo a", Participle: "maldecidos"},
	"bless":      {Search: "anime bless", Verb: "bendijo a", Participle: "bendecidos"},
	"power":      {Search: "anime power up", Verb: "se empoderó con", Participle: "empoderados"},
	"transform":  {Search: "anime transform", Verb: "se transformó ante", Participle: "testigos"},
	"fusion":     {Search: "anime fusion", Verb: "se fusionó con", Participle: "fusionados"},
	"summon":     {Search: "anime summon", Verb: "invocó a", Participle: "invocados"},
	"banish":     {Search: "anime banish", Verb: "desterró a", Participle: "desterrados"},
	"freeze":     {Search: "anime freeze", Verb: "congeló a", Participle: "congelados"},
	"burn":       {Search: "anime fire", Verb: "quemó a", Participle: "quemados"},
	"kamehameha": {Search: "anime kamehameha", Verb: "lanzó kamehameha a", Participle: "kamehameheados"},
	"rasengan":   {Search: "anime rasengan", Verb: "lanzó rasengan a", Participle: "rasenganeados"},
	"uwu":        {Search: "anime uwu", Verb: "hizo uwu a", Participle: "uwueados"},
	"owo":        {Search: "anime owo", Verb: "hizo owo a", Participle: "oweados"},
	"nya":        {Search: "anime nya cat", Verb: "hizo nya a", Participle: "nyados"},
	"meow":       {Search: "anime meow", Verb: "maulló a", Participle: "maullados"},
	"wag":        {Search: "anime wag tail", Verb: "movió la colita a", Participle: "waggeados"},
	"purr":       {Search: "anime purr", Verb: "ronroneó a", Participle: "ronroneados"},
	"sniff":      {Search: "anime sniff", Verb: "olfateó a", Participle: "olfateados"},
	"rawr":       {Search: "anime rawr", Verb: "hizo rawr a", Participle: "rawreados"},
	"sparkle":    {Search: "anime sparkle eyes", Verb: "brilló ante", Participle: "deslumbrados"},
	"kawaii":     {Search: "anime kawaii", Verb: "actuó kawaii con", Participle: "kawaiizados"},
	"cute":       {Search: "anime cute", Verb: "fue cute con", Participle: "cuteados"},
	"adorable":   {Search: "anime adorable", Verb: "fue adorable con", Participle: "adorados"},
	"dab":        {Search: "anime dab", Verb: "hizo dab a", Participle: "dabbeados"},
	"tpose":      {Search: "anime t pose", Verb: "hizo T-pose ante", Participle: "dominados"},
	"default":    {Search: "anime default dance", Verb: "default danceó a", Participle: "defaulteados"},
	"floss":      {Search: "anime floss dance", Verb: "hizo floss a", Participle: "flosseados"},
	"rickroll":   {Search: "anime rick roll", Verb: "rickrolleó a", Participle: "rickrolleados"},
	"prank":      {Search: "anime prank", Verb: "bromeó a", Participle: "prankeados"},
	"trap":       {Search: "anime trap card", Verb: "activó trap card a", Participle: "trapeados"},
	"ded":        {Search: "anime ded", Verb: "murió ante", Participle: "dedeados"},
	"ascend":     {Search: "anime ascend", Verb: "ascendió ante", Participle: "ascendidos"},
	"descend":    {Search: "anime descend", Verb: "descendió ante", Participle: "descendidos"},
	"vanish":     {Search: "anime vanish", Verb: "desapareció de", Participle: "desaparecidos"},
	"appear":     {Search: "anime appear", Verb: "apareció ante", Participle: "aparecidos"},
	"poof":       {Search: "anime poof", Verb: "hizo poof a", Participle: "poofeados"},
	"yeehaw":     {Search: "anime cowboy", Verb: "hizo yeehaw a", Participle: "yeehaweados"},
	"ara":        {Search: "anime ara ara", Verb: "hizo ara ara a", Participle: "ara araeados"},
	"rero":       {Search: "anime rero", Verb: "hizo rero rero a", Participle: "reroeados"},
	"ora":        {Search: "anime ora ora", Verb: "ORA ORA a", Participle: "orados"},
	"muda":       {Search: "anime muda muda", Verb: "MUDA MUDA a", Participle: "mudados"},
	"za":         {Search: "anime za warudo", Verb: "detuvo el tiempo ante", Participle: "timeados"},
	"teehee":     {Search: "anime teehee", Verb: "hizo teehee a", Participle: "teeheeados"},
	"psycho":     {Search: "anime yandere", Verb: "psicopateó a", Participle: "acosados"},
	"creep":      {Search: "anime creepy", Verb: "creepeó a", Participle: "creepeados"},
	"simp":       {Search: "anime simp", Verb: "simpeó a", Participle: "simpeados"},
	"stan":       {Search: "anime stan", Verb: "staneó a", Participle: "staneados"},
	"sus":        {Search: "anime sus suspicious", Verb: "fue sus con", Participle: "suseados"},
	"bonkai":     {Search: "anime bonkai", Verb: "liberó bonkai ante", Participle: "bonkaieados"},
	"bankai":     {Search: "anime bankai", Verb: "liberó bankai ante", Participle: "bankaieados"},
	"gear":       {Search: "anime gear second", Verb: "activó gear ante", Participle: "geareados"},
	"plus":       {Search: "anime plus ultra", Verb: "fue PLUS ULTRA con", Participle: "plus ultrados"},
	"detroit":    {Search: "anime detroit smash", Verb: "DETROIT SMASH a", Participle: "detroit smasheados"},
	"texas":      {Search: "anime texas smash", Verb: "TEXAS SMASH a", Participle: "texas smasheados"},
	"birthday":   {Search: "anime birthday", Verb: "celebró cumpleaños con", Participle: "festejados"},
	"party":      {Search: "anime party", Verb: "festejó con", Participle: "partyeados"},
	"gift":       {Search: "anime gift", Verb: "dio regalo a", Participle: "regalados"},
	"present":    {Search: "anime present", Verb: "sorprendió con regalo a", Participle: "sorprendidos"},
	"flower":     {Search: "anime give flower", Verb: "dio flores a", Participle: "floreados"},
	"rose":       {Search: "anime rose", Verb: "dio una rosa a", Participle: "roseados"},
	"chocolate":  {Search: "anime chocolate", Verb: "dio chocolate a", Participle: "chocolatados"},
	"valentine":  {Search: "anime valentine", Verb: "san valentineó a", Participle: "enamorados"},
	"xmas":       {Search: "anime christmas", Verb: "deseó feliz navidad a", Participle: "navideños"},
	"newyear":    {Search: "anime new year", Verb: "celebró año nuevo con", Participle: "celebrantes"},
	"halloween":  {Search: "anime halloween", Verb: "asustó en halloween a", Participle: "halloweeneados"},
	"cosplay":    {Search: "anime cosplay", Verb: "cosplayeó para", Participle: "cosplayeados"},
	"selfie":     {Search: "anime selfie", Verb: "tomó selfie con", Participle: "selfieados"},
	"photo":      {Search: "anime photo", Verb: "fotografió a", Participle: "fotografiados"},
	"call":       {Search: "anime phone call", Verb: "llamó a", Participle: "llamados"},
	"text":       {Search: "anime texting", Verb: "texteó a", Participle: "texteados"},
	"block":      {Search: "anime block", Verb: "bloqueó a", Participle: "bloqueados"},
	"unblock":    {Search: "anime unblock", Verb: "desbloqueó a", Participle: "desbloqueados"},
	"like":       {Search: "anime thumbs up", Verb: "dio like a", Participle: "likeados"},
	"dislike":    {Search: "anime thumbs down", Verb: "dio dislike a", Participle: "dislikeados"},
	"subscribe":  {Search: "anime subscribe", Verb: "se suscribió a", Participle: "suscritos"},
	"follow":     {Search: "anime follow", Verb: "siguió a", Participle: "seguidos"},
	"unfollow":   {Search: "anime unfollow", Verb: "dejó de seguir a", Participle: "unfolloweados"},
	"clap":       {Search: "anime clap", Verb: "aplaudió a", Participle: "aplaudidos"},
	"slowclap":   {Search: "anime slow clap", Verb: "aplaudió lento a", Participle: "aplaudidos sarcásticamente"},
	"bow":        {Search: "anime bow", Verb: "se inclinó ante", Participle: "reverenciados"},
	"salute":     {Search: "anime salute", Verb: "saludó militarmente a", Participle: "saludados"},
	"pray":       {Search: "anime pray", Verb: "rezó por", Participle: "rezados"},
	"wish":       {Search: "anime wish", Verb: "deseó algo para", Participle: "deseados"},
	"point":      {Search: "anime point", Verb: "señaló a", Participle: "señalados"},
	"shush":      {Search: "anime shush", Verb: "calló a", Participle: "callados"},
	"silence":    {Search: "anime silence", Verb: "silenció a", Participle: "silenciados"},
	"clean":      {Search: "anime clean", Verb: "limpió para", Participle: "beneficiados con limpieza"},
	"work":       {Search: "anime work", Verb: "trabajó con", Participle: "compañeros de trabajo"},
	"study":      {Search: "anime study", Verb: "estudió con", Participle: "compañeros de estudio"},
	"read":       {Search: "anime read", Verb: "leyó con", Participle: "leídos"},
	"write":      {Search: "anime write", Verb: "escribió a", Participle: "escritos"},
	"draw":       {Search: "anime draw", Verb: "dibujó a", Participle: "dibujados"},
	"paint":      {Search: "anime paint", Verb: "pintó a", Participle: "pintados"},
	"build":      {Search: "anime build", Verb: "construyó con", Participle: "constructores"},
	"break":      {Search: "anime break", Verb: "rompió algo de", Participle: "rotos"},
	"fix":        {Search: "anime fix", Verb: "arregló algo para", Participle: "arreglados"},
	"steal":      {Search: "anime steal", Verb: "robó de", Participle: "robados"},
	"give":       {Search: "anime give", Verb: "le dio algo a", Participle: "recibidos"},
	"take":       {Search: "anime take", Verb: "tomó de", Participle: "tomados"},
	"grab":       {Search: "anime grab", Verb: "agarró a", Participle: "agarrados"},
	"push":       {Search: "anime push", Verb: "empujó a", Participle: "empujados"},
	"pull":       {Search: "anime pull", Verb: "jaló a", Participle: "jalados"},
	"drag":       {Search: "anime drag", Verb: "arrastró a", Participle: "arrastrados"},
	"lift":       {Search: "anime lift", Verb: "levantó a", Participle: "levantados"},
	"drop":       {Search: "anime drop", Verb: "dejó caer a", Participle: "dropeados"},
	"catch":      {Search: "anime catch", Verb: "atrapó a", Participle: "atrapados"},
}

type gifTarget struct {
	Name    string         `json:"name"`
	Actions map[string]int `json:"actions"`
	Total   int            `json:"total"`
}

type gifSender struct {
	Name    string                `json:"name"`
	Targets map[string]*gifTarget `json:"targets"`
}

type gifConfig struct {
	ActiveGroups []string `json:"active_groups"`
}

var (
	gifHTTP       = &http.Client{Timeout: 30 * time.Second}
	gifTenorURL   = "https://tenor.googleapis.com/v2/search"
	gifTenorKey   = func() string { return os.Getenv("TENOR_API_KEY") }
	gifSendFn     = gifEnviarGif
	gifNow        = time.Now
	gifCooldown   = 2 * time.Minute
	gifCooldownMu sync.Mutex
	gifCooldowns  = map[string]map[string]map[string][]int64{}
	gifStatsMu    sync.Mutex
)

const (
	gifClientKey       = "whatsbot_self"
	gifSpamMax         = 3
	gifPrivateSendSize = "tinymp4"
)

// Gif porta addons/gif.js: interacciones animadas con Tenor.
type Gif struct{}

func init() { core.Register(Gif{}) }

func (Gif) Commands() []string { return []string{"g", "ongif", "offgif", "topg"} }

var gifStatsDir = func() string { return core.DataPath("assets", "statistics") }

func gifConfigPath() string { return filepath.Join(gifStatsDir(), "gif_config.json") }

func gifInteractionsPath() string {
	return filepath.Join(gifStatsDir(), "interactions.json")
}

func gifLoadConfig() gifConfig {
	var cfg gifConfig
	if err := core.ReadJSON(gifConfigPath(), &cfg); err != nil {
		return gifConfig{ActiveGroups: []string{}}
	}
	if cfg.ActiveGroups == nil {
		cfg.ActiveGroups = []string{}
	}
	return cfg
}

func gifSaveConfig(cfg gifConfig) error { return core.WriteJSON(gifConfigPath(), cfg) }

func gifLoadStats() map[string]map[string]*gifSender {
	stats := map[string]map[string]*gifSender{}
	if err := core.ReadJSON(gifInteractionsPath(), &stats); err != nil {
		return map[string]map[string]*gifSender{}
	}
	if stats == nil {
		return map[string]map[string]*gifSender{}
	}
	return stats
}

func gifSaveStats(stats map[string]map[string]*gifSender) error {
	return core.WriteJSON(gifInteractionsPath(), stats)
}

// gifCheckSpam replica checkSpam(): 3 usos cada 2 minutos por acción.
func gifCheckSpam(grupo, remitente, accion string, ahora time.Time) bool {
	gifCooldownMu.Lock()
	defer gifCooldownMu.Unlock()
	if gifCooldowns[grupo] == nil {
		gifCooldowns[grupo] = map[string]map[string][]int64{}
	}
	if gifCooldowns[grupo][remitente] == nil {
		gifCooldowns[grupo][remitente] = map[string][]int64{}
	}
	now := ahora.UnixMilli()
	historial := gifCooldowns[grupo][remitente][accion]
	filtrado := historial[:0]
	for _, ts := range historial {
		if now-ts < gifCooldown.Milliseconds() {
			filtrado = append(filtrado, ts)
		}
	}
	if len(filtrado) >= gifSpamMax {
		gifCooldowns[grupo][remitente][accion] = filtrado
		return false
	}
	gifCooldowns[grupo][remitente][accion] = append(filtrado, now)
	return true
}

func gifExtractNumber(id string) string {
	if id == "" {
		return "Desconocido"
	}
	return strings.SplitN(id, "@", 2)[0]
}

// gifRegisterInteraction replica registerInteraction().
func gifRegisterInteraction(chatID, senderID, senderName, receiverID, receiverName, action string) int {
	gifStatsMu.Lock()
	defer gifStatsMu.Unlock()
	stats := gifLoadStats()
	if stats[chatID] == nil {
		stats[chatID] = map[string]*gifSender{}
	}
	sender := stats[chatID][senderID]
	if sender == nil {
		sender = &gifSender{Name: senderName, Targets: map[string]*gifTarget{}}
		stats[chatID][senderID] = sender
	} else if senderName != "" && senderName != "Alguien" {
		sender.Name = senderName
	}
	if sender.Targets == nil {
		sender.Targets = map[string]*gifTarget{}
	}
	target := sender.Targets[receiverID]
	if target == nil {
		target = &gifTarget{Name: receiverName, Actions: map[string]int{}}
		sender.Targets[receiverID] = target
	}
	if receiverName != "" && !strings.Contains(receiverName, "@") {
		target.Name = receiverName
	}
	if target.Actions == nil {
		target.Actions = map[string]int{}
	}
	target.Actions[action]++
	target.Total++
	_ = gifSaveStats(stats)
	return target.Actions[action]
}

type gifTenorResponse struct {
	Results []struct {
		MediaFormats map[string]struct {
			URL string `json:"url"`
		} `json:"media_formats"`
	} `json:"results"`
}

// gifBuscarGif consulta Tenor y devuelve la URL del primer formato preferido.
func gifBuscarGif(termino string) string {
	key := gifTenorKey()
	if key == "" {
		return ""
	}
	consulta := gifTenorURL + "?q=" + url.QueryEscape(termino) +
		"&key=" + url.QueryEscape(key) + "&client_key=" + gifClientKey +
		"&limit=40&contentfilter=off&ar_range=wide"
	resp, err := gifHTTP.Get(consulta)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var parsed gifTenorResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil || len(parsed.Results) == 0 {
		return ""
	}
	formats := parsed.Results[0].MediaFormats
	for _, clave := range []string{"tinymp4", "mp4", "gif"} {
		if f, ok := formats[clave]; ok && f.URL != "" {
			return f.URL
		}
	}
	return ""
}

// gifEnviarGif descarga el GIF y lo reenvía como vídeo con gifPlayback.
func gifEnviarGif(ctx *core.Context, jid types.JID, gifURL, caption string, mentions []types.JID) error {
	if ctx == nil || ctx.Client == nil {
		return fmt.Errorf("cliente no disponible")
	}
	resp, err := gifHTTP.Get(gifURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024*1024))
	if err != nil {
		return err
	}
	up, err := ctx.Client.Upload(context.Background(), data, whatsmeow.MediaVideo)
	if err != nil {
		return err
	}
	msg := &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
		URL:           proto.String(up.URL),
		Mimetype:      proto.String("video/mp4"),
		Caption:       proto.String(caption),
		FileSHA256:    up.FileSHA256,
		FileEncSHA256: up.FileEncSHA256,
		FileLength:    proto.Uint64(up.FileLength),
		MediaKey:      up.MediaKey,
		DirectPath:    proto.String(up.DirectPath),
		GifPlayback:   proto.Bool(true),
	}}
	if len(mentions) > 0 {
		jids := make([]string, 0, len(mentions))
		for _, m := range mentions {
			jids = append(jids, m.String())
		}
		msg.VideoMessage.ContextInfo = &waE2E.ContextInfo{MentionedJID: jids}
	}
	_, err = ctx.Client.SendMessage(context.Background(), jid, msg)
	return err
}

func gifSendTop(ctx *core.Context) error {
	chatID := ctx.ChatJID().String()
	gifStatsMu.Lock()
	stats := gifLoadStats()
	gifStatsMu.Unlock()
	grupo := stats[chatID]
	if len(grupo) == 0 {
		return ctx.Reply("📭 Aún no hay interacciones registradas en este grupo.")
	}
	type inter struct {
		SenderName string
		TargetName string
		Total      int
	}
	var todas []inter
	for _, sender := range grupo {
		for _, target := range sender.Targets {
			todas = append(todas, inter{SenderName: sender.Name, TargetName: target.Name, Total: target.Total})
		}
	}
	sort.SliceStable(todas, func(i, j int) bool { return todas[i].Total > todas[j].Total })
	if len(todas) > 10 {
		todas = todas[:10]
	}
	if len(todas) == 0 {
		return ctx.Reply("📭 Aún no hay interacciones válidas.")
	}
	var b strings.Builder
	b.WriteString("🏆 *TOP INTERACCIONES DEL GRUPO*\n\n")
	for i, it := range todas {
		fmt.Fprintf(&b, "%d. *%s* ➡️ *%s* (%d veces)\n", i+1, it.SenderName, it.TargetName, it.Total)
	}
	return ctx.Reply(b.String())
}

func gifNombreConocido(stats map[string]map[string]*gifSender, chatID, targetID string) string {
	if grupo := stats[chatID]; grupo != nil {
		if sender := grupo[targetID]; sender != nil && sender.Name != "" {
			return sender.Name
		}
	}
	return ""
}

func gifNombreStore(ctx *core.Context, targetID string) string {
	if ctx.Store == nil {
		return ""
	}
	contact, ok := ctx.Store.Get(targetID)
	if !ok {
		return ""
	}
	if v, ok := contact["name"].(string); ok && v != "" {
		return v
	}
	if v, ok := contact["notify"].(string); ok {
		return v
	}
	return ""
}

func (Gif) Handle(ctx *core.Context) error {
	if ctx == nil || !wagoIsGroup(ctx) {
		return nil
	}
	chatID := ctx.ChatJID().String()
	senderID := ctx.SenderJID().String()
	senderName := "Alguien"
	if ctx.Message != nil && ctx.Message.Info.PushName != "" {
		senderName = ctx.Message.Info.PushName
	}
	comando := ctx.Command

	cfg := gifLoadConfig()

	if comando == "ongif" || comando == "offgif" {
		info, err := wagoGroupInfo(ctx)
		if err != nil {
			slog.Error("❌ [gif] Error:", "error", err)
			return ctx.React("❌")
		}
		if !wagoSenderIsAdminOrOwner(ctx, info) {
			return ctx.React("⛔")
		}
		if comando == "ongif" {
			if wagoContains(cfg.ActiveGroups, chatID) {
				return ctx.Reply("ℹ️ Ya estaban activas.")
			}
			cfg.ActiveGroups = append(cfg.ActiveGroups, chatID)
			if err := gifSaveConfig(cfg); err != nil {
				return ctx.React("❌")
			}
			return ctx.Reply("✅ *Interacciones activadas.*")
		}
		idx := -1
		for i, g := range cfg.ActiveGroups {
			if g == chatID {
				idx = i
				break
			}
		}
		if idx == -1 {
			return ctx.Reply("ℹ️ No estaban activas.")
		}
		cfg.ActiveGroups = append(cfg.ActiveGroups[:idx], cfg.ActiveGroups[idx+1:]...)
		if err := gifSaveConfig(cfg); err != nil {
			return ctx.React("❌")
		}
		return ctx.Reply("❌ *Interacciones desactivadas.*")
	}

	if comando == "topg" {
		if !wagoContains(cfg.ActiveGroups, chatID) {
			return nil
		}
		return gifSendTop(ctx)
	}

	if comando != "g" {
		return nil
	}
	if !wagoContains(cfg.ActiveGroups, chatID) {
		return nil
	}
	if len(ctx.Args) == 0 {
		return ctx.Reply("⚠️ Escribe una acción o búsqueda. Ej: `.g kiss` o `.g spiderman`")
	}

	primero := strings.ToLower(ctx.Args[0])
	if primero == "help" {
		claves := make([]string, 0, len(gifAcciones))
		for k := range gifAcciones {
			claves = append(claves, k)
		}
		sort.Strings(claves)
		return ctx.Reply("📖 *Guía de interacciones*\n\nComandos:\n- *.g <acción> @user*\n- *.g <texto libre> @user*\n- *.g top* (Ver ranking del grupo)\n\n🎭 *Acciones:*\n_" + strings.Join(claves, ", ") + "_")
	}
	if primero == "top" {
		return gifSendTop(ctx)
	}

	preset, esPreset := gifAcciones[primero]
	searchForTenor := ""
	verbPhrase := ""
	participlePhrase := ""
	statsKey := ""
	if esPreset {
		searchForTenor = preset.Search
		verbPhrase = preset.Verb
		participlePhrase = preset.Participle
		statsKey = primero
	} else {
		var libres []string
		for _, w := range ctx.Args {
			if !strings.HasPrefix(w, "@") {
				libres = append(libres, w)
			}
		}
		rawSearch := strings.Join(libres, " ")
		searchForTenor = "anime " + rawSearch
		verbPhrase = `envió un GIF de "` + rawSearch + `" a`
		participlePhrase = `recibido un GIF de "` + rawSearch + `"`
		statsKey = "custom_gif"
	}

	targetID := senderID
	if ci := ctx.ContextInfo(); ci != nil {
		if menciones := ci.GetMentionedJID(); len(menciones) > 0 {
			targetID = menciones[0]
		} else if ci.GetParticipant() != "" {
			targetID = ci.GetParticipant()
		}
	}

	if !gifCheckSpam(chatID, senderID, statsKey, gifNow()) {
		return ctx.React("❌")
	}

	esPropio := senderID == targetID
	targetName := ""
	necesitaMencion := false
	if esPropio {
		targetName = senderName
	} else {
		stats := gifLoadStats()
		conocido := gifNombreConocido(stats, chatID, targetID)
		if conocido == "" {
			conocido = gifNombreStore(ctx, targetID)
		}
		if conocido != "" && conocido != "Alguien" && !strings.Contains(conocido, "@") && !esSoloDigitos(conocido) {
			targetName = conocido
		} else {
			targetName = "@" + gifExtractNumber(targetID)
			necesitaMencion = true
		}
	}

	gifURL := gifBuscarGif(searchForTenor)
	if gifURL == "" {
		return ctx.Reply("❌ No encontré nada con ese término.")
	}

	count := gifRegisterInteraction(chatID, senderID, senderName, targetID, targetName, statsKey)
	caption := ""
	if esPreset {
		if esPropio {
			caption = fmt.Sprintf("🎭 *%s* %s sí mismo.\n✨ Lo ha hecho %d veces.", senderName, verbPhrase, count)
		} else {
			caption = fmt.Sprintf("🎭 *%s* %s *%s*.\n✨ *%s* ha sido %s por ti %d veces.", senderName, verbPhrase, targetName, targetName, participlePhrase, count)
		}
	} else {
		if esPropio {
			caption = fmt.Sprintf("🔎 *%s* se %s sí mismo.", senderName, verbPhrase)
		} else {
			caption = fmt.Sprintf("🔎 *%s* %s *%s*.", senderName, verbPhrase, targetName)
		}
	}

	var menciones []types.JID
	if necesitaMencion {
		if jid, err := types.ParseJID(targetID); err == nil {
			menciones = append(menciones, jid)
		}
	}
	if err := gifSendFn(ctx, ctx.ChatJID(), gifURL, caption, menciones); err != nil {
		slog.Error("❌ [gif] Error enviando GIF:", "error", err)
	}
	return nil
}

func esSoloDigitos(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
