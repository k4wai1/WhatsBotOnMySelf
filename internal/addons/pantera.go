package addons

import (
	"strings"

	"whatsbot-go/internal/core"
)

// Pantera porta addons/pantera.js: mensajes glitch (Zalgo) o censurados.
type Pantera struct{}

func init() { core.Register(Pantera{}) }

func (Pantera) Commands() []string { return []string{"pantera"} }

var panteraBromas = []string{
	"Hackeando la base de datos central... Por favor espere.",
	"Descargando más memoria RAM en tu dispositivo...",
	"El servidor Debian del bot ha tomado conciencia propia. Huye.",
	"Iniciando secuencia de autodestrucción en 3... 2... 1...",
	"Tu historial de búsqueda ha sido comprometido.",
	"Formateando disco duro remoto... 99% completado.",
	"Detectando anomalía en la matrix... Reiniciando simulación.",
	"Despertando a los antiguos dioses del código fuente...",
	"si les esto eres gay",
	"dios nos abandono, por eso jesus se fue de este mundo",
	"linux lo mas duro del sistema",
	"estos mesajes no los devra ver nadie, que haces aqui?, no es un lugar para alguien de ese mundo",
}

var panteraGlitch = []rune{
	'\u030d', '\u030e', '\u0304', '\u0305', '\u033f', '\u0311', '\u0306', '\u0310',
	'\u0352', '\u0357', '\u0351', '\u0301', '\u0340', '\u0300', '\u0341', '\u030f',
	'\u0312', '\u0313', '\u0314', '\u030c', '\u030a', '\u0328', '\u0316', '\u0317',
	'\u0318', '\u0319', '\u031c', '\u031d', '\u0336', '\u0334', '\u0335', '\u0337', '\u0338',
}

var panteraLongitudes = []int{3, 4, 2, 5, 7, 3, 2, 6, 4}

// panteraZalgo replica generarZalgo(): 4-13 marcas por carácter no espacio.
func panteraZalgo(texto string) string {
	var b strings.Builder
	for _, r := range texto {
		b.WriteRune(r)
		if r == ' ' {
			continue
		}
		nivel := randIntN(10) + 4
		for i := 0; i < nivel; i++ {
			b.WriteRune(panteraGlitch[randIntN(len(panteraGlitch))])
		}
	}
	return b.String()
}

// panteraCensurado replica generarCensurado().
func panteraCensurado() string {
	cantidad := randIntN(8) + 4
	partes := make([]string, 0, cantidad)
	for i := 0; i < cantidad; i++ {
		largo := panteraLongitudes[randIntN(len(panteraLongitudes))]
		partes = append(partes, strings.Repeat("█", largo))
	}
	return strings.Join(partes, " ")
}

// panteraRespuesta arma la respuesta según el estilo aleatorio.
func panteraRespuesta(estilo int) string {
	if estilo == 0 {
		return panteraCensurado() + " " + panteraCensurado()
	}
	frase := panteraBromas[randIntN(len(panteraBromas))]
	if estilo == 1 {
		return panteraZalgo(frase)
	}
	return panteraZalgo("ERROR 404: ") + panteraCensurado() + "\n" + panteraZalgo(frase)
}

func (Pantera) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Reply(panteraRespuesta(randIntN(3)))
}
