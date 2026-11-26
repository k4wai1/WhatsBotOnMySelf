package addons

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"whatsbot-go/internal/core"
)

// TestRegistroSinComandosDuplicados verifica que los 66 addons portados no
// colisionen entre sí. Se usa un router nuevo y un logger que captura los
// avisos de "comando duplicado" de core.Router.
func TestRegistroSinComandosDuplicados(t *testing.T) {
	var avisos bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&avisos, &slog.HandlerOptions{Level: slog.LevelWarn}))

	router := core.NewRouter(nil)
	router.SetLogger(logger)

	addons := []core.Addon{
		Acortar{},
		Add{},
		Alarm{},
		AudioEditor{},
		Bal{},
		Ban{},
		Blue{},
		Cat{},
		Cedula{},
		Channel{},
		Chat{},
		Cita{},
		Cordial{},
		Cortisol{},
		Dado{},
		DebugInfo{},
		DebugPp{},
		&debugStAddon{},
		Definir{},
		EmojiToGif{},
		Envfoto{},
		Felix{},
		Fetch{},
		Files{},
		Ft{},
		Furry{},
		Fx{},
		Gato{},
		Gif{},
		Github{},
		Help{},
		Id{},
		Img{},
		List{},
		Love{},
		Misc{},
		Mute{},
		Ocr{},
		Pantera{},
		Papelera{},
		Paste{},
		Perfil{},
		Pin{},
		Prefix{},
		Qr{},
		Reaction{},
		Rem{},
		Resume{},
		Rm{},
		&saAddon{},
		Scan{},
		Server{},
		Speed{},
		Ss{},
		Stickers{},
		Sushi{},
		Sysinfo{},
		Tasas{},
		Top{},
		Trans{},
		Ttt{},
		Twitter{},
		Unread{},
		Unzip{},
		Voz{},
		Wallpaper{},
		Ytdl{},
	}

	for _, addon := range addons {
		router.Register(addon)
	}

	if strings.Contains(avisos.String(), "comando duplicado") {
		t.Fatalf("se detectaron comandos duplicados:\n%s", avisos.String())
	}

	comandos := router.Commands()
	if len(comandos) < 60 {
		t.Fatalf("solo se registraron %d comandos: %v", len(comandos), comandos)
	}

	vistos := map[string]bool{}
	for _, comando := range comandos {
		if comando == "" {
			continue
		}
		if vistos[comando] {
			t.Fatalf("comando duplicado: %q", comando)
		}
		vistos[comando] = true
	}
}

// TestAddonsComandosUnicos valida que cada addon no repita comandos dentro de
// su propia lista (p. ej. por un alias copiado dos veces).
func TestAddonsComandosUnicos(t *testing.T) {
	addons := []struct {
		nombre string
		addon  core.Addon
	}{
		{"Acortar", Acortar{}},
		{"Add", Add{}},
		{"Alarm", Alarm{}},
		{"AudioEditor", AudioEditor{}},
		{"Bal", Bal{}},
		{"Ban", Ban{}},
		{"Blue", Blue{}},
		{"Cat", Cat{}},
		{"Cedula", Cedula{}},
		{"Channel", Channel{}},
		{"Chat", Chat{}},
		{"Cita", Cita{}},
		{"Cordial", Cordial{}},
		{"Cortisol", Cortisol{}},
		{"Dado", Dado{}},
		{"DebugInfo", DebugInfo{}},
		{"DebugPp", DebugPp{}},
		{"DebugSt", &debugStAddon{}},
		{"Definir", Definir{}},
		{"EmojiToGif", EmojiToGif{}},
		{"Envfoto", Envfoto{}},
		{"Felix", Felix{}},
		{"Fetch", Fetch{}},
		{"Files", Files{}},
		{"Ft", Ft{}},
		{"Furry", Furry{}},
		{"Fx", Fx{}},
		{"Gato", Gato{}},
		{"Gif", Gif{}},
		{"Github", Github{}},
		{"Help", Help{}},
		{"Id", Id{}},
		{"Img", Img{}},
		{"List", List{}},
		{"Love", Love{}},
		{"Misc", Misc{}},
		{"Mute", Mute{}},
		{"Ocr", Ocr{}},
		{"Pantera", Pantera{}},
		{"Papelera", Papelera{}},
		{"Paste", Paste{}},
		{"Perfil", Perfil{}},
		{"Pin", Pin{}},
		{"Prefix", Prefix{}},
		{"Qr", Qr{}},
		{"Reaction", Reaction{}},
		{"Rem", Rem{}},
		{"Resume", Resume{}},
		{"Rm", Rm{}},
		{"Sa", &saAddon{}},
		{"Scan", Scan{}},
		{"Server", Server{}},
		{"Speed", Speed{}},
		{"Ss", Ss{}},
		{"Stickers", Stickers{}},
		{"Sushi", Sushi{}},
		{"Sysinfo", Sysinfo{}},
		{"Tasas", Tasas{}},
		{"Top", Top{}},
		{"Trans", Trans{}},
		{"Ttt", Ttt{}},
		{"Twitter", Twitter{}},
		{"Unread", Unread{}},
		{"Unzip", Unzip{}},
		{"Voz", Voz{}},
		{"Wallpaper", Wallpaper{}},
		{"Ytdl", Ytdl{}},
	}

	for _, item := range addons {
		vistos := map[string]bool{}
		for _, comando := range item.addon.Commands() {
			if comando == "" {
				continue
			}
			if vistos[comando] {
				t.Fatalf("%s repite el comando %q", item.nombre, comando)
			}
			vistos[comando] = true
		}
	}
}

// TestRegistroGlobalCompleto comprueba que cada addon del paquete haya
// ejecutado su init() (o RegisterAll) y esté disponible en el router global.
func TestRegistroGlobalCompleto(t *testing.T) {
	registro := []struct {
		nombre   string
		comandos []string
	}{
		{"Acortar", []string{"acortar", "short"}},
		{"Add", []string{"add"}},
		{"Alarm", []string{"a", "alarma"}},
		{"AudioEditor", []string{"au"}},
		{"Bal", []string{"bal"}},
		{"Ban", []string{"ban"}},
		{"Blue", []string{"blue"}},
		{"Cat", []string{"cat"}},
		{"Cedula", []string{"cedula", "ci"}},
		{"Channel", []string{"graf"}},
		{"Chat", []string{"chat", "c"}},
		{"Cita", []string{"cita"}},
		{"Cordial", []string{"cordial", "cord"}},
		{"Cortisol", []string{"cor"}},
		{"Dado", []string{"dado", "moneda"}},
		{"DebugInfo", []string{"info", "debug"}},
		{"DebugPp", []string{"getpp", "debugpp"}},
		{"DebugSt", []string{"debug-st", "sdebug"}},
		{"Definir", []string{"definir", "def"}},
		{"EmojiToGif", []string{"onreaccion", "offreaccion"}},
		{"Envfoto", []string{"envfoto"}},
		{"Felix", []string{"felix"}},
		{"Fetch", []string{"fetch"}},
		{"Files", []string{"files", "f"}},
		{"Ft", []string{"ft"}},
		{"Furry", []string{"furry"}},
		{"Fx", []string{"fx", "efecto"}},
		{"Gato", []string{"gato"}},
		{"Gif", []string{"g", "ongif", "offgif", "topg"}},
		{"Github", []string{"git", "github", "repo"}},
		{"Help", []string{"help", "ayuda"}},
		{"Id", []string{"id"}},
		{"Img", []string{"img", "vision"}},
		{"List", []string{"list"}},
		{"Love", []string{"love"}},
		{"Misc", []string{"ping", "pong"}},
		{"Mute", []string{"mute", "unmute"}},
		{"Ocr", []string{"ocr"}},
		{"Pantera", []string{"pantera"}},
		{"Paste", []string{"paste"}},
		{"Perfil", []string{"pf", "perfil"}},
		{"Pin", []string{"pin", "unpin"}},
		{"Prefix", []string{"prefix", "setprefix"}},
		{"Qr", []string{"qr"}},
		{"Reaction", []string{"reaction"}},
		{"Rem", []string{"rem", "recordar"}},
		{"Resume", []string{"resu"}},
		{"Rm", []string{"rm"}},
		{"Sa", []string{"sa"}},
		{"Scan", []string{"scan"}},
		{"Server", []string{"server"}},
		{"Speed", []string{"speed"}},
		{"Ss", []string{"ss", "screenshot"}},
		{"Stickers", []string{"st", "sticker", "s"}},
		{"Sushi", []string{"sushi"}},
		{"Sysinfo", []string{"sysinfo", "sistema"}},
		{"Tasas", []string{"dolar", "dólar", "euro", "yuan", "lira", "rublo", "tasas"}},
		{"Top", []string{"ongrup", "top"}},
		{"Trans", []string{"tr", "tri"}},
		{"Ttt", []string{"ttt"}},
		{"Twitter", []string{"x", "tw", "twitter"}},
		{"Unread", []string{"unread"}},
		{"Unzip", []string{"unzip"}},
		{"Voz", []string{"voz", "tts"}},
		{"Wallpaper", []string{"wallpaper", "wp"}},
		{"Ytdl", []string{"ytdl", "ytdlp", "yt", "youtube"}},
	}
	router := core.DefaultRouter()
	RegisterAll(router)
	for _, item := range registro {
		for _, comando := range item.comandos {
			if _, ok := router.Lookup(comando); !ok {
				t.Fatalf("%s no registró el comando %q", item.nombre, comando)
			}
		}
	}
}
