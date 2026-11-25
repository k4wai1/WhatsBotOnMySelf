// Comando principal del bot en Go.
//
// Replica el núcleo de index.js sobre go.mau.fi/whatsmeow:
//   - carga .env, config.json y el caché de contactos
//   - conecta WhatsApp y muestra el QR en la terminal
//   - enruta los comandos a los addons registrados
//   - reconecta ante caídas y apaga limpiamente con SIGINT/SIGTERM
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	// Driver SQLite sin CGO.
	_ "modernc.org/sqlite"

	"whatsbot-go/internal/addons"
	"whatsbot-go/internal/config"
	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("el bot terminó con error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	root := core.Root()
	logger.Info("raíz de datos detectada", "path", root)

	// .env con las credenciales/API keys (mismo archivo que el bot Node).
	if err := godotenv.Load(filepath.Join(root, ".env")); err != nil {
		logger.Warn("no se pudo cargar .env", "error", err)
	}

	// Vinculación por código numérico: --pair <numero> tiene prioridad sobre
	// PAIR_PHONE. Sin ninguno de los dos se conserva el flujo QR de siempre.
	pairPhone := pairPhoneFromArgs(os.Args[1:])
	if pairPhone == "" {
		pairPhone = sanitizePairPhone(os.Getenv("PAIR_PHONE"))
	}

	// Rutas retrocompatibles con el bot en Node.js.
	for _, dir := range []string{"assets", "cache", "files", "statistics"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			return fmt.Errorf("creando %s: %w", dir, err)
		}
	}

	cfgManager, err := config.NewManager(filepath.Join(root, "assets", "config.json"))
	if err != nil {
		logger.Warn("config.json inválido: se usan los prefijos por defecto", "error", err)
	}
	config.SetDefault(cfgManager)

	contacts := store.New(filepath.Join(root, "baileys_store_cache.json"))
	contacts.SetLogger(logger)
	if err := contacts.Load(); err != nil {
		logger.Warn("no se pudo leer el caché de contactos", "error", err)
	}
	logger.Info("caché de contactos cargado", "contactos", contacts.Len())

	// SQLite con modernc.org/sqlite (sin CGO) para la sesión de whatsmeow.
	sessionPath := filepath.Join(root, "session.db")
	dsn := "file:" + sessionPath + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	dbLog := waLog.Stdout("Database", "ERROR", true)
	container, err := sqlstore.New(context.Background(), "sqlite", dsn, dbLog)
	if err != nil {
		return fmt.Errorf("abriendo session.db: %w", err)
	}
	device, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return fmt.Errorf("leyendo dispositivo: %w", err)
	}

	client := whatsmeow.NewClient(device, waLog.Stdout("Client", "INFO", true))
	defer client.Disconnect()

	router := core.DefaultRouter()
	router.Prefixes = cfgManager.Prefixes
	router.Configure(client, contacts)
	router.SetLogger(logger)
	addons.RegisterAll(router)
	logger.Info("addons registrados", "comandos", router.Commands())

	if err := router.StartBackgrounds(client, contacts); err != nil {
		logger.Warn("algunos procesos en segundo plano fallaron", "error", err)
	}

	contacts.StartAutoSave(10 * time.Second)
	defer func() {
		if err := contacts.Close(); err != nil {
			logger.Warn("no se pudo guardar el caché al salir", "error", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var reconnecting sync.Mutex

	client.AddEventHandler(func(evt any) {
		switch v := evt.(type) {
		case *events.Message:
			router.ProcessMessage(v)

		case *events.Contact:
			jid := v.JID.String()
			// En whatsmeow actual, Action nil significa contacto eliminado.
			if v.Action == nil {
				contacts.Delete(jid)
				return
			}
			fields := map[string]any{"id": jid}
			if name := v.Action.GetFullName(); name != "" {
				fields["name"] = name
			}
			if firstName := v.Action.GetFirstName(); firstName != "" {
				fields["firstName"] = firstName
			}
			if username := v.Action.GetUsername(); username != "" {
				fields["username"] = username
			}
			if pn := v.Action.GetPnJID(); pn != "" {
				fields["pn"] = pn
			}
			if lid := v.Action.GetLidJID(); lid != "" {
				fields["lid"] = lid
			}
			contacts.Upsert(jid, fields)

		case *events.PushName:
			if v.NewPushName != "" {
				contacts.SetNotify(store.PurePN(v.JID.String()), v.NewPushName)
			}

		case *events.BusinessName:
			if v.NewBusinessName != "" {
				contacts.Upsert(v.JID.String(), map[string]any{
					"id":           v.JID.String(),
					"verifiedName": v.NewBusinessName,
				})
			}

		case *events.Connected:
			logger.Info("✅ Enlace establecido. Motor en línea.")

		case *events.Disconnected:
			logger.Warn("❌ Conexión cerrada. ¿Debe reconectar? true")
			reconnecting.Lock()
			defer reconnecting.Unlock()
			go reconnect(ctx, client, logger)

		case *events.LoggedOut:
			logger.Error("la sesión se cerró desde el teléfono (logged out); hay que escanear el QR de nuevo")
			stop()
		}
	})

	logger.Info("iniciando conexión con WhatsApp…")
	if client.Store.ID == nil {
		if pairPhone != "" {
			logger.Info("vinculando por Pair Code", "numero", maskPairPhone(pairPhone))
			if err := client.Connect(); err != nil {
				return fmt.Errorf("conectando: %w", err)
			}
			if !waitConnected(ctx, client, 20*time.Second) {
				return errors.New("timeout esperando la conexión inicial para solicitar el Pair Code")
			}
			code, err := client.PairPhone(ctx, pairPhone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
			if err != nil {
				return fmt.Errorf("solicitando Pair Code: %w", err)
			}
			printPairCode(code)
		} else {
			qrChan, err := client.GetQRChannel(ctx)
			if err != nil {
				return fmt.Errorf("creando canal QR: %w", err)
			}
			if err := client.Connect(); err != nil {
				return fmt.Errorf("conectando: %w", err)
			}
			if err := waitForQR(ctx, qrChan, logger); err != nil {
				return err
			}
		}
	} else {
		if pairPhone != "" {
			logger.Warn("ya existe una sesión iniciada: se ignora el Pair Code", "numero", maskPairPhone(pairPhone))
		}
		if err := client.Connect(); err != nil {
			return fmt.Errorf("conectando: %w", err)
		}
	}

	<-ctx.Done()
	logger.Info("Apagando bot…")
	return nil
}

// pairPhoneFromArgs busca --pair <numero> o --pair=<numero> en los argumentos.
// Devuelve el número solo con dígitos (sin "+", espacios ni guiones).
func pairPhoneFromArgs(args []string) string {
	for i, arg := range args {
		switch {
		case arg == "--pair" || arg == "-pair":
			if i+1 < len(args) {
				return sanitizePairPhone(args[i+1])
			}
		case strings.HasPrefix(arg, "--pair="):
			return sanitizePairPhone(strings.TrimPrefix(arg, "--pair="))
		case strings.HasPrefix(arg, "-pair="):
			return sanitizePairPhone(strings.TrimPrefix(arg, "-pair="))
		}
	}
	return ""
}

// sanitizePairPhone deja solo los dígitos del número (código de país incluido).
func sanitizePairPhone(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// maskPairPhone oculta todo menos los últimos 4 dígitos en los logs.
func maskPairPhone(phone string) string {
	if len(phone) <= 4 {
		return "****"
	}
	return strings.Repeat("*", len(phone)-4) + phone[len(phone)-4:]
}

// waitConnected espera a que el websocket quede conectado o expire el plazo.
func waitConnected(ctx context.Context, client *whatsmeow.Client, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for !client.IsConnected() {
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(200 * time.Millisecond):
		}
	}
	return true
}

// printPairCode muestra el código de 8 caracteres de forma bien visible.
func printPairCode(code string) {
	fmt.Println("\n🔢 VINCULACIÓN POR CÓDIGO")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("En WhatsApp: Dispositivos vinculados → Vincular con número de teléfono")
	fmt.Println("e introduce este código:")
	fmt.Println()
	fmt.Printf("        %s\n", code)
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

// waitForQR imprime el QR en la terminal (equivalente a qrcode-terminal).
func waitForQR(ctx context.Context, qrChan <-chan whatsmeow.QRChannelItem, logger *slog.Logger) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case evt, ok := <-qrChan:
			if !ok {
				return nil
			}
			switch evt.Event {
			case "code":
				fmt.Println("\n📱 ESCANEA ESTE CÓDIGO QR PARA INICIAR SESIÓN:")
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
			case "success":
				logger.Info("QR aceptado, sesión iniciada")
			case "timeout":
				return errors.New("timeout esperando el escaneo del QR")
			case "error":
				return fmt.Errorf("error del canal QR: %w", evt.Error)
			default:
				logger.Debug("evento QR", "evento", evt.Event)
			}
		}
	}
}

// reconnect reintenta con backoff exponencial hasta reconectar o apagarse.
func reconnect(ctx context.Context, client *whatsmeow.Client, logger *slog.Logger) {
	backoff := 2 * time.Second
	for attempt := 1; ; attempt++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if client.IsConnected() || client.Store.ID == nil {
			return
		}
		logger.Info("reconectando…", "intento", attempt)
		if err := client.Connect(); err == nil {
			return
		} else {
			logger.Warn("fallo al reconectar", "intento", attempt, "error", err)
		}
		if backoff < time.Minute {
			backoff *= 2
		}
	}
}
