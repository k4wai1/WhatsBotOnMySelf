package core

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"

	"whatsbot-go/internal/config"
	"whatsbot-go/internal/store"
)

// ErrUnknownCommand se devuelve cuando no hay addon para el comando.
var ErrUnknownCommand = errors.New("comando desconocido")

// ParsedCommand es el resultado de interpretar un texto entrante.
type ParsedCommand struct {
	Prefix  string
	Command string
	Args    []string
	Restart bool
}

type backgroundEntry struct {
	name string
	fn   BackgroundFunc
}

// Router mantiene el registro de addons y enruta mensajes.
type Router struct {
	mu        sync.RWMutex
	addons    map[string]Addon
	started   map[Addon]bool
	bg        []backgroundEntry
	bgStarted bool

	// Prefixes se consulta en cada mensaje (para que .prefix funcione).
	Prefixes func() []string

	Client *whatsmeow.Client
	Store  *store.Store

	// Exit permite sustituir os.Exit en tests.
	Exit   func(code int)
	Logger *slog.Logger
}

// NewRouter crea un router. Si prefixes es nil se usan los defaults.
func NewRouter(prefixes func() []string) *Router {
	if prefixes == nil {
		prefixes = func() []string { return config.DefaultPrefixes }
	}
	return &Router{
		addons:   make(map[string]Addon),
		started:  make(map[Addon]bool),
		Prefixes: prefixes,
		Exit:     os.Exit,
		Logger:   slog.Default(),
	}
}

// Configure inyecta el cliente y el store que recibirán los Context.
func (r *Router) Configure(client *whatsmeow.Client, st *store.Store) {
	r.Client = client
	r.Store = st
}

// SetLogger cambia el logger del router.
func (r *Router) SetLogger(l *slog.Logger) {
	if l != nil {
		r.Logger = l
	}
}

// Register añade un addon y, si implementa BackgroundAddon, su Init.
func (r *Router) Register(a Addon) {
	if a == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, cmd := range a.Commands() {
		name := NormalizeCommand(cmd)
		if name == "" {
			continue
		}
		if _, exists := r.addons[name]; exists {
			r.Logger.Warn("comando duplicado: se queda el último registrado", "comando", name)
		}
		r.addons[name] = a
	}

	if bg, ok := a.(BackgroundAddon); ok {
		if !r.started[a] {
			r.started[a] = true
			r.bg = append(r.bg, backgroundEntry{name: fmt.Sprintf("%v", a.Commands()), fn: bg.Init})
		}
	}
}

// RegisterBackground registra un proceso silencioso por función.
func (r *Router) RegisterBackground(name string, fn BackgroundFunc) {
	if fn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bg = append(r.bg, backgroundEntry{name: name, fn: fn})
}

// StartBackgrounds ejecuta los Init registrados (equivale al cargador de
// init(sock, store) de index.js).
func (r *Router) StartBackgrounds(client *whatsmeow.Client, st *store.Store) error {
	r.mu.Lock()
	if r.bgStarted {
		r.mu.Unlock()
		return nil
	}
	r.bgStarted = true
	entries := append([]backgroundEntry(nil), r.bg...)
	r.mu.Unlock()

	for _, entry := range entries {
		if err := entry.fn(client, st); err != nil {
			r.Logger.Error("fallo al iniciar proceso en segundo plano", "proceso", entry.name, "error", err)
			continue
		}
		r.Logger.Info("proceso en segundo plano iniciado", "proceso", entry.name)
	}
	return nil
}

// Commands devuelve la lista ordenada de comandos registrados.
func (r *Router) Commands() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.addons))
	for cmd := range r.addons {
		out = append(out, cmd)
	}
	return out
}

// Lookup busca el addon de un comando.
func (r *Router) Lookup(command string) (Addon, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.addons[NormalizeCommand(command)]
	return a, ok
}

// Dispatch ejecuta el addon correspondiente al comando del Context.
func (r *Router) Dispatch(ctx *Context) error {
	if ctx == nil {
		return nil
	}
	addon, ok := r.Lookup(ctx.Command)
	if !ok {
		return ErrUnknownCommand
	}
	return addon.Handle(ctx)
}

// ExtractText replica la extracción de texto de index.js: conversación,
// texto extendido y captions de imagen/video/documento.
func ExtractText(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	if txt := msg.GetConversation(); txt != "" {
		return txt
	}
	if ext := msg.GetExtendedTextMessage(); ext != nil && ext.GetText() != "" {
		return ext.GetText()
	}
	if img := msg.GetImageMessage(); img != nil && img.GetCaption() != "" {
		return img.GetCaption()
	}
	if vid := msg.GetVideoMessage(); vid != nil && vid.GetCaption() != "" {
		return vid.GetCaption()
	}
	if doc := msg.GetDocumentMessage(); doc != nil && doc.GetCaption() != "" {
		return doc.GetCaption()
	}
	return ""
}

// ParseCommand interpreta el texto con las reglas del bot original:
//   - <prefix>restart (con cualquier prefijo) marca reinicio.
//   - el prefijo se busca con HasPrefix sobre el texto SIN recortar.
//   - los argumentos se separan por espacios tras recortar el resto.
func ParseCommand(text string, prefixes []string) (ParsedCommand, bool) {
	var out ParsedCommand
	if text == "" {
		return out, false
	}

	trimmed := strings.TrimSpace(text)
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		if trimmed == p+"restart" {
			out.Prefix = p
			out.Restart = true
			return out, true
		}
	}

	usedPrefix := ""
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(text, p) {
			usedPrefix = p
			break
		}
	}
	if usedPrefix == "" {
		return out, false
	}

	rest := strings.TrimSpace(text[len(usedPrefix):])
	if rest == "" {
		return out, false
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return out, false
	}

	out.Prefix = usedPrefix
	out.Command = NormalizeCommand(fields[0])
	out.Args = fields[1:]
	return out, true
}

// ProcessMessage es el interceptor global de messages.upsert: alimenta el
// store con el pushName, detecta restart y enruta el comando.
func (r *Router) ProcessMessage(msg *events.Message) {
	if msg == nil || msg.Message == nil {
		return
	}

	text := ExtractText(msg.Message)
	if text == "" {
		return
	}

	r.feedPushName(msg)

	parsed, ok := ParseCommand(text, r.prefixList())
	if !ok {
		return
	}

	ctx := &Context{
		Client:  r.Client,
		Message: msg,
		Store:   r.Store,
		Args:    parsed.Args,
		Command: parsed.Command,
		Prefix:  parsed.Prefix,
		RawText: text,
	}

	if parsed.Restart {
		if err := ctx.React("🔄"); err != nil {
			r.Logger.Warn("no se pudo reaccionar al restart", "error", err)
		}
		r.Logger.Info("🔄 Señal de reinicio recibida.")
		r.exit(1)
		return
	}

	if err := r.Dispatch(ctx); err != nil {
		if errors.Is(err, ErrUnknownCommand) {
			return
		}
		r.Logger.Error("error en addon", "comando", parsed.Command, "error", err)
		if reactErr := ctx.React("❌"); reactErr != nil {
			r.Logger.Warn("no se pudo reaccionar con ❌", "error", reactErr)
		}
	}
}

// feedPushName replica el auto-alimentado del almacén con msg.pushName.
func (r *Router) feedPushName(msg *events.Message) {
	if r.Store == nil || msg.Info.PushName == "" {
		return
	}
	sender := msg.Info.Sender.String()
	if sender == "" {
		sender = msg.Info.Chat.String()
	}
	pure := store.PurePN(sender)
	if pure == "" {
		return
	}
	r.Store.Upsert(pure, map[string]any{"id": pure, "notify": msg.Info.PushName})
}

func (r *Router) prefixList() []string {
	if r.Prefixes == nil {
		return config.DefaultPrefixes
	}
	return r.Prefixes()
}

func (r *Router) exit(code int) {
	if r.Exit != nil {
		r.Exit(code)
		return
	}
	os.Exit(code)
}
