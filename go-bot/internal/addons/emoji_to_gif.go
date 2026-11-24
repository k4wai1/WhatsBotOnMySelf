package addons

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"

	"whatsbot-go/internal/core"
	"whatsbot-go/internal/store"
)

func init() {
	core.Register(EmojiToGif{})
	core.RegisterBackground("emoji-to-gif", emojiInit)
}

var emojiHeartEmojis = []string{"❤️", "💖", "💕", "💓", "💗", "💘", "💝"}

// Inyectables para pruebas.
var (
	emojiConfigPath = func() string { return core.DataPath("statistics", "reacciones_config.json") }
	emojiStatsPath  = func() string { return core.DataPath("statistics", "reacciones_stats.json") }
	emojiHTTPClient = &http.Client{Timeout: 8 * time.Second}
	emojiRandIntN   = rand.IntN
	emojiTenorURL   = "https://tenor.googleapis.com/v2/search"
)

type emojiConfig struct {
	ActiveGroups []string `json:"active_groups"`
}

type emojiStats map[string]map[string]map[string]int

// EmojiToGif porta addons/emoji-to-gif.js: .onreaccion / .offreaccion.
type EmojiToGif struct{}

// Commands cumple core.Addon.
func (EmojiToGif) Commands() []string { return []string{"onreaccion", "offreaccion"} }

// Handle replica la gestión de grupos activos.
func (EmojiToGif) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	jid := ctx.ChatJID().String()

	cfg, err := emojiLoadConfig()
	if err != nil {
		slog.Warn("emoji-to-gif: no se pudo leer la configuración", "error", err)
		cfg = &emojiConfig{}
	}

	switch ctx.Command {
	case "onreaccion":
		if !emojiContains(cfg.ActiveGroups, jid) {
			cfg.ActiveGroups = append(cfg.ActiveGroups, jid)
			if err := emojiSaveConfig(cfg); err != nil {
				return err
			}
			return ctx.React("✅")
		}
		return ctx.React("👍")
	case "offreaccion":
		idx := emojiIndex(cfg.ActiveGroups, jid)
		if idx > -1 {
			cfg.ActiveGroups = append(cfg.ActiveGroups[:idx], cfg.ActiveGroups[idx+1:]...)
			if err := emojiSaveConfig(cfg); err != nil {
				return err
			}
			return ctx.React("❌")
		}
		return ctx.React("👍")
	}
	return nil
}

func emojiInit(client *whatsmeow.Client, st *store.Store) error {
	if err := emojiEnsureFiles(); err != nil {
		slog.Warn("emoji-to-gif: no se pudieron inicializar los archivos", "error", err)
	}
	if client == nil {
		return nil
	}
	client.AddEventHandler(func(evt any) {
		msg, ok := evt.(*events.Message)
		if !ok || msg == nil || msg.Message == nil {
			return
		}
		reaction := msg.Message.GetReactionMessage()
		if reaction == nil {
			return
		}
		emojiHandleReaction(client, st, msg, reaction)
	})
	return nil
}

// emojiHandleReaction replica el listener de mensajes.upsert (solo reacciones).
func emojiHandleReaction(client *whatsmeow.Client, st *store.Store, msg *events.Message, reaction *waE2E.ReactionMessage) {
	emoji := reaction.GetText()
	if emoji == "" {
		return
	}
	jid := msg.Info.Chat.String()
	senderID := msg.Info.Sender.String()
	if senderID == "" {
		senderID = jid
	}
	targetID := reaction.GetKey().GetParticipant()
	if targetID == "" {
		targetID = jid
	}

	cfg, err := emojiLoadConfig()
	if err != nil {
		cfg = &emojiConfig{}
	}
	isAuthorizedGroup := emojiContains(cfg.ActiveGroups, jid)
	isBotOwner := msg.Info.IsFromMe
	if !isAuthorizedGroup && !isBotOwner {
		return
	}
	if !emojiIsHeart(emoji) {
		return
	}
	if senderID == targetID {
		return
	}

	stats, err := emojiLoadStats()
	if err != nil {
		stats = emojiStats{}
	}
	if stats[jid] == nil {
		stats[jid] = map[string]map[string]int{}
	}
	if stats[jid][senderID] == nil {
		stats[jid][senderID] = map[string]int{}
	}
	stats[jid][senderID][targetID]++
	totalHearts := stats[jid][senderID][targetID]
	if err := emojiSaveStats(stats); err != nil {
		slog.Warn("emoji-to-gif: no se pudieron guardar las estadísticas", "error", err)
	}

	senderName := msg.Info.PushName
	if senderName == "" {
		senderName = "Alguien"
	}
	targetName := "alguien"
	if st != nil {
		if name := st.Name(targetID); name != "" {
			targetName = name
		}
	}
	if targetName == "alguien" && reaction.GetKey().GetFromMe() {
		targetName = "ti"
	}

	gifURL, err := emojiRoleplayGif()
	if err != nil || gifURL == "" {
		if err != nil {
			slog.Warn("emoji-to-gif: Tenor falló", "error", err)
		}
		return
	}
	data, err := emojiDownload(gifURL)
	if err != nil || len(data) == 0 {
		return
	}
	caption := fmt.Sprintf("💖 *%s* se sintió lleno de afecto y le mandó un corazón a *%s*.\n\n✨ (Le ha enviado amor %d veces)", senderName, targetName, totalHearts)
	ctx := &core.Context{Client: client, Message: msg}
	if err := ctx.SendVideo(data, caption, true); err != nil {
		slog.Warn("emoji-to-gif: no se pudo enviar el video", "error", err)
	}
}

func emojiRoleplayGif() (string, error) {
	key := os.Getenv("TENOR_API_KEY")
	u := fmt.Sprintf("%s?q=%s&key=%s&limit=20&contentfilter=low",
		emojiTenorURL, url.QueryEscape("genshin wuthering anime cute hug love"), url.QueryEscape(key))
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := emojiHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Tenor HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	var parsed struct {
		Results []struct {
			MediaFormats map[string]struct {
				URL string `json:"url"`
			} `json:"media_formats"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Results) == 0 {
		return "", nil
	}
	selected := parsed.Results[emojiRandIntN(len(parsed.Results))]
	for _, name := range []string{"tinymp4", "mp4", "gif"} {
		if f, ok := selected.MediaFormats[name]; ok && f.URL != "" {
			return f.URL, nil
		}
	}
	return "", nil
}

func emojiDownload(rawurl string) ([]byte, error) {
	resp, err := emojiHTTPClient.Get(rawurl)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

func emojiLoadConfig() (*emojiConfig, error) {
	cfg := &emojiConfig{ActiveGroups: []string{}}
	raw, err := os.ReadFile(emojiConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return cfg, err
	}
	if cfg.ActiveGroups == nil {
		cfg.ActiveGroups = []string{}
	}
	return cfg, nil
}

func emojiSaveConfig(cfg *emojiConfig) error {
	if cfg.ActiveGroups == nil {
		cfg.ActiveGroups = []string{}
	}
	return core.WriteJSON(emojiConfigPath(), cfg)
}

func emojiLoadStats() (emojiStats, error) {
	stats := emojiStats{}
	raw, err := os.ReadFile(emojiStatsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return stats, nil
		}
		return stats, err
	}
	if len(raw) == 0 {
		return stats, nil
	}
	if err := json.Unmarshal(raw, &stats); err != nil {
		return emojiStats{}, err
	}
	if stats == nil {
		stats = emojiStats{}
	}
	return stats, nil
}

func emojiSaveStats(stats emojiStats) error {
	return core.WriteJSON(emojiStatsPath(), stats)
}

// emojiEnsureFiles crea statistics/reacciones_config.json y _stats.json si faltan.
func emojiEnsureFiles() error {
	if err := os.MkdirAll(filepath.Dir(emojiConfigPath()), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(emojiConfigPath()); os.IsNotExist(err) {
		if err := core.WriteJSON(emojiConfigPath(), &emojiConfig{ActiveGroups: []string{}}); err != nil {
			return err
		}
	}
	if _, err := os.Stat(emojiStatsPath()); os.IsNotExist(err) {
		if err := core.WriteJSON(emojiStatsPath(), emojiStats{}); err != nil {
			return err
		}
	}
	return nil
}

func emojiContains(list []string, v string) bool { return emojiIndex(list, v) > -1 }

func emojiIndex(list []string, v string) int {
	for i, item := range list {
		if item == v {
			return i
		}
	}
	return -1
}

func emojiIsHeart(emoji string) bool {
	for _, h := range emojiHeartEmojis {
		if h == emoji {
			return true
		}
	}
	return false
}
