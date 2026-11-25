// gato_sprites.go porta addons/gato/sprites.js: SOLO ARTE ASCII.
package addons

const (
	gatoCocinaReposo = " ╱|_\n" +
		"(•  •7\n" +
		" 、 ˜〵\n" +
		" じしˍ,)ノ"

	gatoCocinaParpadeo = " ╱|_\n" +
		"(-  -7\n" +
		" 、 ˜〵\n" +
		" じしˍ,)ノ"

	gatoSalaReposo = "     へ   ♡\n" +
		" ૮ - ‸՛)   \n" +
		"  / ⁻ ៸|   \n" +
		"乀 (ˍ,لل   "

	gatoSalaParpadeo = "     へ   ♡\n" +
		" ૮  > <)   \n" +
		"  / ⁻ ៸|   \n" +
		"乀 (ˍ,لل   "

	gatoSalaFeliz = "   へ  ♡♡\n" +
		" ૮ ^ ^)   \n" +
		"  / ⁻ ៸|   \n" +
		"乀 (ˍ,لل   "

	gatoSalaTriste = "     へ    \n" +
		" ૮  ‸ ｡)~ \n" +
		"  / ⁻ ៸|   \n" +
		"乀 (ˍ,لل   "

	gatoSalaHambriento = "     へ  〜\n" +
		" ૮ ▽  )~  \n" +
		"  / ⁻ ៸|   \n" +
		"乀 (ˍ,لل   "

	gatoSalaEnfadado = "     へ  ‼\n" +
		" ૮ >Д<)つ \n" +
		"  / ⁻ ៸|   \n" +
		"乀 (ˍ,لل   "
)

var gatoComidas = []string{"🍤", "🍗", "🍣", "🥩", "🍉", "🍋"}

func gatoCocinaComer1(food string) string {
	return " ╱|_\n" +
		"(•  •7\n" +
		" 、 ˜〵\n" +
		" " + food + "しˍ,)ノ"
}

func gatoCocinaComer2(food string) string {
	return " ╱|_\n" +
		"(•  •7\n" +
		" " + food + "૮〵\n" +
		"  | ˍ,)ノ"
}

func gatoCocinaMasticar(m string) string {
	return " ╱|__\n" +
		"(• " + m + " •7\n" +
		" ꪒ  ꪒ〵\n" +
		" じしˍ,)ノ"
}

func gatoCocinaLimon(m, t string) string {
	return " ╱| _\n" +
		"( >" + m + "< 7\n" +
		" 、 ˜〵\n" +
		" じしˍ,)" + t
}

func gatoSalaCaricia(i int) string {
	corazon := "  ♡"
	if i%2 != 0 {
		corazon = "♡"
	}
	return corazon + "  へ  \n" +
		" ૮ ᵕ  )   \n" +
		"  / ⁻ ៸|   \n" +
		"乀 (ˍ,لل   "
}

func gatoDormirReposo(i int) string {
	z1 := []string{"z", "Z", "ᶻ"}[i%3]
	z2 := []string{"Z", "z", "z"}[i%3]
	z3 := []string{"ᶻ", "Z", "z"}[i%3]
	return "      へ " + z1 + " \n" +
		" ૮  -  ) " + z2 + "\n" +
		"  / ⁻ ៸| " + z3 + "\n" +
		"乀 (ˍ,لل      "
}

func gatoBanioBurbujas(i int) string {
	b := [3]string{}
	switch i % 4 {
	case 0:
		b = [3]string{"○", "°", "o"}
	case 1:
		b = [3]string{"°", "*", "○"}
	case 2:
		b = [3]string{"o", "○", "*"}
	default:
		b = [3]string{"*", "o", "°"}
	}
	return "  " + b[0] + " へ " + b[1] + "\n" +
		" ( ˘ ˰ )  \n" +
		" ¤ ⁻ ៸| " + b[2] + " \n" +
		"乀(ˍ,لل     "
}

// gatoSalaArte devuelve la carta de SALA para un ánimo ("" si no existe).
func gatoSalaArte(animo string) (string, bool) {
	switch animo {
	case "feliz":
		return gatoSalaFeliz, true
	case "triste":
		return gatoSalaTriste, true
	case "hambriento":
		return gatoSalaHambriento, true
	case "enfadado":
		return gatoSalaEnfadado, true
	}
	return "", false
}
