package addons

import "whatsbot-go/internal/core"

// RegisterAll registra todos los addons portados en el router dado.
// Se llama desde cmd/bot/main.go tras crear el router.
func RegisterAll(r *core.Router) {
	r.Register(Dado{})
}
