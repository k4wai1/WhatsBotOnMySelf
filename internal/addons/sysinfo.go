package addons

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"time"

	"whatsbot-go/internal/core"
)

// Sysinfo porta addons/sysinfo.js.
type Sysinfo struct{}

func init() { core.Register(Sysinfo{}) }

func (Sysinfo) Commands() []string { return []string{"sysinfo", "sistema"} }

var (
	sysinfoStart = time.Now()
	sysinfoNow   = time.Now
	sysinfoMem   = func() (uint64, uint64) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.Sys, m.HeapAlloc
	}
)

func (Sysinfo) Handle(ctx *core.Context) error {
	if ctx == nil {
		return nil
	}
	rss, heap := sysinfoMem()
	uptime := sysinfoNow().Sub(sysinfoStart).Hours()

	text := "🤖 *Bot Status*\n\n" +
		"⚙️ *Sistema*\n" +
		fmt.Sprintf("• Go: %s\n", runtime.Version()) +
		fmt.Sprintf("• Plataforma: %s\n\n", runtime.GOOS) +
		"💾 *Memoria*\n" +
		fmt.Sprintf("• RSS: %.2f MB\n", float64(rss)/1024/1024) +
		fmt.Sprintf("• Heap: %.2f MB\n\n", float64(heap)/1024/1024) +
		fmt.Sprintf("🕒 *Uptime*: %.2f horas", math.Round(uptime*100)/100)

	if err := ctx.Reply(text); err != nil {
		return ctx.React("❌")
	}
	return nil
}

var _ = strings.TrimSpace
